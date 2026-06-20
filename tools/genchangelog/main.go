// Command genchangelog renders the embedded user-facing changelog
// (internal/changelog/changelog.json) from git history.
//
// It implements the strict opt-in convention from doc/commit-convention.md: a
// commit contributes an entry only if it carries a `Changelog: <text>` trailer
// (and not `Changelog: skip`). Entries are grouped by CalVer release tag
// (YEAR.MONTH.PATCH) and emitted newest-release-first:
//
//	[{"version":"2026.6.0","date":"2026-06-20","entries":["..."]}]
//
// It runs in the Docker build (where .git is present) before `go build`, and can
// be run locally to refresh the file for testing. It shells out to git only and
// needs no network. It reads refs/objects (tags, log), never the worktree, so it
// is unaffected by files the build context omits via .dockerignore.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type release struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Entries []string `json:"entries"`
}

var calver = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func main() {
	out := flag.String("o", "internal/changelog/changelog.json", "output path")
	flag.Parse()

	tags, err := calverTags()
	if err != nil {
		fail(err)
	}

	releases := make([]release, 0, len(tags))
	for i, tag := range tags {
		rangeSpec := tag
		if i > 0 {
			rangeSpec = tags[i-1] + ".." + tag
		}
		entries, err := entriesIn(rangeSpec)
		if err != nil {
			fail(err)
		}
		date, err := git("log", "-1", "--format=%cs", tag+"^{commit}")
		if err != nil {
			fail(err)
		}
		releases = append(releases, release{Version: tag, Date: strings.TrimSpace(date), Entries: entries})
	}
	// Emit newest first.
	reverse(releases)

	b, err := json.MarshalIndent(releases, "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "genchangelog: wrote %d release(s) to %s\n", len(releases), *out)
}

// calverTags returns CalVer tags sorted oldest-first.
func calverTags() ([]string, error) {
	raw, err := git("tag", "--list")
	if err != nil {
		return nil, err
	}
	var tags []string
	for t := range strings.SplitSeq(raw, "\n") {
		if t = strings.TrimSpace(t); calver.MatchString(t) {
			tags = append(tags, t)
		}
	}
	sort.Slice(tags, func(i, j int) bool { return lessVer(tags[i], tags[j]) })
	return tags, nil
}

// entriesIn returns the (non-skip) Changelog trailer values for commits in the
// given revision range, newest commit first.
//
// git parses the trailers itself (the footer block only), so a "changelog:"
// line appearing in body prose is not mistaken for a trailer. One commit per
// NUL; multiple Changelog values within a commit are separated by RS (0x1e).
func entriesIn(rangeSpec string) ([]string, error) {
	out, err := git("log", rangeSpec, "--format=%(trailers:key=Changelog,valueonly=true,separator=%x1e)%x00")
	if err != nil {
		return nil, err
	}
	var entries []string
	for commit := range strings.SplitSeq(out, "\x00") {
		for v := range strings.SplitSeq(commit, "\x1e") {
			if v = strings.TrimSpace(v); v == "" || strings.EqualFold(v, "skip") {
				continue
			}
			entries = append(entries, v)
		}
	}
	return entries, nil
}

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func lessVer(a, b string) bool {
	pa, pb := parse(a), parse(b)
	for i := range 3 {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func parse(s string) [3]int {
	var out [3]int
	for i, p := range strings.SplitN(s, ".", 3) {
		if i > 2 {
			break
		}
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

func reverse(r []release) {
	for l, h := 0, len(r)-1; l < h; l, h = l+1, h-1 {
		r[l], r[h] = r[h], r[l]
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "genchangelog: %v\n", err)
	os.Exit(1)
}
