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
