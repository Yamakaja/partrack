package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Config is the resolved invocation context: where the server is, who we are,
// and which team we act as. Resolution precedence is
// defaults < config file < environment < command flags.
type Config struct {
	URL   string `json:"url,omitempty"`
	Team  string `json:"team,omitempty"` // name or UUID; resolved lazily
	Token string `json:"-"`              // never written to config.json

	ConfigDir string `json:"-"`
	CacheDir  string `json:"-"`

	// TokenSource records where Token came from, for `pt auth status`.
	TokenSource string `json:"-"`
}

// Overrides carries the flag-level values that win over file and environment.
type Overrides struct {
	URL       string
	Team      string
	ConfigDir string
	CacheDir  string
}

// credentials is the on-disk shape of the 0600 token file. It is separate from
// config.json so the config file never holds a secret and can be shared.
type credentials struct {
	Token string `json:"token"`
	URL   string `json:"url,omitempty"`  // the server this token was minted against
	User  string `json:"user,omitempty"` // display email, for `auth status`
	ID    string `json:"id,omitempty"`   // token id, so `auth logout` can revoke it
}

const (
	configFileName = "config.json"
	credsFileName  = "credentials.json"
)

// LoadConfig resolves configuration from all sources.
func LoadConfig(o Overrides) (*Config, error) {
	c := &Config{}

	dir, err := resolveDir(o.ConfigDir, "PT_CONFIG_DIR", "XDG_CONFIG_HOME", ".config")
	if err != nil {
		return nil, err
	}
	c.ConfigDir = dir

	cacheDir, err := resolveDir(o.CacheDir, "PT_CACHE_DIR", "XDG_CACHE_HOME", ".cache")
	if err != nil {
		return nil, err
	}
	c.CacheDir = cacheDir

	// Earlier builds used os.UserConfigDir, which on macOS is
	// ~/Library/Application Support — not where a CLI belongs, and not where dks
	// keeps its state. Move anything left there so an existing login survives.
	if o.ConfigDir == "" {
		migrateLegacyDir(legacyDir(os.UserConfigDir), c.ConfigDir)
	}
	if o.CacheDir == "" {
		migrateLegacyDir(legacyDir(os.UserCacheDir), c.CacheDir)
	}

	// File.
	if err := readJSONFile(filepath.Join(c.ConfigDir, configFileName), c); err != nil {
		return nil, err
	}

	// Credentials file (token + the URL it belongs to).
	var creds credentials
	if err := readJSONFile(filepath.Join(c.ConfigDir, credsFileName), &creds); err != nil {
		return nil, err
	}
	if creds.Token != "" {
		c.Token = creds.Token
		c.TokenSource = "config"
	}
	if c.URL == "" {
		c.URL = creds.URL
	}

	// Environment.
	if v := os.Getenv("PARTRACK_URL"); v != "" {
		c.URL = v
	}
	if v := os.Getenv("PARTRACK_TEAM"); v != "" {
		c.Team = v
	}
	if v := os.Getenv("PARTRACK_TOKEN"); v != "" {
		c.Token = v
		c.TokenSource = "environment"
	}

	// Flags.
	if o.URL != "" {
		c.URL = o.URL
	}
	if o.Team != "" {
		c.Team = o.Team
	}

	c.URL = strings.TrimRight(c.URL, "/")
	return c, nil
}

// resolveDir picks an explicit override, then the tool's own environment
// variable, then the XDG variable, then ~/<fallback>/partrack. This is the same
// ladder dks uses, so both tools keep their state in the same place on every
// platform rather than following per-OS conventions.
func resolveDir(override, envKey, xdgKey, fallback string) (string, error) {
	if override != "" {
		return override, nil
	}
	if v := os.Getenv(envKey); v != "" {
		return v, nil
	}
	if v := os.Getenv(xdgKey); v != "" {
		return filepath.Join(v, "partrack"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", envKey, err)
	}
	return filepath.Join(home, fallback, "partrack"), nil
}

// legacyDir returns the pre-XDG location, or "" when it cannot be determined.
func legacyDir(base func() (string, error)) string {
	b, err := base()
	if err != nil {
		return ""
	}
	return filepath.Join(b, "partrack")
}

// migrateLegacyDir moves files from an old state directory into the current one,
// once. It is best-effort: a failure leaves the old files untouched and simply
// means the user logs in again, which is better than reporting an error for
// something they never asked for. Existing files at the destination win.
func migrateLegacyDir(from, to string) {
	if from == "" || from == to {
		return
	}
	entries, err := os.ReadDir(from)
	if err != nil || len(entries) == 0 {
		return
	}
	if err := os.MkdirAll(to, 0o700); err != nil {
		return
	}
	moved := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dst := filepath.Join(to, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue // never clobber current state
		}
		if os.Rename(filepath.Join(from, e.Name()), dst) == nil {
			moved++
		}
	}
	if moved > 0 {
		os.Remove(from) //nolint:errcheck // only succeeds once empty
		fmt.Fprintf(os.Stderr, "pt: moved %d state file(s) from %s to %s\n", moved, from, to)
	}
}

// readJSONFile decodes path into v. A missing file is not an error — it just
// leaves v untouched.
func readJSONFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	return nil
}

// writeFileAtomic writes data to path via a temp file + rename, so a crash
// mid-write cannot leave a truncated config or a half-written token.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck // no-op once the rename succeeds

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// Save persists the non-secret settings to config.json.
func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(c.ConfigDir, configFileName), append(data, '\n'), 0o644)
}

// saveCredentials writes the token file with 0600 permissions.
func (c *Config) saveCredentials(creds credentials) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(c.ConfigDir, credsFileName), append(data, '\n'), 0o600)
}

// clearCredentials removes the stored token.
func (c *Config) clearCredentials() error {
	err := os.Remove(filepath.Join(c.ConfigDir, credsFileName))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// configPath returns the path of the config file.
func (c *Config) configPath() string {
	return filepath.Join(c.ConfigDir, configFileName)
}
