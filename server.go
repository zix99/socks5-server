package main

import (
	"os"

	"socks5-server-ng/pkg/go-socks5"

	"github.com/caarlos0/env/v9"
	"github.com/sirupsen/logrus"
)

type params struct {
	User             string   `env:"PROXY_USER" envDefault:""`
	Password         string   `env:"PROXY_PASSWORD,unset" envDefault:""`
	Port             string   `env:"PROXY_PORT" envDefault:"1080"`
	StatusPort       string   `env:"PROXY_STATUS_PORT"`
	ProxyResolver    string   `env:"PROXY_RESOLVER"`
	ProxyResolverNet string   `env:"PROXY_RESOLVER_NET" envDefault:"ip4"` // ip, ip4, ip6
	ProxyRequireFQDN bool     `env:"PROXY_REQUIRE_FQDN"`                  // if true, require the FQDN (rather than IP). Forces resolver to work
	Verbose          bool     `env:"PROXY_VERBOSE"`
	AllowedDestFqdn  string   `env:"ALLOWED_DEST_FQDN" envDefault:""`
	AllowedCIDRs     []string `env:"ALLOWED_CIDR" envSeparator:"," envDefault:""`
}

func main() {
	// Working with app params
	cfg := params{}
	err := env.Parse(&cfg)
	if err != nil {
		logrus.Fatalf("%+v\n", err)
	}

	// verbose?
	if cfg.Verbose {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.Debugf("Verbose logging enabled")
	}

	//Initialize socks5 config
	socks5conf := &socks5.Config{}

	if cfg.User+cfg.Password != "" {
		creds := socks5.StaticCredentials{
			os.Getenv("PROXY_USER"): os.Getenv("PROXY_PASSWORD"),
		}
		cator := socks5.UserPassAuthenticator{Credentials: creds}
		socks5conf.AuthMethods = []socks5.Authenticator{cator}
	}

	rules := socks5.PermitChain{}

	if cfg.AllowedDestFqdn != "" {
		destAddrRule, err := PermitDestAddrPattern(cfg.AllowedDestFqdn)
		if err != nil {
			logrus.Fatal(err)
		}
		rules = append(rules, destAddrRule)
	}

	if cfg.ProxyRequireFQDN {
		rules = append(rules, RuleRequireFQDN())
	}

	rules = append(rules, socks5.PermitAll())
	socks5conf.Rules = rules

	if len(cfg.AllowedCIDRs) > 0 {
		cidrSet, err := socks5.NewCidrSet(cfg.AllowedCIDRs...)
		if err != nil {
			logrus.Fatal(err)
		}
		socks5conf.Filter = cidrSet
	}

	if cfg.ProxyResolver != "" {
		socks5conf.Resolver = socks5.NewCustomResolver(cfg.ProxyResolver, cfg.ProxyResolverNet)
	}

	server, err := socks5.New(socks5conf)
	if err != nil {
		logrus.Fatal(err)
	}

	if cfg.StatusPort != "" {
		go serveStatusPage(server, ":"+cfg.StatusPort)
	}

	logrus.Infof("Start listening proxy service on port %s\n", cfg.Port)
	if err := server.ListenAndServe("tcp", ":"+cfg.Port); err != nil {
		logrus.Fatal(err)
	}
}
