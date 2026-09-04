package handler_test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestSourcingPolicy_SetAndGet(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sourcing@example.com", "pw", "Src")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Src Team")
	token := testutil.CreateSession(t, testDB, userID)
	distID := testutil.GetTestDistributorID(t, testDB)
	testutil.EnableDistributor(t, testDB, teamID, distID)

	path := fmt.Sprintf("/api/v1/teams/%s/sourcing-policy", teamID)

	// PUT: enable ranked strategy with a 15% guardrail and rank the test distributor #1.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPut, path, map[string]any{
		"strategy":             "preferred_in_stock",
		"cost_tolerance_pct":   15,
		"distributor_priority": []string{distID},
	}, token)
	if status != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d: %v", status, body)
	}
	if body["strategy"] != "preferred_in_stock" {
		t.Errorf("expected strategy preferred_in_stock, got %v", body["strategy"])
	}
	if pct, _ := body["cost_tolerance_pct"].(string); pct == "" {
		t.Errorf("expected cost_tolerance_pct set, got %v", body["cost_tolerance_pct"])
	} else if f, _ := strconv.ParseFloat(pct, 64); f != 15 {
		t.Errorf("expected tolerance 15, got %v", pct)
	}
	assertDistributorPriority(t, body, distID, 1)

	// GET should reflect the same policy.
	body, status = testutil.Do(t, testEnv.Server, http.MethodGet, path, nil, token)
	if status != http.StatusOK {
		t.Fatalf("GET expected 200, got %d: %v", status, body)
	}
	if body["strategy"] != "preferred_in_stock" {
		t.Errorf("GET expected strategy preferred_in_stock, got %v", body["strategy"])
	}
	assertDistributorPriority(t, body, distID, 1)

	// PUT cost strategy with no ranking clears priorities and tolerance.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPut, path, map[string]any{
		"strategy":             "cost",
		"distributor_priority": []string{},
	}, token)
	if status != http.StatusOK {
		t.Fatalf("PUT(cost) expected 200, got %d: %v", status, body)
	}
	if body["cost_tolerance_pct"] != nil {
		t.Errorf("expected tolerance cleared, got %v", body["cost_tolerance_pct"])
	}
	assertDistributorPriority(t, body, distID, 0) // 0 => expect nil priority
}

func TestSourcingPolicy_RejectsBadStrategy(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sourcing-bad@example.com", "pw", "Src")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Src Team")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPut,
		fmt.Sprintf("/api/v1/teams/%s/sourcing-policy", teamID),
		map[string]any{"strategy": "cheapest_ever"}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad strategy, got %d", status)
	}
}

func TestSourcingPolicy_MemberCanReadNotWrite(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sourcing-member@example.com", "pw", "Src")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Src Team")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/sourcing-policy", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200 for member read, got %d: %v", status, body)
	}

	_, status = testutil.Do(t, testEnv.Server, http.MethodPut,
		fmt.Sprintf("/api/v1/teams/%s/sourcing-policy", teamID),
		map[string]any{"strategy": "cost", "distributor_priority": []string{}}, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 for member write, got %d", status)
	}
}

// assertDistributorPriority checks the distributor's priority in a policy response.
// wantPriority == 0 asserts the priority is null (unranked).
func assertDistributorPriority(t *testing.T, body map[string]any, distID string, wantPriority int) {
	t.Helper()
	dists, _ := body["distributors"].([]any)
	for _, d := range dists {
		dm, _ := d.(map[string]any)
		if dm["distributor_id"] != distID {
			continue
		}
		if wantPriority == 0 {
			if dm["priority"] != nil {
				t.Errorf("expected nil priority, got %v", dm["priority"])
			}
			return
		}
		p, ok := dm["priority"].(float64)
		if !ok || int(p) != wantPriority {
			t.Errorf("expected priority %d, got %v", wantPriority, dm["priority"])
		}
		return
	}
	t.Errorf("distributor %s not found in policy response", distID)
}
