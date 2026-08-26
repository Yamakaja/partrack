package bomfile

import (
	"os"
	"regexp"
	"testing"
)

// The header table here is a hand-port of AUTO_MAP in the import wizard
// (web/src/lib/components/bom/bom-import-utils.ts). The two being the same is
// the whole premise of `pt bom check`: if the CLI decides a column means
// something the wizard does not, the check predicts an import that will not
// happen — the most expensive way to be wrong.
//
// Unlike the response types, this pair cannot share a declaration across the
// language boundary, so the next best thing is a test that reads the TypeScript
// and fails when they diverge.
const wizardAutoMapPath = "../../web/src/lib/components/bom/bom-import-utils.ts"

var (
	tsBlockRe = regexp.MustCompile(`(?s)export const AUTO_MAP[^{]*\{(.*?)\n\};`)
	tsEntryRe = regexp.MustCompile(`(?m)^\s*'?([^':\n]+?)'?\s*:\s*'([a-z]+)'`)
)

func TestAutoMapMatchesTheImportWizard(t *testing.T) {
	src, err := os.ReadFile(wizardAutoMapPath)
	if err != nil {
		// A checkout without the frontend is a legitimate way to build the CLI.
		t.Skipf("wizard source not available (%v)", err)
	}
	block := tsBlockRe.FindSubmatch(src)
	if block == nil {
		t.Fatalf("could not find AUTO_MAP in %s — if it was renamed, update this test "+
			"rather than deleting it: the tables still have to agree", wizardAutoMapPath)
	}

	wizard := map[string]ColumnRole{}
	for _, m := range tsEntryRe.FindAllSubmatch(block[1], -1) {
		wizard[string(m[1])] = ColumnRole(m[2])
	}
	if len(wizard) == 0 {
		t.Fatal("parsed no entries from AUTO_MAP; the test's regex needs updating")
	}

	for header, want := range wizard {
		if got, ok := autoMap[header]; !ok {
			t.Errorf("the wizard maps %q to %q; bomfile does not know it", header, want)
		} else if got != want {
			t.Errorf("%q: wizard says %q, bomfile says %q", header, want, got)
		}
	}
	for header, got := range autoMap {
		if _, ok := wizard[header]; !ok {
			t.Errorf("bomfile maps %q to %q; the wizard does not know it, so an import "+
				"would leave that column ignored", header, got)
		}
	}
}
