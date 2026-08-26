package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Precedence is defaults < config file < environment < flags. Each layer must
// override the one before it and nothing else.
func TestConfigPrecedence(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(configFileName, `{"url":"https://file.test","team":"File Team"}`)

	t.Run("file only", func(t *testing.T) {
		cfg, err := LoadConfig(Overrides{ConfigDir: configDir, CacheDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.URL != "https://file.test" || cfg.Team != "File Team" {
			t.Errorf("cfg = %+v", cfg)
		}
	})

	t.Run("environment beats file", func(t *testing.T) {
		t.Setenv("PARTRACK_URL", "https://env.test")
		t.Setenv("PARTRACK_TEAM", "Env Team")
		cfg, err := LoadConfig(Overrides{ConfigDir: configDir, CacheDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.URL != "https://env.test" || cfg.Team != "Env Team" {
			t.Errorf("cfg = %+v", cfg)
		}
	})

	t.Run("flags beat environment", func(t *testing.T) {
		t.Setenv("PARTRACK_URL", "https://env.test")
		cfg, err := LoadConfig(Overrides{
			URL: "https://flag.test", Team: "Flag Team", ConfigDir: configDir, CacheDir: dir,
		})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.URL != "https://flag.test" || cfg.Team != "Flag Team" {
			t.Errorf("cfg = %+v", cfg)
		}
	})

	t.Run("trailing slash is trimmed", func(t *testing.T) {
		cfg, err := LoadConfig(Overrides{URL: "https://x.test/", ConfigDir: configDir, CacheDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.URL != "https://x.test" {
			t.Errorf("url = %q", cfg.URL)
		}
	})
}

func TestCredentialsAreReadSeparatelyAndNeverSaved(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")

	cfg, err := LoadConfig(Overrides{URL: "https://x.test", ConfigDir: configDir, CacheDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.saveCredentials(credentials{
		Token: "ptk_secret", URL: "https://x.test", User: "a@b.test", ID: "tok-1",
	}); err != nil {
		t.Fatal(err)
	}

	// The token file must not be world- or group-readable.
	info, err := os.Stat(filepath.Join(configDir, credsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credentials mode = %o, want 600", perm)
	}

	// config.json must never carry the secret.
	cfg.Token = "ptk_secret"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfg.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ptk_secret") {
		t.Errorf("config.json leaked the token: %s", data)
	}

	// A fresh load picks the token back up from the credentials file.
	reloaded, err := LoadConfig(Overrides{ConfigDir: configDir, CacheDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Token != "ptk_secret" {
		t.Errorf("token = %q, want it restored from credentials.json", reloaded.Token)
	}
	if reloaded.TokenSource != "config" {
		t.Errorf("token source = %q", reloaded.TokenSource)
	}

	if err := reloaded.clearCredentials(); err != nil {
		t.Fatal(err)
	}
	// Clearing twice must not fail — logout is idempotent.
	if err := reloaded.clearCredentials(); err != nil {
		t.Errorf("second clear failed: %v", err)
	}
}

func TestEnvTokenOverridesStoredToken(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	cfg, err := LoadConfig(Overrides{URL: "https://x.test", ConfigDir: configDir, CacheDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.saveCredentials(credentials{Token: "ptk_stored"}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PARTRACK_TOKEN", "ptk_from_env")
	reloaded, err := LoadConfig(Overrides{ConfigDir: configDir, CacheDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Token != "ptk_from_env" {
		t.Errorf("token = %q, want the environment value", reloaded.Token)
	}
	if reloaded.TokenSource != "environment" {
		t.Errorf("token source = %q, want environment", reloaded.TokenSource)
	}
}

func TestConfigGetSet(t *testing.T) {
	cfg := &Config{}
	if err := cfg.set("url", "https://x.test/"); err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "https://x.test" {
		t.Errorf("url = %q, want the trailing slash trimmed", cfg.URL)
	}
	if v, err := cfg.get("url"); err != nil || v != "https://x.test" {
		t.Errorf("get url = (%q, %v)", v, err)
	}
	if err := cfg.set("token", "ptk_nope"); err == nil {
		t.Error("token must not be settable through `pt config set`")
	}
	if _, err := cfg.get("bogus"); err == nil {
		t.Error("expected an error for an unknown key")
	}
}

// Config and cache live where dks keeps its state — XDG paths with a ~/.config
// and ~/.cache fallback — rather than the per-OS location os.UserConfigDir
// returns (~/Library/Application Support on macOS).
func TestResolveDirFollowsXDG(t *testing.T) {
	t.Run("tool env wins", func(t *testing.T) {
		t.Setenv("PT_CONFIG_DIR", "/explicit")
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		got, err := resolveDir("", "PT_CONFIG_DIR", "XDG_CONFIG_HOME", ".config")
		if err != nil || got != "/explicit" {
			t.Errorf("got (%q, %v)", got, err)
		}
	})

	t.Run("xdg next", func(t *testing.T) {
		t.Setenv("PT_CONFIG_DIR", "")
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		got, err := resolveDir("", "PT_CONFIG_DIR", "XDG_CONFIG_HOME", ".config")
		if err != nil || got != filepath.Join("/xdg", "partrack") {
			t.Errorf("got (%q, %v)", got, err)
		}
	})

	t.Run("home fallback", func(t *testing.T) {
		t.Setenv("PT_CONFIG_DIR", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "/home/tester")
		got, err := resolveDir("", "PT_CONFIG_DIR", "XDG_CONFIG_HOME", ".config")
		if err != nil || got != filepath.Join("/home/tester", ".config", "partrack") {
			t.Errorf("got (%q, %v)", got, err)
		}
	})

	t.Run("explicit override beats everything", func(t *testing.T) {
		t.Setenv("PT_CONFIG_DIR", "/env")
		got, err := resolveDir("/flag", "PT_CONFIG_DIR", "XDG_CONFIG_HOME", ".config")
		if err != nil || got != "/flag" {
			t.Errorf("got (%q, %v)", got, err)
		}
	})
}

// A login made by an older build must survive the path change.
func TestMigrateLegacyDirMovesCredentials(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "legacy")
	current := filepath.Join(root, "current")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, credsFileName), []byte(`{"token":"ptk_old"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	migrateLegacyDir(old, current)

	data, err := os.ReadFile(filepath.Join(current, credsFileName))
	if err != nil {
		t.Fatalf("credentials were not migrated: %v", err)
	}
	if !strings.Contains(string(data), "ptk_old") {
		t.Errorf("migrated file has wrong content: %s", data)
	}
	if _, err := os.Stat(filepath.Join(old, credsFileName)); err == nil {
		t.Error("the legacy file should have been moved, not copied")
	}
}

// Migration must never overwrite state the current location already holds.
func TestMigrateLegacyDirNeverClobbers(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "legacy")
	current := filepath.Join(root, "current")
	for _, d := range []string{old, current} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(old, credsFileName), []byte(`{"token":"ptk_old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(current, credsFileName), []byte(`{"token":"ptk_current"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	migrateLegacyDir(old, current)

	data, err := os.ReadFile(filepath.Join(current, credsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ptk_current") {
		t.Errorf("migration clobbered current credentials: %s", data)
	}
}
