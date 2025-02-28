package socks5

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"github.com/sirupsen/logrus"
)

const (
	socks5Version = uint8(5)
)

// Config is used to setup and configure a Server
type Config struct {
	// Client Filter RuleSet
	Filter ClientFilter

	// AuthMethods can be provided to implement custom authentication
	// By default, "auth-less" mode is enabled.
	// For password-based auth use UserPassAuthenticator.
	AuthMethods []Authenticator

	// If provided, username/password authentication is enabled,
	// by appending a UserPassAuthenticator to AuthMethods. If not provided,
	// and AUthMethods is nil, then "auth-less" mode is enabled.
	Credentials CredentialStore

	// Resolver can be provided to do custom name resolution.
	// Defaults to DNSResolver if not provided.
	Resolver NameResolver

	// Rules is provided to enable custom logic around permitting
	// various commands. If not provided, PermitAll is used.
	Rules RuleSet

	// Rewriter can be used to transparently rewrite addresses.
	// This is invoked before the RuleSet is invoked.
	// Defaults to NoRewrite.
	Rewriter AddressRewriter

	// BindIP is used for bind or udp associate
	BindIP net.IP

	// Detailed metrics (per-downstream)
	DetailedMetrics bool

	// Logger can be used to provide a custom log target.
	// Defaults to stdout.
	Logger *logrus.Logger

	// Optional function for dialing out
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

type NetMetrics struct {
	Active atomic.Int64
	Rx, Tx atomic.Int64
}

type HostMetrics struct {
	NetMetrics
	Commands  [4]atomic.Int64
	ActiveUDP atomic.Int64
	LastSeen  atomic.Value
}

// Server is reponsible for accepting connections and handling
// the details of the SOCKS5 protocol
type Server struct {
	config      *Config
	authMethods map[uint8]Authenticator

	hostMetrics   *ttlcache.Cache[string, *HostMetrics]
	targetMetrics *ttlcache.Cache[string, *NetMetrics]
}

// New creates a new Server and potentially returns an error
func New(conf *Config) (*Server, error) {
	// Ensure we have at least one authentication method enabled
	if len(conf.AuthMethods) == 0 {
		if conf.Credentials != nil {
			conf.AuthMethods = []Authenticator{&UserPassAuthenticator{conf.Credentials}}
		} else {
			conf.AuthMethods = []Authenticator{&NoAuthAuthenticator{}}
		}
	}

	// Ensure we have a DNS resolver
	if conf.Resolver == nil {
		conf.Resolver = SysDNSResolver{}
	}

	// Ensure we have a rule set
	if conf.Rules == nil {
		conf.Rules = PermitAll()
	}

	// Ensure we have a log target
	if conf.Logger == nil {
		conf.Logger = logrus.StandardLogger()
	}

	server := &Server{
		config: conf,
		hostMetrics: ttlcache.New[string, *HostMetrics](
			ttlcache.WithTTL[string, *HostMetrics](24*time.Hour),
			ttlcache.WithLoader[string, *HostMetrics](ttlcache.LoaderFunc[string, *HostMetrics](func(c *ttlcache.Cache[string, *HostMetrics], key string) *ttlcache.Item[string, *HostMetrics] {
				item := c.Set(key, &HostMetrics{}, ttlcache.DefaultTTL)
				return item
			})),
		),
		targetMetrics: ttlcache.New[string, *NetMetrics](
			ttlcache.WithTTL[string, *NetMetrics](30*time.Minute),
			ttlcache.WithLoader[string, *NetMetrics](ttlcache.LoaderFunc[string, *NetMetrics](func(c *ttlcache.Cache[string, *NetMetrics], key string) *ttlcache.Item[string, *NetMetrics] {
				item := c.Set(key, &NetMetrics{}, ttlcache.DefaultTTL)
				return item
			})),
		),
	}

	go server.hostMetrics.Start()
	go server.targetMetrics.Start()

	server.authMethods = make(map[uint8]Authenticator)

	for _, a := range conf.AuthMethods {
		server.authMethods[a.GetCode()] = a
	}

	return server, nil
}

func (s *Server) Close() {
	s.hostMetrics.Stop()
	s.targetMetrics.Stop()
}

// ListenAndServe is used to create a listener and serve on it
func (s *Server) ListenAndServe(network, addr string) error {
	l, err := net.Listen(network, addr)
	if err != nil {
		return err
	}
	return s.Serve(l)
}

// Serve is used to serve connections from a listener
func (s *Server) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go s.ServeConn(conn)
	}
}

// ServeConn is used to serve a single connection.
func (s *Server) ServeConn(conn net.Conn) error {
	defer conn.Close()
	bufConn := bufio.NewReader(conn)

	// Check client IP against whitelist
	clientIP, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		s.config.Logger.Errorf("socks: Failed to get client IP address: %v", err)
		return err
	}
	ip := net.ParseIP(clientIP)
	if s.config.Filter != nil && !s.config.Filter.Allowed(ip) {
		s.config.Logger.Warnf("socks: Connection from not allowed IP address: %s", clientIP)
		return fmt.Errorf("connection from not allowed IP address")
	}

	// Read the version byte
	version := []byte{0}
	if _, err := bufConn.Read(version); err != nil {
		s.config.Logger.Errorf("socks: Failed to get version byte: %v", err)
		return err
	}

	// Ensure we are compatible
	if version[0] != socks5Version {
		err := fmt.Errorf("Unsupported SOCKS version: %v", version)
		s.config.Logger.Errorf("socks: %v", err)
		return err
	}

	// Authenticate the connection
	authContext, err := s.authenticate(conn, bufConn)
	if err != nil {
		err = fmt.Errorf("Failed to authenticate: %v", err)
		s.config.Logger.Warnf("socks: %v", err)
		return err
	}

	request, err := NewRequest(bufConn)
	if err != nil {
		if err == unrecognizedAddrType {
			if err := sendReply(conn, addrTypeNotSupported, nil); err != nil {
				return fmt.Errorf("Failed to send reply: %v", err)
			}
		}
		return fmt.Errorf("Failed to read destination address: %v", err)
	}
	request.AuthContext = authContext
	if client, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		request.RemoteAddr = &AddrSpec{IP: client.IP, Port: client.Port}
	}

	// Process the client request
	if err := s.handleRequest(request, conn); err != nil {
		err = fmt.Errorf("Failed to handle request: %v", err)
		s.config.Logger.Errorf("socks: %v", err)
		return err
	}

	return nil
}

func (s *Server) RangeHostMetrics(f func(host string, m *HostMetrics)) {
	s.hostMetrics.Range(func(item *ttlcache.Item[string, *HostMetrics]) bool {
		f(item.Key(), item.Value())
		return true
	})
}

func (s *Server) RangeTargetMetrics(f func(target string, m *NetMetrics)) {
	s.targetMetrics.Range(func(item *ttlcache.Item[string, *NetMetrics]) bool {
		f(item.Key(), item.Value())
		return true
	})
}
