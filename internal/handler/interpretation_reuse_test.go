package handler_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// overrideInterpretation is a review-gate interpretation for a 10k 0603 resistor.
// confidence 0.42 is a sentinel: a fresh parse would yield 1.0, so seeing 0.42
// on a later, un-overridden line proves the interpretation was reused rather
// than re-parsed. The category/package/params still match the seeded generic so
// satisfaction resolves the line to auto_generic — exercising the whole chain.
func overrideInterpretation() map[string]any {
	return map[string]any{
		"route":    "parametric",
		"class":    "R",
		"category": "resistors",
		"mounting": "smd",
		// Package is modelled as a parameter (slug "package"), not a top-level field.
		"params": []any{
			map[string]any{"slug": "resistance", "raw": "10k", "value_si": 10000},
			map[string]any{"slug": "package", "raw": "0603", "value_si": nil},
		},
		"flags":      []any{},
		"confidence": 0.42,
	}
}

// seedGenericResistor creates the resistors/0603/10k generic the imported lines
// resolve against.
func seedGenericResistor(t *testing.T, teamID, token string) string {
	t.Helper()
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/parts/generic", map[string]any{
		"team_id":     teamID,
		"category_id": resistorsCategoryID,
		"package":     "0603",
		"params":      []map[string]any{{"slug": "resistance", "value": "10k"}},
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("seed generic: expected 201, got %d: %v", status, body)
	}
	id, _ := body["part_id"].(string)
	return id
}

// importResistorLine imports a single 10k/0603 resistor line, optionally carrying
// a review-gate interpretation override. Returns the new revision id.
func importResistorLine(t *testing.T, teamID, projectID, token, label string, interp map[string]any) string {
	t.Helper()
	item := map[string]any{
		"reference_designators": "R1", "quantity": 1,
		"value": "10k", "footprint": "R_0603_1608Metric",
	}
	if interp != nil {
		item["interpretation"] = interp
	}
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions", teamID, projectID),
		map[string]any{"label": label, "set_active": true, "items": []any{item}}, token)
	if status != http.StatusAccepted {
		t.Fatalf("import %s: expected 202, got %d: %v", label, status, body)
	}
	revID, _ := body["bom_revision_id"].(string)
	return revID
}

// pollResolvedLine waits for the async import job to resolve the single line in a
// revision, then returns it. Waiting for the job to finish also avoids racing
// the test-cleanup truncate against an in-flight job.
func pollResolvedLine(t *testing.T, teamID, projectID, token, revID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		body, st := testutil.Do(t, testEnv.Server, http.MethodGet,
			fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s", teamID, projectID, revID), nil, token)
		if st == http.StatusOK {
			if its, _ := body["items"].([]any); len(its) == 1 {
				it := its[0].(map[string]any)
				if it["resolution_state"] != nil {
					return it
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("import job did not resolve the line within the timeout")
	return nil
}

func assertReusedInterpretation(t *testing.T, item map[string]any) {
	t.Helper()
	if item["interpretation_source"] != "user" {
		t.Errorf("expected interpretation_source=user (reused), got %v", item["interpretation_source"])
	}
	interp, _ := item["interpretation"].(map[string]any)
	if interp == nil {
		t.Fatalf("missing interpretation on reused line: %v", item)
	}
	if c, _ := interp["confidence"].(float64); c != 0.42 {
		t.Errorf("expected carried-over confidence sentinel 0.42, got %v (line was re-parsed, not reused)", interp["confidence"])
	}
}

// TestInterpretationReuse_PriorRevisionCarryover verifies that a user's review-gate
// correction in one revision is carried into the next revision of the SAME project
// without re-review (source stays 'user', the corrected interpretation survives).
func TestInterpretationReuse_PriorRevisionCarryover(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "reuse-prior@example.com", "pw", "Reuse")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Reuse Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-REUSE")
	token := testutil.CreateSession(t, testDB, userID)

	seedGenericResistor(t, teamID, token)

	// Rev A: the user supplies a corrected interpretation via the review gate.
	revA := importResistorLine(t, teamID, projectID, token, "v1.0", overrideInterpretation())
	pollResolvedLine(t, teamID, projectID, token, revA)

	// Rev B: the same line, no override — must inherit Rev A's user interpretation.
	revB := importResistorLine(t, teamID, projectID, token, "v1.1", nil)
	item := pollResolvedLine(t, teamID, projectID, token, revB)

	assertReusedInterpretation(t, item)
	// The identical re-imported line reuses Rev A's resolution directly (the
	// line-signature prior-revision tier runs before the generic match tier), so it
	// resolves to the same part labelled auto_prior_revision rather than re-deriving
	// it as auto_generic.
	if item["resolution_state"] != "auto_prior_revision" {
		t.Errorf("expected resolution_state=auto_prior_revision, got %v", item["resolution_state"])
	}
}

// TestInterpretationReuse_TeamMemoryCrossProject verifies that a correction made in
// one project is remembered team-wide and reused in a DIFFERENT project that has no
// prior revision of its own.
func TestInterpretationReuse_TeamMemoryCrossProject(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "reuse-team@example.com", "pw", "Reuse")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Reuse Team")
	projectA := testutil.CreateProject(t, testDB, teamID, userID, "PCB-A")
	projectB := testutil.CreateProject(t, testDB, teamID, userID, "PCB-B")
	token := testutil.CreateSession(t, testDB, userID)

	seedGenericResistor(t, teamID, token)

	// Project A: correct the interpretation via the review gate → team memory.
	revA := importResistorLine(t, teamID, projectA, token, "v1.0", overrideInterpretation())
	pollResolvedLine(t, teamID, projectA, token, revA)

	// Project B: first revision (no prior carryover), same value+footprint — the
	// team memory must supply the remembered interpretation.
	revB := importResistorLine(t, teamID, projectB, token, "v1.0", nil)
	item := pollResolvedLine(t, teamID, projectB, token, revB)

	assertReusedInterpretation(t, item)
}

// TestGenericPartCandidates verifies the review-gate candidate endpoint returns the
// catalog parts that satisfy a generic line's parametric spec.
func TestGenericPartCandidates(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "cand@example.com", "pw", "Cand")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Cand Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-CAND")
	token := testutil.CreateSession(t, testDB, userID)

	partID := seedGenericResistor(t, teamID, token) // 10k / 0603

	rev := importResistorLine(t, teamID, projectID, token, "v1.0", nil)
	item := pollResolvedLine(t, teamID, projectID, token, rev)
	itemID, _ := item["id"].(string)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/items/%s/generic-candidates",
			teamID, projectID, rev, itemID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("candidates: expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	found := false
	for _, c := range items {
		if cm, ok := c.(map[string]any); ok && cm["part_id"] == partID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected seeded generic %s among candidates, got %v", partID, items)
	}
}

// TestBOMImport_FuzzyMPNLocalMatch verifies that a BOM line whose MPN has no exact
// catalog match but a single fuzzy (substring) match resolves to that local part
// (flagged auto_mpn_fuzzy for review) instead of launching a distributor search.
func TestBOMImport_FuzzyMPNLocalMatch(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "fuzzy@example.com", "pw", "Fuzzy")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Fuzzy Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-FUZZY")
	token := testutil.CreateSession(t, testDB, userID)

	// Catalog holds a packaged variant of the BOM's base MPN (no exact match).
	partID := testutil.CreatePart(t, testDB, "global", "AZ431AN-ATRG1", nil)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions", teamID, projectID),
		map[string]any{"label": "v1.0", "set_active": true, "items": []any{
			map[string]any{"reference_designators": "U2", "quantity": 1, "mpn": "AZ431A"},
		}}, token)
	if status != http.StatusAccepted {
		t.Fatalf("import: expected 202, got %d: %v", status, body)
	}
	revID, _ := body["bom_revision_id"].(string)

	item := pollResolvedLine(t, teamID, projectID, token, revID)
	// Resolved to the local part, but flagged (fuzzy) so it surfaces for a confirm.
	if item["resolution_state"] != "auto_mpn_fuzzy" {
		t.Errorf("resolution_state = %v, want auto_mpn_fuzzy", item["resolution_state"])
	}
	if item["resolved_part_id"] != partID {
		t.Errorf("resolved_part_id = %v, want %s", item["resolved_part_id"], partID)
	}
	if item["resolve_item"] != nil {
		t.Errorf("expected no distributor resolve job, got %v", item["resolve_item"])
	}
}
