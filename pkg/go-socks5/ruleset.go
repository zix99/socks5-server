package socks5

import "context"

// RuleSet is used to provide custom rules to allow or prohibit actions
type RuleSet interface {
	Allow(ctx context.Context, req *Request) bool
}

// Permit value globally
type PermitDefault struct {
	Value bool
}

// PermitAll returns a RuleSet which allows all types of connections
func PermitAll() RuleSet {
	return &PermitDefault{true}
}

// PermitNone returns a RuleSet which disallows all types of connections
func PermitNone() RuleSet {
	return &PermitDefault{false}
}

func (p *PermitDefault) Allow(ctx context.Context, req *Request) bool {
	return p.Value
}

type PermitChain []RuleSet

func (s PermitChain) Allow(ctx context.Context, req *Request) bool {
	for _, rule := range s {
		if !rule.Allow(ctx, req) {
			return false
		}
	}
	return true
}
