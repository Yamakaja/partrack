package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// resistorsCategoryID is the seeded id for the "resistors" category (migration 000015).
const resistorsCategoryID = 30

func TestCreateGenericPart_CreateOrGet(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "generic@example.com", "pw", "Gen")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Gen Team")
	token := testutil.CreateSession(t, testDB, userID)

	mk := func(params []map[string]any) (map[string]any, int) {
		return testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/parts/generic", map[string]any{
			"team_id":     teamID,
			"category_id": resistorsCategoryID,
			"package":     "0603",
			"params":      params,
		}, token)
	}

	// 1. First create → 201, existing=false.
	body, status := mk([]map[string]any{{"slug": "resistance", "value": "10k"}})
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %v", status, body)
	}
	if body["existing"] != false {
		t.Errorf("create: expected existing=false, got %v", body["existing"])
	}
	firstID, _ := body["part_id"].(string)
	if firstID == "" {
		t.Fatal("create: missing part_id")
	}

	// 2. Same constraints → create-or-get returns the existing part (200).
	body, status = mk([]map[string]any{{"slug": "resistance", "value": "10k"}})
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %v", status, body)
	}
	if body["existing"] != true {
		t.Errorf("get: expected existing=true, got %v", body["existing"])
	}
	if body["part_id"] != firstID {
		t.Errorf("get: expected same part_id %s, got %v", firstID, body["part_id"])
	}

	// 3. Adding a tolerance constraint is a different generic (closed-world) → new part.
	body, status = mk([]map[string]any{
		{"slug": "resistance", "value": "10k"},
		{"slug": "tolerance", "value": "1%"},
	})
	if status != http.StatusCreated {
		t.Fatalf("create-with-tol: expected 201, got %d: %v", status, body)
	}
	if body["part_id"] == firstID {
		t.Error("create-with-tol: expected a distinct part, got the same id")
	}
}

func TestCreateGenericPart_Searchable(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "gensearch@example.com", "pw", "Gen")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Gen Team")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/parts/generic", map[string]any{
		"team_id":     teamID,
		"category_id": resistorsCategoryID,
		"package":     "0603",
		"params":      []map[string]any{{"slug": "resistance", "value": "10k"}},
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", status)
	}

	// The generic is found by a value-based parameter search (resistance = param id 1 = 10000 Ω),
	// proving the parsed parameter landed and is searchable.
	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		"/api/v1/parts?team_id="+teamID+"&category_id=30&param_1_min=10000&param_1_max=10000", nil, token)
	if status != http.StatusOK {
		t.Fatalf("search: expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("search: expected 1 matching generic part, got %d (%v)", len(items), body)
	}
}

// TestCreateGenericPart_SynthesizedIdentity verifies the create path populates a
// human MPN, description, and "Generic" manufacturer from the category's generic
// config — a 1 Ω 0402 resistor becomes GEN-RES-1R-0402 / "1 Ω 0402 Resistor".
func TestCreateGenericPart_SynthesizedIdentity(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "gensynth@example.com", "pw", "Gen")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Gen Team")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/parts/generic", map[string]any{
		"team_id":     teamID,
		"category_id": resistorsCategoryID,
		"package":     "0402",
		"params":      []map[string]any{{"slug": "resistance", "value": "1"}},
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %v", status, body)
	}
	partID, _ := body["part_id"].(string)

	var mpn, description, manufacturer *string
	if err := testDB.QueryRow(context.Background(),
		"SELECT mpn, description, manufacturer FROM parts WHERE id = $1", partID,
	).Scan(&mpn, &description, &manufacturer); err != nil {
		t.Fatalf("load part: %v", err)
	}
	if mpn == nil || *mpn != "GEN-RES-1R-0402" {
		t.Errorf("mpn = %v, want GEN-RES-1R-0402", mpn)
	}
	if description == nil || *description != "1 Ω 0402 Resistor" {
		t.Errorf("description = %v, want \"1 Ω 0402 Resistor\"", description)
	}
	if manufacturer == nil || *manufacturer != "Generic" {
		t.Errorf("manufacturer = %v, want Generic", manufacturer)
	}
}

// TestImportRevision_ParametricAutoGeneric exercises the import job's match-only
// parametric tier end-to-end: a generic line resolves to a pre-existing matching
// generic part as auto_generic.
func TestImportRevision_ParametricAutoGeneric(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "rev-generic@example.com", "pw", "Rev")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Rev Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-GEN")
	token := testutil.CreateSession(t, testDB, userID)

	// Seed a matching generic part: resistors / 0603 / 10k.
	gbody, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/parts/generic", map[string]any{
		"team_id":     teamID,
		"category_id": resistorsCategoryID,
		"package":     "0603",
		"params":      []map[string]any{{"slug": "resistance", "value": "10k"}},
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("seed generic: expected 201, got %d: %v", status, gbody)
	}
	genericID, _ := gbody["part_id"].(string)

	// Import a BOM with a generic resistor line that should match the seeded part.
	ibody, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions", teamID, projectID),
		map[string]any{
			"label":      "v1.0",
			"set_active": true,
			"items": []any{
				map[string]any{
					"reference_designators": "R1", "quantity": 1,
					"value": "10k", "footprint": "R_0603_1608Metric",
				},
			},
		}, token)
	if status != http.StatusAccepted {
		t.Fatalf("import: expected 202, got %d: %v", status, ibody)
	}
	revID, _ := ibody["bom_revision_id"].(string)

	// The import job runs asynchronously; poll the revision until the line resolves.
	var item map[string]any
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rbody, st := testutil.Do(t, testEnv.Server, http.MethodGet,
			fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s", teamID, projectID, revID), nil, token)
		if st == http.StatusOK {
			if its, _ := rbody["items"].([]any); len(its) == 1 {
				it := its[0].(map[string]any)
				if it["resolution_state"] != nil {
					item = it
					break
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if item == nil {
		t.Fatal("import job did not resolve the line within the timeout")
	}
	if item["resolution_state"] != "auto_generic" {
		t.Fatalf("expected resolution_state=auto_generic, got %v", item["resolution_state"])
	}
	if rp, _ := item["resolved_part_id"].(string); rp != genericID {
		t.Errorf("expected resolved_part_id=%s, got %v", genericID, item["resolved_part_id"])
	}
}
