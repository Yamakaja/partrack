package changelog

import "testing"

func versions(rs []Release) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Version
	}
	return out
}

func TestSince(t *testing.T) {
	releases = []Release{
		{Version: "2026.7.1"},
		{Version: "2026.7.0"},
		{Version: "2026.6.0"},
	}

	if got := versions(Since("2026.6.0")); len(got) != 2 || got[0] != "2026.7.1" || got[1] != "2026.7.0" {
		t.Fatalf("Since(2026.6.0) = %v, want [2026.7.1 2026.7.0]", got)
	}
	if got := Since("2026.7.1"); len(got) != 0 {
		t.Fatalf("Since(latest) = %v, want none", versions(got))
	}
	if got := Since(""); len(got) != 3 {
		t.Fatalf("Since(empty) = %v, want all 3", versions(got))
	}
	// A `git describe` suffix is tolerated — compared by its base version.
	if got := Since("2026.6.0-3-gabc123"); len(got) != 2 {
		t.Fatalf("Since(describe suffix) = %v, want 2", versions(got))
	}
	// An unparseable `since` yields all releases.
	if got := Since("garbage"); len(got) != 3 {
		t.Fatalf("Since(garbage) = %v, want all 3", versions(got))
	}
}

func TestParseCalVer(t *testing.T) {
	cases := map[string]bool{
		"2026.6.0":          true,
		"2026.12.34":        true,
		"2026.6.0-3-gabc12": true,
		"dev":               false,
		"2026.6":            false,
		"v2026.6.0":         false,
	}
	for in, wantOK := range cases {
		if _, ok := parseCalVer(in); ok != wantOK {
			t.Errorf("parseCalVer(%q) ok=%v, want %v", in, ok, wantOK)
		}
	}
}
