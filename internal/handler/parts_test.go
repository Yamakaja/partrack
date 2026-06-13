package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Yamakaja/partrack/internal/testutil"
	"github.com/Yamakaja/partrack/internal/worker"
)

func TestCreatePart_Global(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateAdminUser(t, testDB, "parts-global@example.com", "pw", "Parts")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/parts",
		map[string]any{"scope": "global", "mpn": "RC0402"}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	if body["scope"] != "global" {
		t.Errorf("expected scope=global, got %v", body["scope"])
	}
	if body["mpn"] != "RC0402" {
		t.Errorf("expected mpn=RC0402, got %v", body["mpn"])
	}
}

func TestCreatePart_Team(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "parts-team@example.com", "pw", "Parts")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Parts Team")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/parts",
		map[string]any{"scope": "team", "team_id": teamID, "mpn": "CUSTOM-01"}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	if body["scope"] != "team" {
		t.Errorf("expected scope=team, got %v", body["scope"])
	}
}

func TestCreatePart_TeamScopeWithoutTeamID(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "parts-noteam@example.com", "pw", "Parts")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/parts",
		map[string]any{"scope": "team", "mpn": "CUSTOM-02"}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestListParts_Pagination(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "parts-page@example.com", "pw", "Parts")
	token := testutil.CreateSession(t, testDB, userID)

	// Create 3 parts.
	for i := range 3 {
		testutil.CreatePart(t, testDB, "global", fmt.Sprintf("PART-%02d", i), nil)
	}

	body, status := testutil.Do(t, testEnv.Server,
		http.MethodGet, "/api/v1/parts?limit=2&offset=0", nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if body["total"].(float64) < 3 {
		t.Errorf("expected total >= 3, got %v", body["total"])
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Errorf("expected 2 items with limit=2, got %d", len(items))
	}
}

func TestListParts_SearchQuery(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "parts-search@example.com", "pw", "Parts")
	token := testutil.CreateSession(t, testDB, userID)
	testutil.CreatePart(t, testDB, "global", "LM317T", nil)
	testutil.CreatePart(t, testDB, "global", "BC547", nil)

	body, _ := testutil.Do(t, testEnv.Server,
		http.MethodGet, "/api/v1/parts?q=LM317", nil, token)
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Errorf("expected 1 result for q=LM317, got %d", len(items))
	}
}

func TestGetPart(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "parts-get@example.com", "pw", "Parts")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "ATmega328P", nil)

	body, status := testutil.Do(t, testEnv.Server,
		http.MethodGet, fmt.Sprintf("/api/v1/parts/%s", partID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["id"] != partID {
		t.Errorf("wrong part ID: %v", body["id"])
	}
}

func TestUpdatePart(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "parts-update@example.com", "pw", "Parts")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "STM32F4", nil)

	body, status := testutil.Do(t, testEnv.Server,
		http.MethodPatch, fmt.Sprintf("/api/v1/parts/%s", partID),
		map[string]any{"description": "ARM Cortex-M4"}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["description"] != "ARM Cortex-M4" {
		t.Errorf("expected updated description, got %v", body["description"])
	}
}

// waitEnrich subscribes to the worker queue and blocks until a status=complete
// event arrives for teamID, or fails the test after 5 seconds.
func waitEnrich(t *testing.T, q *worker.Queue, teamID string) {
	t.Helper()
	ch, cancel := q.Subscribe(teamID, "")
	defer cancel()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-ch:
			var d map[string]any
			json.Unmarshal(ev.Data, &d) //nolint:errcheck
			if d["status"] == "complete" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for enrichment to complete")
		}
	}
}

func TestEnrichPart_LM317_WithTestDistributor(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "enrich-lm317@example.com", "pw", "Enrich")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Enrich Team")
	token := testutil.CreateSession(t, testDB, userID)
	distID := testutil.GetTestDistributorID(t, testDB)
	testutil.EnableDistributor(t, testDB, teamID, distID)
	partID := testutil.CreatePart(t, testDB, "global", "LM317T", nil)

	body, status := testutil.Do(t, testEnv.Server,
		http.MethodPost, fmt.Sprintf("/api/v1/teams/%s/parts/%s/enrich", teamID, partID),
		map[string]any{"sku": "LM317-TEST-TUBE", "distributor_id": distID}, token)
	if status != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %v", status, body)
	}
	if body["job_id"] == nil {
		t.Error("expected job_id in response")
	}

	// Wait for the worker to finish.
	waitEnrich(t, testEnv.Queue, teamID)

	// Verify enrichment results via the API.
	detail, status := testutil.Do(t, testEnv.Server,
		http.MethodGet, fmt.Sprintf("/api/v1/parts/%s", partID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200 on GetPart, got %d", status)
	}

	listings, _ := detail["listings"].([]any)
	if len(listings) != 1 {
		t.Errorf("expected 1 listing after enrichment, got %d", len(listings))
	}

	params, _ := detail["parameters"].(map[string]any)
	distSourced, _ := params["distributor_sourced"].([]any)
	if len(distSourced) != 3 {
		t.Errorf("expected 3 distributor parameters after enrichment, got %d", len(distSourced))
	}
}

func TestEnrichPart_MissingBody(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "enrich-noteam@example.com", "pw", "Enrich")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Enrich Team")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "LM317T", nil)

	_, status := testutil.Do(t, testEnv.Server,
		http.MethodPost, fmt.Sprintf("/api/v1/teams/%s/parts/%s/enrich", teamID, partID), nil, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 with nil body, got %d", status)
	}
}

func TestEnrichPart_NonMemberForbidden(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	ownerID := testutil.CreateUser(t, testDB, "enrich-owner@example.com", "pw", "Owner")
	outsiderID := testutil.CreateUser(t, testDB, "enrich-out@example.com", "pw", "Out")
	teamID := testutil.CreateTeam(t, testDB, ownerID, "admin", "Enrich Team")
	outsiderToken := testutil.CreateSession(t, testDB, outsiderID)
	partID := testutil.CreatePart(t, testDB, "global", "LM317T", nil)

	distID := testutil.GetTestDistributorID(t, testDB)
	_, status := testutil.Do(t, testEnv.Server,
		http.MethodPost, fmt.Sprintf("/api/v1/teams/%s/parts/%s/enrich", teamID, partID),
		map[string]any{"sku": "LM317-TEST-TUBE", "distributor_id": distID}, outsiderToken)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
}

func TestCreateAndDeleteUserParameter(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	// Manual (user-specified) parameters on a global part require instance admin.
	userID := testutil.CreateAdminUser(t, testDB, "params@example.com", "pw", "Params")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "CUSTOM-PART", nil)

	// Create a user parameter.
	body, status := testutil.Do(t, testEnv.Server,
		http.MethodPost, fmt.Sprintf("/api/v1/parts/%s/parameters", partID),
		map[string]any{"name": "resistance", "value": "10k", "unit": "Ω"}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	paramID, _ := body["id"].(string)

	// Delete it.
	_, status = testutil.Do(t, testEnv.Server,
		http.MethodDelete, fmt.Sprintf("/api/v1/parts/%s/parameters/%s", partID, paramID),
		nil, token)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204 on delete, got %d", status)
	}
}

// TestUserParameterResolution verifies that a user-specified parameter is
// resolved to a canonical parameter definition (and parsed to a numeric value)
// on write, so user values participate in the list-view parameter columns and
// value search the same way distributor-sourced values do.
func TestUserParameterResolution(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	ctx := context.Background()

	userID := testutil.CreateAdminUser(t, testDB, "param-resolve@example.com", "pw", "Params")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "RESOLVE-PART", nil)

	// Use the canonical parameter definitions seeded by migration 000018
	// (reference data, not truncated between tests).
	var resistanceID, capacitanceID int32
	if err := testDB.QueryRow(ctx,
		`SELECT id FROM parameter_definitions WHERE slug = 'resistance'`).Scan(&resistanceID); err != nil {
		t.Fatalf("lookup resistance def: %v", err)
	}
	if err := testDB.QueryRow(ctx,
		`SELECT id FROM parameter_definitions WHERE slug = 'capacitance'`).Scan(&capacitanceID); err != nil {
		t.Fatalf("lookup capacitance def: %v", err)
	}

	createParam := func(name, value string) map[string]any {
		t.Helper()
		body, status := testutil.Do(t, testEnv.Server,
			http.MethodPost, fmt.Sprintf("/api/v1/parts/%s/parameters", partID),
			map[string]any{"name": name, "value": value, "unit": "Ω"}, token)
		if status != http.StatusCreated {
			t.Fatalf("create %q: expected 201, got %d: %v", name, status, body)
		}
		return body
	}

	// 1. Registry match by slug → resolves + parses.
	b := createParam("resistance", "10k")
	if got := b["parameter_id"]; got != float64(resistanceID) {
		t.Errorf("slug match: parameter_id = %v, want %d", got, resistanceID)
	}
	if got := b["numeric_value"]; got != float64(10000) {
		t.Errorf("slug match: numeric_value = %v, want 10000", got)
	}

	// 2. Registry match by case-insensitive display name.
	b = createParam("RESISTANCE", "4.7k")
	if got := b["parameter_id"]; got != float64(resistanceID) {
		t.Errorf("display-name match: parameter_id = %v, want %d", got, resistanceID)
	}
	if got := b["numeric_value"]; got != float64(4700) {
		t.Errorf("display-name match: numeric_value = %v, want 4700", got)
	}

	// 3. No registry match → stays free-text (NULL parameter_id / numeric_value).
	b = createParam("custom_widget_count", "hello")
	if got := b["parameter_id"]; got != nil {
		t.Errorf("no match: parameter_id = %v, want nil", got)
	}
	if got := b["numeric_value"]; got != nil {
		t.Errorf("no match: numeric_value = %v, want nil", got)
	}

	// 4. Override inheritance: a distributor param whose raw name matches no
	//    definition still lets a same-name user override inherit its mapping.
	distID := testutil.GetTestDistributorID(t, testDB)
	if _, err := testDB.Exec(ctx,
		`INSERT INTO part_parameters (part_id, name, value, source_type, source_distributor_id, parameter_id, numeric_value)
		 VALUES ($1, 'Cap.', '47n', 'distributor', $2, $3, $4)`,
		partID, distID, capacitanceID, 47e-9); err != nil {
		t.Fatalf("seed distributor param: %v", err)
	}
	b = createParam("Cap.", "100n")
	if got := b["parameter_id"]; got != float64(capacitanceID) {
		t.Errorf("override inherit: parameter_id = %v, want %d", got, capacitanceID)
	}
	if got, ok := b["numeric_value"].(float64); !ok || got < 9.99e-8 || got > 1.001e-7 {
		t.Errorf("override inherit: numeric_value = %v, want ~1e-7", b["numeric_value"])
	}
}
