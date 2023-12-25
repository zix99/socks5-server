package main

import (
	"context"
	"regexp"

	"socks5-server-ng/pkg/go-socks5"
)

// PermitDestAddrPattern returns a RuleSet which selectively allows addresses
func PermitDestAddrPattern(pattern string) (socks5.RuleSet, error) {
	r, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &PermitDestAddrPatternRuleSet{r}, nil
}

// PermitDestAddrPatternRuleSet is an implementation of the RuleSet which
// enables filtering supported destination address
type PermitDestAddrPatternRuleSet struct {
	re *regexp.Regexp
}

func (p *PermitDestAddrPatternRuleSet) Allow(ctx context.Context, req *socks5.Request) bool {
	return p.re.MatchString(req.DestAddr.FQDN)
}

// Special rules
type RequestRule func(req *socks5.Request) bool

func (s RequestRule) Allow(ctx context.Context, req *socks5.Request) bool {
	return s(req)
}

func RuleRequireFQDN() RequestRule {
	return func(req *socks5.Request) bool {
		return req.DestAddr.FQDN != ""
	}
}
