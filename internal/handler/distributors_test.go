package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestListDistributors(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "distlist@example.com", "pw", "DistList")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/distributors", nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) == 0 {
		t.Fatal("expected at least one distributor (TestDistributor from migration)")
	}
	// Verify the test distributor appears.
	found := false
	for _, it := range items {
		m := it.(map[string]any)
		if m["api_type"] == "test" {
			found = true
		}
	}
	if !found {
		t.Error("TestDistributor (api_type=test) not found in /distributors list")
	}
}

func TestListDistributors_Unauthenticated(t *testing.T) {
	_, status := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/distributors", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestListDistributorConfigs_EmptyInitially(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	adminID := testutil.CreateUser(t, testDB, "distcfg@example.com", "pw", "DistCfg")
	teamID := testutil.CreateTeam(t, testDB, adminID, "admin", "DistCfg Team")
	token := testutil.CreateSession(t, testDB, adminID)

	body, status := testutil.Do(t, testEnv.Server,
		http.MethodGet, fmt.Sprintf("/api/v1/teams/%s/distributor-configs", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 configs initially, got %d", len(items))
	}
}

func TestUpsertDistributorConfig_Enable(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	adminID := testutil.CreateUser(t, testDB, "distupsert@example.com", "pw", "DistUpsert")
	teamID := testutil.CreateTeam(t, testDB, adminID, "admin", "Upsert Team")
	token := testutil.CreateSession(t, testDB, adminID)
	distID := testutil.GetTestDistributorID(t, testDB)

	body, status := testutil.Do(t, testEnv.Server,
		http.MethodPut, fmt.Sprintf("/api/v1/teams/%s/distributor-configs/%s", teamID, distID),
		map[string]any{"enabled": true, "preferred": true, "cache_duration_seconds": 3600},
		token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", body["enabled"])
	}
}

func TestUpsertDistributorConfig_NonAdminForbidden(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	adminID := testutil.CreateUser(t, testDB, "distnonadmin@example.com", "pw", "Admin")
	memberID := testutil.CreateUser(t, testDB, "distmember@example.com", "pw", "Member")
	teamID := testutil.CreateTeam(t, testDB, adminID, "admin", "Dist Team")
	testutil.AddMember(t, testDB, teamID, memberID, "member")
	memberToken := testutil.CreateSession(t, testDB, memberID)
	distID := testutil.GetTestDistributorID(t, testDB)

	_, status := testutil.Do(t, testEnv.Server,
		http.MethodPut, fmt.Sprintf("/api/v1/teams/%s/distributor-configs/%s", teamID, distID),
		map[string]any{"enabled": true}, memberToken)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
}

func TestDeleteDistributorConfig(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	adminID := testutil.CreateUser(t, testDB, "distdelete@example.com", "pw", "DistDelete")
	teamID := testutil.CreateTeam(t, testDB, adminID, "admin", "Delete Team")
	token := testutil.CreateSession(t, testDB, adminID)
	distID := testutil.GetTestDistributorID(t, testDB)

	// Enable first.
	testutil.EnableDistributor(t, testDB, teamID, distID)

	_, status := testutil.Do(t, testEnv.Server,
		http.MethodDelete, fmt.Sprintf("/api/v1/teams/%s/distributor-configs/%s", teamID, distID),
		nil, token)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}

	// Config should no longer appear in the list.
	body, _ := testutil.Do(t, testEnv.Server,
		http.MethodGet, fmt.Sprintf("/api/v1/teams/%s/distributor-configs", teamID), nil, token)
	items, _ := body["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 configs after delete, got %d", len(items))
	}
}
