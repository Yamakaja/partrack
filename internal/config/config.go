package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	OIDC     []OIDCProvider `yaml:"oidc"`
	LDAP     []LDAPProvider `yaml:"ldap"`
	Log      LogConfig      `yaml:"log"`
	App      AppConfig      `yaml:"app"`
	Storage  StorageConfig  `yaml:"storage"`
}

// StorageConfig configures the content-addressed blob store used for uploaded
// images (and, later, footprints/symbols). Backend "file" stores bytes on the
// local filesystem under DataDir; an object-store backend may be added behind
// the same interface later.
type StorageConfig struct {
	Backend string `yaml:"backend"`  // "file" (default)
	DataDir string `yaml:"data_dir"` // root for the file backend; e.g. ./data
	// MaxUploadBytes caps a single upload. Defaults to 5 MiB when unset.
	MaxUploadBytes int64 `yaml:"max_upload_bytes"`
	// AllowedTypes is the sniffed-content-type allowlist for uploads. Defaults to
	// common raster image types when unset. SVG is intentionally excluded (XSS).
	AllowedTypes []string `yaml:"allowed_types"`
	// MaxDocumentBytes caps a single document (datasheet/office/Touchstone)
	// upload. Documents are larger than note images, so this defaults to 25 MiB.
	MaxDocumentBytes int64 `yaml:"max_document_bytes"`
	// AllowedDocumentExtensions is the lowercased file-extension allowlist for
	// document uploads (including the leading dot). Office and Touchstone files
	// have no distinctive magic bytes, so documents are classified by extension;
	// non-PDF documents are always served as a download, never inline, so this
	// list can be liberal without inline-execution (XSS) risk. Defaults applied
	// when unset.
	AllowedDocumentExtensions []string `yaml:"allowed_document_extensions"`
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
	Name string `yaml:"name"` // URL-safe slug used in routes and external_identities.provider
	// DisplayName is shown on the login button; defaults to Name when empty.
	DisplayName   string `yaml:"display_name"`
	Issuer        string `yaml:"issuer"`
	ClientID      string `yaml:"client_id"`
	ClientSecret  string `yaml:"client_secret"`
	AutoProvision bool   `yaml:"auto_provision"`
}

// LDAPProvider configures a directory (OpenLDAP / Active Directory) that
// authenticates users by a search-then-bind flow. Unlike OIDC, LDAP login is
// credential-based (no browser redirect): the user's username/password is
// posted to the normal /auth/login endpoint and verified against the directory.
type LDAPProvider struct {
	Name string `yaml:"name"` // slug stored in external_identities.provider
	// DisplayName is informational (shown in admin/UI); defaults to Name.
	DisplayName string `yaml:"display_name"`

	// URL is the directory endpoint, e.g. "ldaps://ldap.corp.example:636" or
	// "ldap://ldap.corp.example:389" (combine with start_tls for StartTLS).
	URL string `yaml:"url"`
	// StartTLS upgrades a plaintext ldap:// connection to TLS via StartTLS.
	// Ignored (and unnecessary) for ldaps:// URLs.
	StartTLS bool `yaml:"start_tls"`
	// InsecureSkipVerify disables TLS certificate verification. Development only.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
	// CACertFile is a PEM file of additional trusted CA certificates, for
	// directories presenting a corporate/self-signed certificate. Verification
	// stays on — prefer this over insecure_skip_verify.
	CACertFile string `yaml:"ca_cert_file"`
	// TimeoutSeconds bounds connect + bind. Defaults to 8.
	TimeoutSeconds int `yaml:"timeout_seconds"`

	// BindDN / BindPassword is the read-only service account used to search for
	// the user before re-binding as them. BindPassword may be overridden by
	// PARTRACK_LDAP_<NAME>_PASSWORD.
	BindDN       string `yaml:"bind_dn"`
	BindPassword string `yaml:"bind_password"`

	// UserBaseDN is the subtree searched for the login user.
	UserBaseDN string `yaml:"user_base_dn"`
	// UserFilter is the search filter with a single %s placeholder for the
	// (escaped) username, e.g. "(uid=%s)", "(mail=%s)", "(sAMAccountName=%s)".
	UserFilter string `yaml:"user_filter"`

	// EmailDomains, when set, restricts this provider to logins whose identifier
	// is an email in one of these domains (e.g. ["institute.com"]). With multiple
	// directories this routes each login to the right one and avoids binding
	// against every directory (latency + lockout risk). A provider with no
	// domains is a catch-all, tried for any identifier.
	EmailDomains []string `yaml:"email_domains"`
	// StripDomain sends only the local part of an email identifier to UserFilter,
	// so a domain-routed login still works with a uid-style filter like (uid=%s).
	StripDomain bool `yaml:"strip_domain"`

	// Attribute names read off the user entry.
	AttrEmail       string `yaml:"attr_email"`        // default "mail"
	AttrDisplayName string `yaml:"attr_display_name"` // default "cn"
	AttrUniqueID    string `yaml:"attr_unique_id"`    // stable subject; default "entryUUID"
	AttrMemberOf    string `yaml:"attr_member_of"`    // group membership attr; default "memberOf"

	// GroupBaseDN enables group resolution by reverse search instead of reading
	// the memberOf attribute — needed when the directory does not populate
	// memberOf (OpenLDAP without the memberof overlay) or to expand nested
	// groups (Active Directory). When set, the user's groups are the DNs of
	// entries under GroupBaseDN matching GroupFilter.
	GroupBaseDN string `yaml:"group_base_dn"`
	// GroupFilter is the search filter for group resolution, with a single %s
	// placeholder for the (escaped) user DN. Defaults to "(member=%s)". For AD
	// nested groups use "(member:1.2.840.113556.1.4.1941:=%s)".
	GroupFilter string `yaml:"group_filter"`

	// RequiredGroup, when set, gates login: only users who are members of this
	// group DN may authenticate (and be provisioned). Anyone who binds
	// successfully but is not a member is denied, exactly like a wrong password.
	// Empty allows any directory user.
	RequiredGroup string `yaml:"required_group"`

	// AutoProvision creates a partrack user on first successful login when no
	// existing user matches the directory email.
	AutoProvision bool `yaml:"auto_provision"`

	// AdminGroup is a group DN; members are granted the instance admin flag.
	// Empty disables directory-driven admin.
	AdminGroup string `yaml:"admin_group"`
	// GroupMappings map a directory group DN to a partrack team + role. On every
	// login, memberships in teams named by any mapping are reconciled to match
	// the user's current directory groups (highest role wins on conflict);
	// teams not referenced by any mapping are left untouched.
	GroupMappings []LDAPGroupMapping `yaml:"group_mappings"`
}

// LDAPGroupMapping binds a directory group to a partrack team membership.
type LDAPGroupMapping struct {
	Group string `yaml:"group"` // group DN (matched case-insensitively)
	Team  string `yaml:"team"`  // partrack team name
	Role  string `yaml:"role"`  // viewer | member | purchaser | admin
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
		Storage: StorageConfig{
			Backend:          "file",
			DataDir:          "./data",
			MaxUploadBytes:   5 << 20,  // 5 MiB
			MaxDocumentBytes: 25 << 20, // 25 MiB
			AllowedTypes:     []string{"image/png", "image/jpeg", "image/webp", "image/gif"},
			AllowedDocumentExtensions: []string{
				".pdf",
				".png", ".jpg", ".jpeg", ".gif", ".webp", // raster images
				".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
				".odt", ".ods", ".odp",
				".csv", ".tsv", ".txt", ".md",
				".zip",
				".s1p", ".s2p", ".s3p", ".s4p", // Touchstone S-parameter files
			},
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
	if v := os.Getenv("PARTRACK_STORAGE_DATA_DIR"); v != "" {
		cfg.Storage.DataDir = v
	}
	if v := os.Getenv("PARTRACK_STORAGE_MAX_UPLOAD_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Storage.MaxUploadBytes = n
		}
	}
	if v := os.Getenv("PARTRACK_STORAGE_MAX_DOCUMENT_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Storage.MaxDocumentBytes = n
		}
	}

	// Per-provider OIDC secret override: PARTRACK_OIDC_<NAME>_SECRET
	for i := range cfg.OIDC {
		envKey := "PARTRACK_OIDC_" + strings.ToUpper(cfg.OIDC[i].Name) + "_SECRET"
		if v := os.Getenv(envKey); v != "" {
			cfg.OIDC[i].ClientSecret = v
		}
	}

	// Per-provider LDAP bind-password override: PARTRACK_LDAP_<NAME>_PASSWORD,
	// plus attribute defaults so a minimal config "just works".
	for i := range cfg.LDAP {
		p := &cfg.LDAP[i]
		envKey := "PARTRACK_LDAP_" + strings.ToUpper(p.Name) + "_PASSWORD"
		if v := os.Getenv(envKey); v != "" {
			p.BindPassword = v
		}
		if p.AttrEmail == "" {
			p.AttrEmail = "mail"
		}
		if p.AttrDisplayName == "" {
			p.AttrDisplayName = "cn"
		}
		if p.AttrUniqueID == "" {
			p.AttrUniqueID = "entryUUID"
		}
		if p.AttrMemberOf == "" {
			p.AttrMemberOf = "memberOf"
		}
		if p.TimeoutSeconds <= 0 {
			p.TimeoutSeconds = 8
		}
		if p.GroupBaseDN != "" && p.GroupFilter == "" {
			p.GroupFilter = "(member=%s)"
		}
		if p.GroupFilter != "" && !strings.Contains(p.GroupFilter, "%s") {
			fmt.Fprintf(os.Stderr, "config: ldap provider %q group_filter must contain a single %%s user-DN placeholder\n", p.Name)
			os.Exit(1)
		}
		if p.Name == "" || p.URL == "" || p.UserBaseDN == "" || p.UserFilter == "" {
			fmt.Fprintf(os.Stderr, "config: ldap provider %d requires name, url, user_base_dn and user_filter\n", i)
			os.Exit(1)
		}
		if !strings.Contains(p.UserFilter, "%s") {
			fmt.Fprintf(os.Stderr, "config: ldap provider %q user_filter must contain a single %%s username placeholder\n", p.Name)
			os.Exit(1)
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
