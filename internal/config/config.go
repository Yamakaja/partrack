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
}

type OIDCProvider struct {
	Name          string `yaml:"name"`
	Issuer        string `yaml:"issuer"`
	ClientID      string `yaml:"client_id"`
	ClientSecret  string `yaml:"client_secret"`
	AutoProvision bool   `yaml:"auto_provision"`
	DefaultTeamID string `yaml:"default_team_id"`
}

type LogConfig struct {
	Level string `yaml:"level"`
}

type AppConfig struct {
	BaseURL string `yaml:"base_url"`
}

func defaults() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr: ":8080",
		},
		Log: LogConfig{
			Level: "info",
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

	// Per-provider OIDC secret override: PARTRACK_OIDC_<NAME>_SECRET
	for i := range cfg.OIDC {
		envKey := "PARTRACK_OIDC_" + strings.ToUpper(cfg.OIDC[i].Name) + "_SECRET"
		if v := os.Getenv(envKey); v != "" {
			cfg.OIDC[i].ClientSecret = v
		}
	}

	return cfg
}
