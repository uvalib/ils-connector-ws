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
	VirgoJWTKey string
	UserJWTKey  string
}

type solrConfig struct {
	URL  string
	Core string
}

type smtpConfig struct {
	Host    string
	Port    int
	User    string
	Pass    string
	Sender  string
	DevMode bool
}

type serviceConfig struct {
	Port               int
	Secrets            secretsConfig
	Sirsi              sirsiConfig
	Solr               solrConfig
	VirgoURL           string
	PDAURL             string
	UserInfoURL        string
	HSILLiadURL        string
	CourseReserveEmail string
	LawReserveEmail    string
	SMTP               smtpConfig
}

func loadConfiguration() *serviceConfig {
	var cfg serviceConfig
	flag.IntVar(&cfg.Port, "port", 8080, "Service port (default 8080)")

	// secrets and keys
	flag.StringVar(&cfg.Secrets.VirgoJWTKey, "jwtkey", "", "JWT signature key")
	flag.StringVar(&cfg.Secrets.UserJWTKey, "userkey", "", "Auth Shared secret for user service")

	// sirsi config
	flag.StringVar(&cfg.Sirsi.WebServicesURL, "sirsiurl", "", "Sirsi web services url")
	flag.StringVar(&cfg.Sirsi.ScriptURL, "sirsiscript", "", "Sirsi script services url")
	flag.StringVar(&cfg.Sirsi.User, "sirsiuser", "", "Sirsi user")
	flag.StringVar(&cfg.Sirsi.Password, "sirsipass", "", "Sirsi password")
	flag.StringVar(&cfg.Sirsi.ClientID, "sirsiclient", "", "Sirsi client ID")
	flag.StringVar(&cfg.Sirsi.Library, "sirsilibrary", "UVA-LIB", "Sirsi Library ID")

	// Solr config
	flag.StringVar(&cfg.Solr.URL, "solr", "", "Solr URL")
	flag.StringVar(&cfg.Solr.Core, "core", "test_core", "Solr core")

	// external services
	flag.StringVar(&cfg.VirgoURL, "virgo", "", "URL to Virgo")
	flag.StringVar(&cfg.PDAURL, "pda", "", "URL to PDA")
	flag.StringVar(&cfg.UserInfoURL, "userinfo", "", "URL to user info service")

	// email / smtp
	flag.StringVar(&cfg.CourseReserveEmail, "cremail", "", "Email recipient for course reserves requests")
	flag.StringVar(&cfg.LawReserveEmail, "lawemail", "", "Law Email recipient for course reserves requests")
	flag.StringVar(&cfg.SMTP.Host, "smtphost", "", "SMTP Host")
	flag.IntVar(&cfg.SMTP.Port, "smtpport", 0, "SMTP Port")
	flag.StringVar(&cfg.SMTP.User, "smtpuser", "", "SMTP User")
	flag.StringVar(&cfg.SMTP.Pass, "smtppass", "", "SMTP Password")
	flag.StringVar(&cfg.SMTP.Sender, "smtpsender", "virgo4@virginia.edu", "SMTP sender email")
	flag.BoolVar(&cfg.SMTP.DevMode, "stubsmtp", false, "Log email insted of sending (dev mode)")

	// Illiad communications
	flag.StringVar(&cfg.HSILLiadURL, "hsilliad", "", "HS Illiad API URL")

	flag.Parse()

	if cfg.Secrets.VirgoJWTKey == "" {
		log.Fatal("jwtkey param is required")
	}
	if cfg.Secrets.UserJWTKey == "" {
		log.Fatal("userkey param is required")
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
	if cfg.Solr.URL == "" || cfg.Solr.Core == "" {
		log.Fatal("solr and core params are required")
	}
	if cfg.HSILLiadURL == "" {
		log.Fatal("hsilliad param is required")
	}
	if cfg.CourseReserveEmail == "" {
		log.Fatal("cremail param is required")
	}
	if cfg.LawReserveEmail == "" {
		log.Fatal("lawemail param is required")
	}

	log.Printf("[CONFIG] port          = [%d]", cfg.Port)
	log.Printf("[CONFIG] sirsiurl      = [%s]", cfg.Sirsi.WebServicesURL)
	log.Printf("[CONFIG] sirsiscript   = [%s]", cfg.Sirsi.ScriptURL)
	log.Printf("[CONFIG] sirsiuser     = [%s]", cfg.Sirsi.User)
	log.Printf("[CONFIG] sirsiclient   = [%s]", cfg.Sirsi.ClientID)
	log.Printf("[CONFIG] sirsilibrary  = [%s]", cfg.Sirsi.Library)
	log.Printf("[CONFIG] solr          = [%s]", cfg.Solr.URL)
	log.Printf("[CONFIG] core          = [%s]", cfg.Solr.Core)
	if cfg.SMTP.User != "" {
		log.Printf("[CONFIG] smtpuser      = [%s]", cfg.SMTP.User)
	}
	log.Printf("[CONFIG] smtpsender    = [%s]", cfg.SMTP.Sender)
	log.Printf("[CONFIG] stubsmtp      = [%t]", cfg.SMTP.DevMode)
	log.Printf("[CONFIG] cremail       = [%s]", cfg.CourseReserveEmail)
	log.Printf("[CONFIG] lawemail      = [%s]", cfg.LawReserveEmail)
	log.Printf("[CONFIG] hsilliad      = [%s]", cfg.HSILLiadURL)
	log.Printf("[CONFIG] virgo         = [%s]", cfg.VirgoURL)
	log.Printf("[CONFIG] pda           = [%s]", cfg.PDAURL)
	log.Printf("[CONFIG] userinfo      = [%s]", cfg.UserInfoURL)

	return &cfg
}
