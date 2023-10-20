package socks5

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"strings"
	"time"
)

// NameResolver is used to implement custom name resolution
type NameResolver interface {
	Resolve(ctx context.Context, name string) (net.IP, error)
}

// SysDNSResolver uses the system DNS to resolve host names
type SysDNSResolver struct{}

func (d SysDNSResolver) Resolve(ctx context.Context, name string) (net.IP, error) {
	addr, err := net.ResolveIPAddr("ip", name)
	if err != nil {
		return nil, err
	}
	return addr.IP, err
}

// CustomResolver uses a specific name server IP to resolve domains
type CustomResolver struct {
	r       *net.Resolver
	network string
}

func NewCustomResolver(nameserver, network string) *CustomResolver {
	if strings.IndexByte(nameserver, ':') < 0 {
		nameserver = nameserver + ":53"
	}

	return &CustomResolver{
		r: &net.Resolver{
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: 2 * time.Second,
				}
				return d.DialContext(ctx, network, nameserver)
			},
			PreferGo: true,
		},
		network: network,
	}
}

func (d *CustomResolver) Resolve(ctx context.Context, name string) (net.IP, error) {
	addrs, err := d.r.LookupIP(ctx, d.network, name)
	if err != nil {
		return nil, err
	}
	switch len(addrs) {
	case 0:
		return nil, errors.New("resolve: no ip")
	case 1:
		return addrs[0], nil
	default:
		return addrs[rand.Intn(len(addrs))], nil
	}
}
