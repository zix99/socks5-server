package main

import (
	"log"
	"os"

	"socks5-server-ng/pkg/go-socks5"

	"github.com/caarlos0/env/v9"
)

type params struct {
	User             string   `env:"PROXY_USER" envDefault:""`
	Password         string   `env:"PROXY_PASSWORD,unset" envDefault:""`
	Port             string   `env:"PROXY_PORT" envDefault:"1080"`
	StatusPort       string   `env:"PROXY_STATUS_PORT"`
	ProxyResolver    string   `env:"PROXY_RESOLVER"`
	ProxyResolverNet string   `env:"PROXY_RESOLVER_NET" envDefault:"ip4"` // ip, ip4, ip6
	AllowedDestFqdn  string   `env:"ALLOWED_DEST_FQDN" envDefault:""`
	AllowedCIDRs     []string `env:"ALLOWED_CIDR" envSeparator:"," envDefault:""`
}

func main() {
	// Working with app params
	cfg := params{}
	err := env.Parse(&cfg)
	if err != nil {
		log.Fatalf("%+v\n", err)
	}

	//Initialize socks5 config
	socks5conf := &socks5.Config{
		Logger: log.New(os.Stdout, "", log.LstdFlags),
	}

	if cfg.User+cfg.Password != "" {
		creds := socks5.StaticCredentials{
			os.Getenv("PROXY_USER"): os.Getenv("PROXY_PASSWORD"),
		}
		cator := socks5.UserPassAuthenticator{Credentials: creds}
		socks5conf.AuthMethods = []socks5.Authenticator{cator}
	}

	if cfg.AllowedDestFqdn != "" {
		rules, err := PermitDestAddrPattern(cfg.AllowedDestFqdn)
		if err != nil {
			log.Fatal(err)
		}
		socks5conf.Rules = rules
	}

	if len(cfg.AllowedCIDRs) > 0 {
		cidrSet, err := socks5.NewCidrSet(cfg.AllowedCIDRs...)
		if err != nil {
			log.Fatal(err)
		}
		socks5conf.Filter = cidrSet
	}

	if cfg.ProxyResolver != "" {
		socks5conf.Resolver = socks5.NewCustomResolver(cfg.ProxyResolver, cfg.ProxyResolverNet)
	}

	server, err := socks5.New(socks5conf)
	if err != nil {
		log.Fatal(err)
	}

	if cfg.StatusPort != "" {
		go serveStatusPage(server, ":"+cfg.StatusPort)
	}

	log.Printf("Start listening proxy service on port %s\n", cfg.Port)
	if err := server.ListenAndServe("tcp", ":"+cfg.Port); err != nil {
		log.Fatal(err)
	}
}
