package version

import (
	"strings"
	"testing"
)

// Restore the package-level build vars after a test mutates them.
func withBuildVars(t *testing.T, v, c, d string) {
	t.Helper()
	ov, oc, od := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = ov, oc, od })
	Version, Commit, Date = v, c, d
}

// A release build passes the commit through ldflags, deliberately without a
// --dirty suffix, because the Docker build context omits tracked files and so
// looks modified to the toolchain no matter what the repository actually holds.
// Honouring vcs.modified there stamped every deployed binary "-dirty".
func TestLdflagsCommitIsNeverMarkedDirty(t *testing.T) {
	withBuildVars(t, "2026.8.0", "c9fbb6a", "2026-08-26T18:50:00Z")

	got := Get()
	if got.Commit != "c9fbb6a" {
		t.Errorf("Commit = %q, want the ldflags value verbatim", got.Commit)
	}
	if strings.HasSuffix(got.Commit, "-dirty") {
		t.Errorf("a build stamped via ldflags must not be reported dirty: %q", got.Commit)
	}
	if got.Version != "2026.8.0" || got.Date != "2026-08-26T18:50:00Z" {
		t.Errorf("ldflags values not preserved: %+v", got)
	}
}

// An unstamped build still has to say something useful rather than empty
// strings, and this is the path where the toolchain's dirty flag is meaningful.
func TestUnstampedBuildFallsBackToSentinels(t *testing.T) {
	withBuildVars(t, "", "", "")

	got := Get()
	if got.Version != "dev" {
		// `go test` records vcs stamps for the module under test, so only
		// Version is reliably a sentinel here; Commit may come from build info.
		t.Errorf("Version = %q, want the dev sentinel", got.Version)
	}
	for _, f := range []string{got.Version, got.Commit, got.Date} {
		if f == "" {
			t.Errorf("no field may be empty: %+v", got)
		}
	}
}
