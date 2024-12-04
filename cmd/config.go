package main

import (
	"flag"
	"log"
)

type sirsiConfig struct {
	WebServicesURL string
	ScriptURL      string
	User           string
	Password       string
	ClientID       string
	Library        string
}

type secretsConfig struct {
	VirgoJWTKey      string
	VirgoJWTSecret   string
	AuthSharedSecret string
	SecretKeyBase    string
}

type serviceConfig struct {
	Port        int
	Secrets     secretsConfig
	Sirsi       sirsiConfig
	VirgoURL    string
	PDAURL      string
	UserInfoURL string
}

func loadConfiguration() *serviceConfig {
	var cfg serviceConfig
	flag.IntVar(&cfg.Port, "port", 8080, "Service port (default 8080)")

	// secrets and keys
	flag.StringVar(&cfg.Secrets.VirgoJWTKey, "jwtkey", "", "JWT signature key")
	flag.StringVar(&cfg.Secrets.VirgoJWTSecret, "jwtsecret", "", "JWT signature secret")
	flag.StringVar(&cfg.Secrets.AuthSharedSecret, "authsecret", "", "Auth Shared secret (for user service)")
	flag.StringVar(&cfg.Secrets.SecretKeyBase, "secretbase", "", "JWT base secret")

	// sirsi config
	flag.StringVar(&cfg.Sirsi.WebServicesURL, "sirsiurl", "", "Sirsi web services url")
	flag.StringVar(&cfg.Sirsi.ScriptURL, "sirsiscript", "", "Sirsi script services url")
	flag.StringVar(&cfg.Sirsi.User, "sirsiuser", "", "Sirsi user")
	flag.StringVar(&cfg.Sirsi.Password, "sirsipass", "", "Sirsi password")
	flag.StringVar(&cfg.Sirsi.ClientID, "sirsiclient", "", "Sirsi client ID")
	flag.StringVar(&cfg.Sirsi.Library, "sirsilibrary", "UVA-LIB", "Sirsi Library ID")

	// external services
	flag.StringVar(&cfg.VirgoURL, "virgo", "", "URL to Virgo")
	flag.StringVar(&cfg.PDAURL, "pda", "", "URL to PDA")
	flag.StringVar(&cfg.UserInfoURL, "userinfo", "", "URL to user info service")

	flag.Parse()

	if cfg.Secrets.VirgoJWTKey == "" {
		log.Fatal("jwtkey param is required")
	}
	if cfg.Secrets.VirgoJWTSecret == "" {
		log.Fatal("jwtsecret param is required")
	}
	if cfg.Secrets.AuthSharedSecret == "" {
		log.Fatal("authsecret param is required")
	}
	if cfg.Secrets.SecretKeyBase == "" {
		log.Fatal("secretbase param is required")
	}
	if cfg.Sirsi.WebServicesURL == "" {
		log.Fatal("sirsiurl param is required")
	}
	if cfg.Sirsi.ScriptURL == "" {
		log.Fatal("sirsiscript param is required")
	}
	if cfg.Sirsi.User == "" {
		log.Fatal("sirsiuser param is required")
	}
	if cfg.Sirsi.Password == "" {
		log.Fatal("sirsipass param is required")
	}
	if cfg.Sirsi.ClientID == "" {
		log.Fatal("sirsiclient param is required")
	}
	if cfg.VirgoURL == "" {
		log.Fatal("virgo param is required")
	}
	if cfg.PDAURL == "" {
		log.Fatal("pda param is required")
	}
	if cfg.UserInfoURL == "" {
		log.Fatal("userinfo param is required")
	}

	log.Printf("[CONFIG] port          = [%d]", cfg.Port)
	log.Printf("[CONFIG] sirsiurl      = [%s]", cfg.Sirsi.WebServicesURL)
	log.Printf("[CONFIG] sirsiscript   = [%s]", cfg.Sirsi.ScriptURL)
	log.Printf("[CONFIG] sirsiuser     = [%s]", cfg.Sirsi.User)
	log.Printf("[CONFIG] sirsiclient   = [%s]", cfg.Sirsi.ClientID)
	log.Printf("[CONFIG] sirsilibrary  = [%s]", cfg.Sirsi.Library)
	log.Printf("[CONFIG] virgo         = [%s]", cfg.VirgoURL)
	log.Printf("[CONFIG] pda           = [%s]", cfg.PDAURL)
	log.Printf("[CONFIG] userinfo      = [%s]", cfg.UserInfoURL)

	return &cfg
}
