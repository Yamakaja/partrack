// Package changelog exposes the user-facing changelog embedded into the binary.
//
// The embedded changelog.json is generated from git history by
// tools/genchangelog (run in the Docker build, where .git is present). A
// committed placeholder of `[]` keeps local `go build`/tests compiling; the real
// data is produced at build time. See doc/commit-convention.md for the
// `Changelog:` trailer convention the entries come from.
package changelog

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"
)

//go:embed changelog.json
var data []byte

// Release is one CalVer release and its curated user-facing entries.
type Release struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Entries []string `json:"entries"`
}

// releases is newest-first (as emitted by the generator).
var releases = []Release{}

func init() {
	_ = json.Unmarshal(data, &releases) // placeholder `[]` => empty
}

// All returns every release, newest first.
func All() []Release { return releases }

// Since returns the releases strictly newer than the given version, newest
// first. An empty or unparseable `since` yields all releases.
func Since(since string) []Release {
	base, ok := parseCalVer(since)
	if !ok {
		return releases
	}
	out := []Release{}
	for _, r := range releases {
		v, ok := parseCalVer(r.Version)
		if ok && lessCalVer(base, v) {
			out = append(out, r)
		}
	}
	return out
}

// parseCalVer extracts the leading YEAR.MONTH.PATCH from s, ignoring any
// `git describe` suffix (e.g. "2026.6.0-3-gabc123" -> {2026,6,0}).
func parseCalVer(s string) ([3]int, bool) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

func lessCalVer(a, b [3]int) bool {
	for i := range 3 {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
