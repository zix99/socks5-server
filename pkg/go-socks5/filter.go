package socks5

import "net"

type ClientFilter interface {
	Allowed(ip net.IP) bool
}

type ClientFilterAllowAll struct{}

func (s *ClientFilterAllowAll) Allowed(ip net.IP) bool {
	return true
}

type ClientFilterCIDR struct {
	cidrs []*net.IPNet
}

func NewCidrSet(cidrs ...string) (*ClientFilterCIDR, error) {
	ret := &ClientFilterCIDR{}

	for _, cidr := range cidrs {
		_, net, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, err
		}
		ret.cidrs = append(ret.cidrs, net)
	}

	return ret, nil
}

func (s *ClientFilterCIDR) Allowed(ip net.IP) bool {
	for _, net := range s.cidrs {
		if net.Contains(ip) {
			return true
		}
	}
	return false
}
