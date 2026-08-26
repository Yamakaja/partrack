package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// resolvePreview posts a BOM to the preview endpoint and returns the decoded
// body, failing the test on any non-200.
func resolvePreview(t *testing.T, teamID, token string, req map[string]any) map[string]any {
	t.Helper()
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/teams/"+teamID+"/bom/resolve-preview", req, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	return body
}

func previewItems(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, _ := body["items"].([]any)
	out := make([]map[string]any, len(raw))
	for i, r := range raw {
		out[i], _ = r.(map[string]any)
	}
	return out
}

// The preview's job is to report which tier each line would hit. This walks a
// BOM with one line per outcome: an exact MPN in the catalog, an MPN in the
// catalog twice, an MPN nobody has, and a line with no identifiers at all.
func TestResolvePreviewReportsTierPerLine(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "resolve@example.com", "pw", "Resolve")
	token := testutil.CreateSession(t, testDB, userID)
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Resolve Team")

	testutil.CreatePart(t, testDB, "team", "RC0402FR-0710KL", &teamID)
	// The same MPN twice: an ambiguity the importer refuses to resolve by picking.
	testutil.CreatePart(t, testDB, "team", "GRM155R71C104KA88D", &teamID)
	testutil.CreatePart(t, testDB, "team", "GRM155R71C104KA88D", &teamID)

	body := resolvePreview(t, teamID, token, map[string]any{
		"items": []map[string]any{
			{"reference_designators": "R1", "quantity": 1, "mpn": "RC0402FR-0710KL"},
			{"reference_designators": "C1", "quantity": 1, "mpn": "GRM155R71C104KA88D"},
			{"reference_designators": "U1", "quantity": 1, "mpn": "NOBODY-HAS-THIS"},
			{"reference_designators": "R2", "quantity": 1, "value": "10k", "footprint": "R_0603_1608Metric"},
		},
	})

	items := previewItems(t, body)
	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(items))
	}

	if got := items[0]["state"]; got != "auto_mpn" {
		t.Errorf("exact MPN: state = %v, want auto_mpn", got)
	}
	if items[0]["part"] == nil {
		t.Errorf("a resolved line must name the part it resolved to: %v", items[0])
	}

	if got := items[1]["state"]; got != "ambiguous" {
		t.Errorf("duplicate MPN: state = %v, want ambiguous", got)
	}
	cands, _ := items[1]["candidates"].([]any)
	if len(cands) != 2 {
		t.Errorf("an ambiguous line must carry what it was choosing between, got %v", items[1]["candidates"])
	}

	// An unknown MPN is not a dead end: the distributor pipeline can go and find it.
	if got := items[2]["state"]; got != "importing" {
		t.Errorf("unknown MPN: state = %v, want importing", got)
	}

	// No identifier and nothing in the catalog to match parametrically.
	if got := items[3]["state"]; got != "" {
		t.Errorf("unmatchable parametric line: state = %v, want \"\"", got)
	}
	if got := items[3]["route"]; got != "parametric" {
		t.Errorf("the interpretation should still be reported: route = %v", got)
	}

	summary, _ := body["summary"].(map[string]any)
	for state, want := range map[string]float64{"auto_mpn": 1, "ambiguous": 1, "importing": 1, "": 1} {
		if got, _ := summary[state].(float64); got != want {
			t.Errorf("summary[%q] = %v, want %v", state, got, want)
		}
	}
}

// A preview must not become an import. Nothing may be written — no revision, no
// line items, and above all no part_resolve_items, which would fan out to
// distributor APIs for a BOM nobody has committed to importing.
func TestResolvePreviewWritesNothing(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "nowrite@example.com", "pw", "NoWrite")
	token := testutil.CreateSession(t, testDB, userID)
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "NoWrite Team")
	partID := testutil.CreatePart(t, testDB, "team", "RC0402FR-0710KL", &teamID)

	resolvePreview(t, teamID, token, map[string]any{
		"items": []map[string]any{
			{"reference_designators": "R1", "quantity": 1, "mpn": "RC0402FR-0710KL"},
			{"reference_designators": "U1", "quantity": 1, "mpn": "NOBODY-HAS-THIS"},
		},
	})

	for _, table := range []string{"bom_revisions", "bom_line_items", "part_resolve_items"} {
		var n int
		if err := testDB.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s has %d rows; the preview must write nothing", table, n)
		}
	}

	// Resolving a line normally creates an inventory entry. A preview must not.
	var n int
	if err := testDB.QueryRow(context.Background(),
		"SELECT count(*) FROM inventory_entries WHERE team_id = $1 AND part_id = $2", teamID, partID).Scan(&n); err != nil {
		t.Fatalf("counting inventory entries: %v", err)
	}
	if n != 0 {
		t.Errorf("the preview created an inventory entry")
	}
}

// The catalog is scoped to the team. Another team's part must not be offered as
// a resolution, or the preview would promise something the import cannot do.
func TestResolvePreviewDoesNotSeeOtherTeamsParts(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "scoped@example.com", "pw", "Scoped")
	token := testutil.CreateSession(t, testDB, userID)
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Mine")

	otherUser := testutil.CreateUser(t, testDB, "other@example.com", "pw", "Other")
	otherTeam := testutil.CreateTeam(t, testDB, otherUser, "member", "Theirs")
	testutil.CreatePart(t, testDB, "team", "THEIR-SECRET-PART", &otherTeam)

	body := resolvePreview(t, teamID, token, map[string]any{
		"items": []map[string]any{
			{"reference_designators": "U1", "quantity": 1, "mpn": "THEIR-SECRET-PART"},
		},
	})
	items := previewItems(t, body)
	if got := items[0]["state"]; got != "importing" {
		t.Errorf("state = %v; another team's part must not resolve the line", got)
	}
	if items[0]["part"] != nil {
		t.Errorf("leaked another team's part: %v", items[0]["part"])
	}
}

// A global part is shared, so it does resolve — the counterpart to the scoping
// test above, which would otherwise pass by never matching anything.
func TestResolvePreviewSeesGlobalParts(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "global@example.com", "pw", "Global")
	token := testutil.CreateSession(t, testDB, userID)
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Global Team")
	testutil.CreatePart(t, testDB, "global", "LM358DR", nil)

	body := resolvePreview(t, teamID, token, map[string]any{
		"items": []map[string]any{{"reference_designators": "U1", "quantity": 1, "mpn": "LM358DR"}},
	})
	items := previewItems(t, body)
	if got := items[0]["state"]; got != "auto_mpn" {
		t.Errorf("state = %v, want auto_mpn for a global part", got)
	}
}

// A SKU resolves only against a chosen distributor. This is the single most
// common surprise in a BOM import, so both halves are pinned: with the
// distributor the line resolves, without it the same line does not.
func TestResolvePreviewSKUNeedsADistributor(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sku@example.com", "pw", "SKU")
	token := testutil.CreateSession(t, testDB, userID)
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "SKU Team")
	distID := testutil.GetTestDistributorID(t, testDB)
	testutil.EnableDistributor(t, testDB, teamID, distID)

	partID := testutil.CreatePart(t, testDB, "team", "RC0402FR-0710KL", &teamID)
	testutil.CreateListing(t, testDB, partID, distID, "311-10.0KHRCT-ND")

	withDist := previewItems(t, resolvePreview(t, teamID, token, map[string]any{
		"items": []map[string]any{{
			"reference_designators": "R1", "quantity": 1,
			"sku": "311-10.0KHRCT-ND", "resolved_distributor_id": distID,
		}},
	}))
	if got := withDist[0]["state"]; got != "auto_sku" {
		t.Errorf("with a distributor: state = %v, want auto_sku", got)
	}

	withoutDist := previewItems(t, resolvePreview(t, teamID, token, map[string]any{
		"items": []map[string]any{{
			"reference_designators": "R1", "quantity": 1, "sku": "311-10.0KHRCT-ND",
		}},
	}))
	if got := withoutDist[0]["state"]; got == "auto_sku" {
		t.Errorf("without a distributor the SKU tier must not fire, got %v", got)
	}
}

// Asking about a project you are not a member of must not report on its
// revisions, even when you are a member of the team in the path.
func TestResolvePreviewRejectsForeignProject(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "proj@example.com", "pw", "Proj")
	token := testutil.CreateSession(t, testDB, userID)
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Mine")

	otherUser := testutil.CreateUser(t, testDB, "proj2@example.com", "pw", "Proj2")
	otherTeam := testutil.CreateTeam(t, testDB, otherUser, "member", "Theirs")
	foreignProject := testutil.CreateProject(t, testDB, otherTeam, otherUser, "Theirs")

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/teams/"+teamID+"/bom/resolve-preview", map[string]any{
			"project_id": foreignProject,
			"items":      []map[string]any{{"reference_designators": "R1", "quantity": 1, "mpn": "X"}},
		}, token)
	if status == http.StatusOK {
		t.Errorf("a project from another team was accepted (status %d)", status)
	}
}

func TestResolvePreviewValidatesInput(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "valid@example.com", "pw", "Valid")
	token := testutil.CreateSession(t, testDB, userID)
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Valid Team")

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/teams/"+teamID+"/bom/resolve-preview", map[string]any{"items": []map[string]any{}}, token)
	if status != http.StatusBadRequest {
		t.Errorf("empty items: status = %d, want 400", status)
	}

	// The per-line cost is a ladder of catalog queries, so the bound is low and
	// must actually be enforced.
	many := make([]map[string]any, 1001)
	for i := range many {
		many[i] = map[string]any{"reference_designators": "R1", "quantity": 1}
	}
	_, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/teams/"+teamID+"/bom/resolve-preview", map[string]any{"items": many}, token)
	if status != http.StatusBadRequest {
		t.Errorf("oversized request: status = %d, want 400", status)
	}
}
