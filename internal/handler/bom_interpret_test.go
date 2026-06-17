package handler_test

import (
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestInterpretPreview(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "interp@example.com", "pw", "Interp")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/bom/interpret-preview", map[string]any{
		"items": []map[string]any{
			{"refdes": "R1", "footprint": "R_0402_1005Metric", "value": "10k"},
			{"refdes": "U1", "value": "LM358"},
			{"refdes": "TP1"},
		},
	}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}

	items, _ := body["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("expected 3 interpretations, got %d", len(items))
	}

	row0 := items[0].(map[string]any)
	if row0["route"] != "parametric" || row0["category"] != "resistors" || row0["package"] != "0402" {
		t.Errorf("row0: expected parametric/resistors/0402, got %v", row0)
	}
	params0, _ := row0["params"].([]any)
	if len(params0) != 1 || params0[0].(map[string]any)["slug"] != "resistance" {
		t.Errorf("row0: expected a single resistance param, got %v", params0)
	}

	if items[1].(map[string]any)["route"] != "identity" {
		t.Errorf("row1 (LM358): expected identity, got %v", items[1])
	}
	if items[2].(map[string]any)["route"] != "ignore" {
		t.Errorf("row2 (TP1): expected ignore, got %v", items[2])
	}
}

// TestInterpretPreview_TeamMemory verifies that a team's remembered correction is
// layered over the fresh parse, so the review gate shows what the import will
// persist (flagged `reused`), rather than a parse the import would override.
func TestInterpretPreview_TeamMemory(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "interp-mem@example.com", "pw", "Interp")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Interp Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-INT")
	token := testutil.CreateSession(t, testDB, userID)

	seedGenericResistor(t, teamID, token)

	// Record a user correction (confidence sentinel 0.42) in team memory.
	rev := importResistorLine(t, teamID, projectID, token, "v1.0", overrideInterpretation())
	pollResolvedLine(t, teamID, projectID, token, rev)

	// Preview the same line plus an unrelated one, scoped to the team.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/bom/interpret-preview", map[string]any{
		"team_id": teamID,
		"items": []map[string]any{
			{"refdes": "R9", "footprint": "R_0603_1608Metric", "value": "10k"}, // remembered
			{"refdes": "R10", "footprint": "R_0402_1005Metric", "value": "1k"}, // fresh
		},
	}, token)
	if status != http.StatusOK {
		t.Fatalf("preview: expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	remembered := items[0].(map[string]any)
	if remembered["reused"] != true {
		t.Errorf("row0: expected reused=true, got %v", remembered["reused"])
	}
	if c, _ := remembered["confidence"].(float64); c != 0.42 {
		t.Errorf("row0: expected remembered confidence 0.42, got %v", remembered["confidence"])
	}

	fresh := items[1].(map[string]any)
	if fresh["reused"] != false {
		t.Errorf("row1: expected reused=false, got %v", fresh["reused"])
	}
}
