package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	OIDC     []OIDCProvider `yaml:"oidc"`
	Log      LogConfig      `yaml:"log"`
	App      AppConfig      `yaml:"app"`
}

type ServerConfig struct {
	ListenAddr     string   `yaml:"listen_addr"`
	TrustedProxies []string `yaml:"trusted_proxies"` // CIDRs or IPs, e.g. ["127.0.0.1/8", "10.0.0.0/8"]
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

type AuthConfig struct {
	SessionSecret string `yaml:"session_secret"`
	// RateLimitPerMinute is the per-IP request budget for unauthenticated
	// credential endpoints (login, password reset, token creation). 0 disables
	// rate limiting (used in tests).
	RateLimitPerMinute int `yaml:"rate_limit_per_minute"`
}

type OIDCProvider struct {
	Name string `yaml:"name"` // URL-safe slug used in routes and oidc_identities.provider
	// DisplayName is shown on the login button; defaults to Name when empty.
	DisplayName   string `yaml:"display_name"`
	Issuer        string `yaml:"issuer"`
	ClientID      string `yaml:"client_id"`
	ClientSecret  string `yaml:"client_secret"`
	AutoProvision bool   `yaml:"auto_provision"`
}

type LogConfig struct {
	Level string `yaml:"level"`
}

type AppConfig struct {
	BaseURL string `yaml:"base_url"`
	// SecureCookies controls the Secure flag on session/auth cookies. Defaults to
	// true; set false only for local HTTP development. TLS is expected to be
	// terminated by a reverse proxy in front of the app.
	SecureCookies bool `yaml:"secure_cookies"`
}

func defaults() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr: ":8080",
		},
		Log: LogConfig{
			Level: "info",
		},
		App: AppConfig{
			SecureCookies: true,
		},
		Auth: AuthConfig{
			RateLimitPerMinute: 10,
		},
	}
}

// Load reads the YAML config file and overlays environment variable overrides.
// Config file path is read from PARTRACK_CONFIG (defaults to ./config.yaml).
// If the file does not exist, only defaults and env vars are used.
func Load() *Config {
	cfg := defaults()

	path := os.Getenv("PARTRACK_CONFIG")
	if path == "" {
		path = "config.yaml"
	}

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "config: failed to read %s: %v\n", path, err)
		os.Exit(1)
	}
	if data != nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "config: failed to parse %s: %v\n", path, err)
			os.Exit(1)
		}
	}

	// Env var overrides
	if v := os.Getenv("PARTRACK_DB_URL"); v != "" {
		cfg.Database.URL = v
	}
	if v := os.Getenv("PARTRACK_SESSION_SECRET"); v != "" {
		cfg.Auth.SessionSecret = v
	}
	if v := os.Getenv("PARTRACK_LISTEN_ADDR"); v != "" {
		cfg.Server.ListenAddr = v
	}
	if v := os.Getenv("PARTRACK_APP_BASE_URL"); v != "" {
		cfg.App.BaseURL = v
	}
	if v := os.Getenv("PARTRACK_SECURE_COOKIES"); v != "" {
		cfg.App.SecureCookies = v != "false" && v != "0"
	}

	// Per-provider OIDC secret override: PARTRACK_OIDC_<NAME>_SECRET
	for i := range cfg.OIDC {
		envKey := "PARTRACK_OIDC_" + strings.ToUpper(cfg.OIDC[i].Name) + "_SECRET"
		if v := os.Getenv(envKey); v != "" {
			cfg.OIDC[i].ClientSecret = v
		}
	}

	// The session secret is the HMAC key for OAuth state and signed link tokens;
	// a weak/empty value makes them forgeable. Fail fast rather than start insecure.
	if len(cfg.Auth.SessionSecret) < 32 {
		fmt.Fprintln(os.Stderr, "config: auth.session_secret must be set and at least 32 bytes (env PARTRACK_SESSION_SECRET)")
		os.Exit(1)
	}

	// OIDC redirect URIs are built from app.base_url; without it the flow cannot
	// produce the exact redirect URI registered at the IdP.
	cfg.App.BaseURL = strings.TrimRight(cfg.App.BaseURL, "/")
	if len(cfg.OIDC) > 0 && cfg.App.BaseURL == "" {
		fmt.Fprintln(os.Stderr, "config: app.base_url must be set when OIDC providers are configured (env PARTRACK_APP_BASE_URL)")
		os.Exit(1)
	}

	return cfg
}
