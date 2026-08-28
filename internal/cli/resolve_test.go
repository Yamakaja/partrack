package cli

import (
	"context"
	"os"
	"strconv"
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
	a.saveResults("team-1", kindPart, []Ref{
		{Kind: kindPart, ID: "11111111-1111-1111-1111-111111111111", Label: "RC0402FR-0710KL"},
		{Kind: kindPart, ID: "22222222-2222-2222-2222-222222222222", Label: "ERJ-2RKF1002X"},
	})

	got, err := a.resolveResultRef("@2", kindPart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("@2 = %q", got)
	}

	for _, bad := range []string{"@0", "@x", "@-1"} {
		if _, err := a.resolveResultRef(bad, kindPart); err == nil {
			t.Errorf("expected an error for %q", bad)
		} else if ee, ok := err.(*ExitError); !ok || ee.Code != ExitUsage {
			t.Errorf("%q should be a usage error, got %v", bad, err)
		}
	}

	if _, err := a.resolveResultRef("@9", kindPart); err == nil {
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

	mk("https://a.test").saveResults("team-1", kindPart, []Ref{
		{Kind: kindPart, ID: "11111111-1111-1111-1111-111111111111", Label: "PART-A"},
	})
	if _, err := mk("https://b.test").resolveResultRef("@1", kindPart); err == nil {
		t.Error("@1 resolved against a different server's result set")
	}
}

func TestResolvePrefix(t *testing.T) {
	a := newTestApp(t)
	a.saveResults("team-1", kindPart, []Ref{
		{Kind: kindPart, ID: "3f2a1111-1111-1111-1111-111111111111", Label: "PART-ONE"},
		{Kind: kindPart, ID: "3f2a2222-2222-2222-2222-222222222222", Label: "PART-TWO"},
		{Kind: kindPart, ID: "abcd3333-3333-3333-3333-333333333333", Label: "PART-THREE"},
	})

	got, err := a.resolvePrefix("abcd", kindPart)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "abcd3333-3333-3333-3333-333333333333" {
		t.Errorf("prefix abcd = %q", got)
	}

	// A prefix shared by two known parts must not silently pick one.
	if _, err := a.resolvePrefix("3f2a", kindPart); err == nil {
		t.Error("expected an ambiguity error")
	} else if ee, ok := err.(*ExitError); !ok || ee.Code != ExitAmbiguous {
		t.Errorf("want ExitAmbiguous, got %v", err)
	}

	// An unknown prefix yields no id and no error, so the caller can fall back
	// to treating the input as an MPN.
	id, err := a.resolvePrefix("9999", kindPart)
	if err != nil || id != "" {
		t.Errorf("unknown prefix = (%q, %v), want (\"\", nil)", id, err)
	}

	// Too short to be an id prefix at all.
	if id, err := a.resolvePrefix("3f", kindPart); err != nil || id != "" {
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
	p := Ref{Kind: kindPart, ID: "11111111-1111-1111-1111-111111111111", Label: "PART-A"}
	a.remember("team-1", p)
	a.remember("team-1", p)

	s := a.loadState()
	if len(s.Recent) != 1 {
		t.Errorf("recent has %d entries, want 1", len(s.Recent))
	}
}

// saveResults folds rows into the recent roll, which is what makes an id prefix
// from an earlier search still resolve.
func TestSaveResultsMergesIntoRecent(t *testing.T) {
	a := newTestApp(t)
	a.saveResults("team-1", kindPart, []Ref{{Kind: kindPart, ID: "aaaa1111-1111-1111-1111-111111111111", Label: "A"}})
	a.saveResults("team-1", kindPart, []Ref{{Kind: kindPart, ID: "bbbb2222-2222-2222-2222-222222222222", Label: "B"}})

	s := a.loadState()
	if len(s.Results) != 1 || s.Results[0].Label != "B" {
		t.Errorf("results should hold only the newest search: %v", s.Results)
	}
	if len(s.Recent) != 2 {
		t.Fatalf("recent has %d entries, want 2", len(s.Recent))
	}
	if s.Recent[0].Label != "B" {
		t.Errorf("newest entry should lead the recent roll, got %q", s.Recent[0].Label)
	}
	// The older part must still be addressable by prefix.
	if id, err := a.resolvePrefix("aaaa", kindPart); err != nil || id == "" {
		t.Errorf("older part is no longer resolvable by prefix: (%q, %v)", id, err)
	}
}

// An empty search still clears the result set — @N must never resolve into a
// stale one — but the resulting error must not tell someone who just searched
// to go and search.
func TestEmptyResultSetIsDistinguishedFromNoSearch(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.resolveResultRef("@1", kindPart); err == nil {
		t.Fatal("expected an error")
	} else if !strings.Contains(err.Error(), "run `pt search` first") {
		t.Errorf("before any search: %v", err)
	}

	a.saveResults("team-1", kindPart, nil)
	if _, err := a.resolveResultRef("@1", kindPart); err == nil {
		t.Fatal("expected an error")
	} else if !strings.Contains(err.Error(), "matched nothing") {
		t.Errorf("after an empty search: %v", err)
	}
}

// @N is scoped to the kind of the last listing. Without the check, `pt orders
// show @1` right after a search would fetch an order by a part's id — a lookup
// that either 404s confusingly or, worse, hits an unrelated object.
func TestResultRefRejectsAWrongKind(t *testing.T) {
	a := newTestApp(t)
	a.saveResults("team-1", kindOrder, []Ref{
		{Kind: kindOrder, ID: "11111111-1111-1111-1111-111111111111", Label: "PO-42"},
	})

	if _, err := a.resolveResultRef("@1", kindPart); err == nil {
		t.Fatal("@1 resolved a part against an order result set")
	} else if ee, ok := err.(*ExitError); !ok || ee.Code != ExitUsage {
		t.Errorf("kind mismatch should be a usage error, got %v", err)
	} else if !strings.Contains(err.Error(), "pt search") {
		t.Errorf("the error must name the command that fixes it: %v", err)
	}

	if got, err := a.resolveResultRef("@1", kindOrder); err != nil || got == "" {
		t.Errorf("@1 should still resolve as an order: (%q, %v)", got, err)
	}
}

// An id prefix must not match across kinds either — the recent roll holds parts
// and orders side by side.
func TestResolvePrefixIsScopedToKind(t *testing.T) {
	a := newTestApp(t)
	a.saveResults("team-1", kindOrder, []Ref{
		{Kind: kindOrder, ID: "3f2a1111-1111-1111-1111-111111111111", Label: "PO-42"},
	})

	if got, err := a.resolvePrefix("3f2a", kindPart); err != nil || got != "" {
		t.Errorf("an order prefix matched as a part: (%q, %v)", got, err)
	}
	if got, err := a.resolvePrefix("3f2a", kindOrder); err != nil || got == "" {
		t.Errorf("the order prefix should resolve: (%q, %v)", got, err)
	}
}

// The pre-kind state file stored parts as {id, mpn, manufacturer}. Upgrading pt
// must not silently drop the recent roll a user had already built up.
func TestLegacyStateFileDecodesAsParts(t *testing.T) {
	a := newTestApp(t)
	legacy := `{"url":` + strconv.Quote(a.cfg.URL) + `,"team_id":"team-1",
	  "results":[{"id":"11111111-1111-1111-1111-111111111111","mpn":"PART-A","manufacturer":"Acme"}],
	  "recent":[{"id":"11111111-1111-1111-1111-111111111111","mpn":"PART-A","manufacturer":"Acme"}]}`
	if err := writeFileAtomic(a.statePath(), []byte(legacy), 0o644); err != nil {
		t.Fatalf("seeding the legacy state file: %v", err)
	}

	s := a.loadState()
	if len(s.Recent) != 1 || s.Recent[0].Kind != kindPart {
		t.Fatalf("legacy entry did not decode as a part: %+v", s.Recent)
	}
	if s.Recent[0].Label != "PART-A" {
		t.Errorf("legacy mpn should become the label, got %q", s.Recent[0].Label)
	}
	if got, err := a.resolveResultRef("@1", kindPart); err != nil || got == "" {
		t.Errorf("@1 should still resolve from a legacy state file: (%q, %v)", got, err)
	}
}
