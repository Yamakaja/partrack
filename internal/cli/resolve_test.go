package cli

import (
	"context"
	"os"
	"strings"
	"testing"
)

// newTestApp builds an app rooted in temporary directories, so tests never read
// or write the developer's real config and cache.
func newTestApp(t *testing.T) *app {
	t.Helper()
	dir := t.TempDir()
	cfg, err := LoadConfig(Overrides{
		URL:       "https://partrack.test",
		ConfigDir: dir + "/config",
		CacheDir:  dir + "/cache",
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return &app{cfg: cfg, format: FormatPlain, out: os.Stdout}
}

func TestResolveResultRef(t *testing.T) {
	a := newTestApp(t)
	a.saveResults("team-1", []PartIdent{
		{ID: "11111111-1111-1111-1111-111111111111", Mpn: "RC0402FR-0710KL"},
		{ID: "22222222-2222-2222-2222-222222222222", Mpn: "ERJ-2RKF1002X"},
	})

	got, err := a.resolveResultRef("@2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("@2 = %q", got)
	}

	for _, bad := range []string{"@0", "@x", "@-1"} {
		if _, err := a.resolveResultRef(bad); err == nil {
			t.Errorf("expected an error for %q", bad)
		} else if ee, ok := err.(*ExitError); !ok || ee.Code != ExitUsage {
			t.Errorf("%q should be a usage error, got %v", bad, err)
		}
	}

	if _, err := a.resolveResultRef("@9"); err == nil {
		t.Error("expected an out-of-range error")
	} else if ee, ok := err.(*ExitError); !ok || ee.Code != ExitNotFound {
		t.Errorf("out of range should be ExitNotFound, got %v", err)
	}
}

// The result set belongs to one server; pointing pt at another must not let @N
// resolve to a part id from the previous one.
func TestResultSetIsScopedToServer(t *testing.T) {
	dir := t.TempDir()
	mk := func(url string) *app {
		cfg, err := LoadConfig(Overrides{URL: url, ConfigDir: dir + "/config", CacheDir: dir + "/cache"})
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		return &app{cfg: cfg, format: FormatPlain, out: os.Stdout}
	}

	mk("https://a.test").saveResults("team-1", []PartIdent{
		{ID: "11111111-1111-1111-1111-111111111111", Mpn: "PART-A"},
	})
	if _, err := mk("https://b.test").resolveResultRef("@1"); err == nil {
		t.Error("@1 resolved against a different server's result set")
	}
}

func TestResolvePrefix(t *testing.T) {
	a := newTestApp(t)
	a.saveResults("team-1", []PartIdent{
		{ID: "3f2a1111-1111-1111-1111-111111111111", Mpn: "PART-ONE"},
		{ID: "3f2a2222-2222-2222-2222-222222222222", Mpn: "PART-TWO"},
		{ID: "abcd3333-3333-3333-3333-333333333333", Mpn: "PART-THREE"},
	})

	got, err := a.resolvePrefix("abcd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "abcd3333-3333-3333-3333-333333333333" {
		t.Errorf("prefix abcd = %q", got)
	}

	// A prefix shared by two known parts must not silently pick one.
	if _, err := a.resolvePrefix("3f2a"); err == nil {
		t.Error("expected an ambiguity error")
	} else if ee, ok := err.(*ExitError); !ok || ee.Code != ExitAmbiguous {
		t.Errorf("want ExitAmbiguous, got %v", err)
	}

	// An unknown prefix yields no id and no error, so the caller can fall back
	// to treating the input as an MPN.
	id, err := a.resolvePrefix("9999")
	if err != nil || id != "" {
		t.Errorf("unknown prefix = (%q, %v), want (\"\", nil)", id, err)
	}

	// Too short to be an id prefix at all.
	if id, err := a.resolvePrefix("3f"); err != nil || id != "" {
		t.Errorf("short prefix = (%q, %v), want (\"\", nil)", id, err)
	}
}

// A full UUID needs no lookup, and must resolve without any network client.
func TestResolvePartAcceptsFullUUID(t *testing.T) {
	a := newTestApp(t)
	const id = "3F2A1111-1111-1111-1111-111111111111"
	got, err := a.resolvePart(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "3f2a1111-1111-1111-1111-111111111111" {
		t.Errorf("uuid = %q, want it lowercased", got)
	}
}

func TestRememberPartDeduplicates(t *testing.T) {
	a := newTestApp(t)
	p := PartIdent{ID: "11111111-1111-1111-1111-111111111111", Mpn: "PART-A"}
	a.rememberPart("team-1", p)
	a.rememberPart("team-1", p)

	s := a.loadState()
	if len(s.Recent) != 1 {
		t.Errorf("recent has %d entries, want 1", len(s.Recent))
	}
}

// saveResults folds rows into the recent roll, which is what makes an id prefix
// from an earlier search still resolve.
func TestSaveResultsMergesIntoRecent(t *testing.T) {
	a := newTestApp(t)
	a.saveResults("team-1", []PartIdent{{ID: "aaaa1111-1111-1111-1111-111111111111", Mpn: "A"}})
	a.saveResults("team-1", []PartIdent{{ID: "bbbb2222-2222-2222-2222-222222222222", Mpn: "B"}})

	s := a.loadState()
	if len(s.Results) != 1 || s.Results[0].Mpn != "B" {
		t.Errorf("results should hold only the newest search: %v", s.Results)
	}
	if len(s.Recent) != 2 {
		t.Fatalf("recent has %d entries, want 2", len(s.Recent))
	}
	if s.Recent[0].Mpn != "B" {
		t.Errorf("newest entry should lead the recent roll, got %q", s.Recent[0].Mpn)
	}
	// The older part must still be addressable by prefix.
	if id, err := a.resolvePrefix("aaaa"); err != nil || id == "" {
		t.Errorf("older part is no longer resolvable by prefix: (%q, %v)", id, err)
	}
}

// An empty search still clears the result set — @N must never resolve into a
// stale one — but the resulting error must not tell someone who just searched
// to go and search.
func TestEmptyResultSetIsDistinguishedFromNoSearch(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.resolveResultRef("@1"); err == nil {
		t.Fatal("expected an error")
	} else if !strings.Contains(err.Error(), "run `pt search` first") {
		t.Errorf("before any search: %v", err)
	}

	a.saveResults("team-1", nil)
	if _, err := a.resolveResultRef("@1"); err == nil {
		t.Fatal("expected an error")
	} else if !strings.Contains(err.Error(), "matched no parts") {
		t.Errorf("after an empty search: %v", err)
	}
}
