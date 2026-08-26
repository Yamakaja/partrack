// Package version exposes build-time version metadata for the running binary.
//
// The exported vars are populated at build time via the linker:
//
//	-ldflags "-X github.com/Yamakaja/partrack/internal/version.Version=2026.6.0 ..."
//
// (see docker/Dockerfile and .github/workflows/release.yml). partrack versions
// follow CalVer (YEAR.MONTH.PATCH, e.g. "2026.6.0"); untagged builds use the
// `git describe` output (e.g. "2026.6.0-3-gb2170a1").
//
// A plain `go build` / `go run` leaves the vars empty; Get() then falls back to
// the VCS stamps the Go toolchain records in the build info, so local/dev builds
// still report a commit and timestamp.
package version

import "runtime/debug"

// Set via -ldflags -X at build time. Empty for un-stamped local builds.
var (
	Version = ""
	Commit  = ""
	Date    = ""
)

// Info is the JSON shape returned by GET /api/v1/version.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Get resolves the build metadata, preferring the ldflags-injected values and
// falling back to the toolchain's VCS build stamps. Anything still unknown is
// reported as a sentinel ("dev"/"unknown") rather than an empty string.
func Get() Info {
	v, c, d := Version, Commit, Date

	// The dirty marker belongs only to the fallback path. A release build passes
	// the commit via ldflags from `git rev-parse`, deliberately without --dirty,
	// because the Docker build context omits tracked files (.dockerignore drops
	// config.yaml, doc/, *.md) and carries untracked deployment files — so the
	// toolchain's vcs.modified is true for every containerised build, whatever
	// the real repository state. Honouring it there marked every deployed binary
	// "-dirty" and made the version endpoint useless for the one question it is
	// asked: is this a clean build of a known commit?
	fromLdflags := c != ""

	if bi, ok := debug.ReadBuildInfo(); ok {
		var dirty bool
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if c == "" {
					c = s.Value
					if len(c) > 12 {
						c = c[:12]
					}
				}
			case "vcs.time":
				if d == "" {
					d = s.Value
				}
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if dirty && c != "" && !fromLdflags {
			c += "-dirty"
		}
	}

	if v == "" {
		v = "dev"
	}
	if c == "" {
		c = "unknown"
	}
	if d == "" {
		d = "unknown"
	}
	return Info{Version: v, Commit: c, Date: d}
}
