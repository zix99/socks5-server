package socks5

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/puzpuzpuz/xsync/v3"
)

const (
	ConnectCommand   = uint8(1)
	BindCommand      = uint8(2)
	AssociateCommand = uint8(3)
	ipv4Address      = uint8(1)
	fqdnAddress      = uint8(3)
	ipv6Address      = uint8(4)
)

const (
	successReply uint8 = iota
	serverFailure
	ruleFailure
	networkUnreachable
	hostUnreachable
	connectionRefused
	ttlExpired
	commandNotSupported
	addrTypeNotSupported
)

var (
	unrecognizedAddrType = fmt.Errorf("Unrecognized address type")
)

// AddressRewriter is used to rewrite a destination transparently
type AddressRewriter interface {
	Rewrite(ctx context.Context, request *Request) *AddrSpec
}

// AddrSpec is used to return the target AddrSpec
// which may be specified as IPv4, IPv6, or a FQDN
type AddrSpec struct {
	FQDN string
	IP   net.IP
	Port int
}

func (a *AddrSpec) String() string {
	if a.FQDN != "" {
		return fmt.Sprintf("%s (%s):%d", a.FQDN, a.IP, a.Port)
	}
	return fmt.Sprintf("%s:%d", a.IP, a.Port)
}

// Address returns a string suitable to dial; prefer returning IP-based
// address, fallback to FQDN
func (a AddrSpec) Address() string {
	if 0 != len(a.IP) {
		return net.JoinHostPort(a.IP.String(), strconv.Itoa(a.Port))
	}
	return net.JoinHostPort(a.FQDN, strconv.Itoa(a.Port))
}

// A Request represents request received by a server
type Request struct {
	// Protocol version
	Version uint8
	// Requested command
	Command uint8
	// AuthContext provided during negotiation
	AuthContext *AuthContext
	// AddrSpec of the the network that sent the request
	RemoteAddr *AddrSpec
	// AddrSpec of the desired destination
	DestAddr *AddrSpec
	// AddrSpec of the actual destination (might be affected by rewrite)
	realDestAddr *AddrSpec
	bufConn      io.Reader
}

type conn interface {
	Write([]byte) (int, error)
	RemoteAddr() net.Addr
}

// NewRequest creates a new Request from the tcp connection
func NewRequest(bufConn io.Reader) (*Request, error) {
	// Read the version byte
	header := []byte{0, 0, 0}
	if _, err := io.ReadAtLeast(bufConn, header, 3); err != nil {
		return nil, fmt.Errorf("Failed to get command version: %v", err)
	}

	// Ensure we are compatible
	if header[0] != socks5Version {
		return nil, fmt.Errorf("Unsupported command version: %v", header[0])
	}

	// Read in the destination address
	dest, err := readAddrSpec(bufConn)
	if err != nil {
		return nil, err
	}

	request := &Request{
		Version:  socks5Version,
		Command:  header[1],
		DestAddr: dest,
		bufConn:  bufConn,
	}

	return request, nil
}

// handleRequest is used for request processing after authentication
func (s *Server) handleRequest(req *Request, conn conn) error {
	ctx := context.Background()

	// Resolve the address if we have a FQDN
	dest := req.DestAddr
	if dest.FQDN != "" {
		addr, err := s.config.Resolver.Resolve(ctx, dest.FQDN)
		if err != nil {
			if err := sendReply(conn, hostUnreachable, nil); err != nil {
				return fmt.Errorf("Failed to send reply: %v", err)
			}
			return fmt.Errorf("Failed to resolve destination '%v': %v", dest.FQDN, err)
		}
		dest.IP = addr
	}

	// Apply any address rewrites
	req.realDestAddr = req.DestAddr
	if s.config.Rewriter != nil {
		req.realDestAddr = s.config.Rewriter.Rewrite(ctx, req)
	}

	// Record metrics
	metrics := s.getHostMetrics(req.RemoteAddr.IP.String())
	metrics.Commands[req.Command].Add(1)
	metrics.LastSeen.Store(time.Now())

	// Switch on the command
	switch req.Command {
	case ConnectCommand:
		return s.handleConnect(ctx, conn, req)
	case BindCommand:
		return s.handleBind(ctx, conn, req)
	case AssociateCommand:
		return s.handleAssociate(ctx, conn, req)
	default:
		// s.Metrics.NotSupported.Add(1)
		if err := sendReply(conn, commandNotSupported, nil); err != nil {
			return fmt.Errorf("Failed to send reply: %v", err)
		}
		return fmt.Errorf("Unsupported command: %v", req.Command)
	}
}

// handleConnect is used to handle a connect command
func (s *Server) handleConnect(ctx context.Context, conn conn, req *Request) error {
	s.config.Logger.Infof("%s connect to %s", req.RemoteAddr.String(), req.realDestAddr.String())

	host := s.getHostMetrics(req.RemoteAddr.IP.String())
	host.Active.Add(1)
	defer host.Active.Add(-1)

	// Check if this is allowed
	if ok := s.config.Rules.Allow(ctx, req); !ok {
		if err := sendReply(conn, ruleFailure, nil); err != nil {
			return fmt.Errorf("Failed to send reply: %v", err)
		}
		return fmt.Errorf("Connect to %v blocked by rules", req.DestAddr)
	}

	// Attempt to connect
	target, err := s.dial(ctx, "tcp", req.realDestAddr.Address())
	if err != nil {
		msg := err.Error()
		resp := hostUnreachable
		if strings.Contains(msg, "refused") {
			resp = connectionRefused
		} else if strings.Contains(msg, "network is unreachable") {
			resp = networkUnreachable
		}
		if err := sendReply(conn, resp, nil); err != nil {
			return fmt.Errorf("Failed to send reply: %v", err)
		}
		return fmt.Errorf("Connect to %v failed: %v", req.DestAddr, err)
	}
	defer target.Close()

	// Send success
	local := target.LocalAddr().(*net.TCPAddr)
	bind := AddrSpec{IP: local.IP, Port: local.Port}
	if err := sendReply(conn, successReply, &bind); err != nil {
		return fmt.Errorf("Failed to send reply: %v", err)
	}

	// Proxy
	proxyTx := proxy(&host.Tx, target, req.bufConn)
	proxyRx := proxy(&host.Rx, conn, target)

	if err := <-proxyRx; err != nil {
		return err
	}
	if err := <-proxyTx; err != nil {
		return err
	}

	return nil
}

// handleBind is used to handle a connect command
func (s *Server) handleBind(ctx context.Context, conn conn, req *Request) error {
	s.config.Logger.Warnf("Bind requested by %s, but unsupported", req.RemoteAddr)

	// Check if this is allowed
	if ok := s.config.Rules.Allow(ctx, req); !ok {
		if err := sendReply(conn, ruleFailure, nil); err != nil {
			return fmt.Errorf("Failed to send reply: %v", err)
		}
		return fmt.Errorf("Bind to %v blocked by rules", req.DestAddr)
	}

	// TODO: Support bind
	if err := sendReply(conn, commandNotSupported, nil); err != nil {
		return fmt.Errorf("Failed to send reply: %v", err)
	}
	return nil
}

// handleAssociate is used to handle a connect command
func (s *Server) handleAssociate(ctx context.Context, conn conn, req *Request) error {
	// Check if this is allowed
	if ok := s.config.Rules.Allow(ctx, req); !ok {
		if err := sendReply(conn, ruleFailure, nil); err != nil {
			return fmt.Errorf("Failed to send reply: %v", err)
		}
		return fmt.Errorf("Associate to %v blocked by rules", req.DestAddr)
	}

	metric := s.getHostMetrics(req.RemoteAddr.IP.String())
	metric.ActiveUDP.Add(1)
	defer metric.ActiveUDP.Add(-1)

	// Create UDP to listen on
	listenUdpSock, err := net.ListenUDP("udp", nil)
	if err != nil {
		sendReply(conn, serverFailure, nil)
		s.config.Logger.Warn(err)
		return err
	}
	defer listenUdpSock.Close()

	// Tell client we've opened a UDP socket
	bindAddr := listenUdpSock.LocalAddr().(*net.UDPAddr)
	s.config.Logger.Infof("%s associate with %s", req.RemoteAddr.String(), bindAddr.String())
	if err := sendReply(conn, successReply, &AddrSpec{IP: bindAddr.IP, Port: bindAddr.Port}); err != nil {
		return err
	}

	// Start receiving on UDP
	go s.handleAssociateConnection(ctx, metric, req.RemoteAddr, listenUdpSock)

	// Wait to read EOF/Closed
	for {
		miscBuf := make([]byte, 128)
		if n, err := req.bufConn.Read(miscBuf); err != nil {
			s.config.Logger.Debugf("Socket closed: %s", listenUdpSock.LocalAddr().String())
			break
		} else {
			s.config.Logger.Warnf("Received %d bytes of unexpected data from %s", n, req.RemoteAddr.String())
		}
	}

	return nil
}

func (s *Server) handleAssociateConnection(ctx context.Context, metric *HostMetrics, reqDestAddr *AddrSpec, sock *net.UDPConn) {
	const BUF_SIZE = 4096
	buf := make([]byte, BUF_SIZE)
	targetConns := xsync.NewMapOf[string, net.Conn]()
	defer func() {
		targetConns.Range(func(key string, value net.Conn) bool {
			value.Close()
			return true
		})
		targetConns.Clear()
	}()

	for {
		n, srcAddr, err := sock.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				break
			}
			continue
		}

		reader := bytes.NewReader(buf[:n])

		// Skip RSV & frag
		reader.Seek(3, io.SeekCurrent)

		// parse datagram
		targetAddr, _ := readAddrSpec(reader)
		headerEndPos, _ := reader.Seek(0, io.SeekCurrent)
		header := buf[:headerEndPos]
		data := buf[headerEndPos : n-int(headerEndPos)]

		// Check if src equal
		if !srcAddr.IP.Equal(reqDestAddr.IP) {
			s.config.Logger.Warnf("UDP Source packet (%s) is not expected (%s)", srcAddr, reqDestAddr)
			continue
		}

		// Start proxying to target (UDP-style)
		targetKey := srcAddr.String() + "--" + targetAddr.String()
		targetSock, hasTargetSock := targetConns.Load(targetKey)
		if !hasTargetSock {
			targetSock, err = s.dial(ctx, "udp", targetAddr.Address())
			if err != nil {
				continue
			}
			targetConns.Store(targetKey, targetSock)
			s.config.Logger.Debugf("New UDP target %s for %s", targetAddr.Address(), srcAddr.String())

			// Start proxy target back to client
			go func() {
				defer func() {
					targetSock.Close()
					targetConns.Delete(targetKey)
					s.config.Logger.Debugf("Closed %s", targetKey)
				}()

				for {
					buf := make([]byte, BUF_SIZE)
					n, err := targetSock.Read(buf)
					if err != nil {
						break
					}
					// Wrap header
					ret := make([]byte, n+len(header))
					copy(ret, header)
					copy(ret[len(header):], buf[:n])
					// Send to src
					metric.Rx.Add(int64(n))
					if _, err := sock.WriteToUDP(ret, srcAddr); err != nil {
						break
					}
				}
			}()
		}

		// Pass data to target
		metric.Tx.Add(int64(len(data)))
		if _, err := targetSock.Write(data); err != nil {
			if !errors.Is(err, io.EOF) || !errors.Is(err, net.ErrClosed) {
				s.config.Logger.Warnf("Error sending for %s: %v", targetSock.RemoteAddr().String(), err)
			}
			targetSock.Close()
			continue
		}
	}
}

func (s *Server) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if s.config.Dial != nil {
		return s.config.Dial(ctx, network, addr)
	}
	return net.Dial(network, addr)
}

type closeWriter interface {
	CloseWrite() error
}

// proxy is used to shuffle data from src to destination, and sends errors
// down a dedicated channel
func proxy(metric *atomic.Int64, dst io.Writer, src io.Reader) <-chan error {
	ret := make(chan error, 1)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				metric.Add(int64(n))

				_, err := dst.Write(buf[:n])
				if err != nil {
					ret <- err
					break
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					ret <- err
				} else {
					ret <- nil
				}
				break
			}
		}
		if tcpConn, ok := dst.(closeWriter); ok {
			tcpConn.CloseWrite()
		}
	}()
	return ret
}
