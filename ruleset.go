package main

import (
	"context"
	"regexp"

	"socks5-server-ng/pkg/go-socks5"
)

// PermitDestAddrPattern returns a RuleSet which selectively allows addresses
func PermitDestAddrPattern(pattern string) socks5.RuleSet {
	return &PermitDestAddrPatternRuleSet{pattern}
}

// PermitDestAddrPatternRuleSet is an implementation of the RuleSet which
// enables filtering supported destination address
type PermitDestAddrPatternRuleSet struct {
	AllowedFqdnPattern string
}

func (p *PermitDestAddrPatternRuleSet) Allow(ctx context.Context, req *socks5.Request) bool {
	match, _ := regexp.MatchString(p.AllowedFqdnPattern, req.DestAddr.FQDN)
	return match
}
