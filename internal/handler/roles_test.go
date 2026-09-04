package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// The team role model: every read is viewer+, mutations step up by domain
// (member = inventory/BOM/build operations, purchaser = procurement, admin =
// team configuration). These tests pin the gates that were previously
// inconsistent with that model — a member could not see the cart, the
// roster, or which distributors the team uses, but could see every order
// and build.

// rolesTeam creates a team with one user per role and returns the team id
// plus a session token for each role.
func rolesTeam(t *testing.T) (teamID string, tokens map[string]string) {
	t.Helper()
	adminID := testutil.CreateUser(t, testDB, "roles-admin@example.com", "pw", "Admin")
	teamID = testutil.CreateTeam(t, testDB, adminID, "admin", "Roles Team")
	tokens = map[string]string{"admin": testutil.CreateSession(t, testDB, adminID)}
	for _, role := range []string{"viewer", "member", "purchaser"} {
		id := testutil.CreateUser(t, testDB, "roles-"+role+"@example.com", "pw", role)
		testutil.AddMember(t, testDB, teamID, id, role)
		tokens[role] = testutil.CreateSession(t, testDB, id)
	}
	return teamID, tokens
}

func TestRoles_CartReadIsViewerWriteIsPurchaser(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	teamID, tokens := rolesTeam(t)
	partID := testutil.CreatePart(t, testDB, "global", "ROLES-PART-01", nil)

	for _, role := range []string{"viewer", "member"} {
		for _, path := range []string{"/cart", "/cart/count", "/cart/part-quantity?part_id=" + partID} {
			body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
				fmt.Sprintf("/api/v1/teams/%s%s", teamID, path), nil, tokens[role])
			if status != http.StatusOK {
				t.Errorf("%s GET %s: expected 200, got %d: %v", role, path, status, body)
			}
		}
		_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
			fmt.Sprintf("/api/v1/teams/%s/cart/items", teamID),
			map[string]any{"part_id": partID, "quantity": 1}, tokens[role])
		if status != http.StatusForbidden {
			t.Errorf("%s POST /cart/items: expected 403, got %d", role, status)
		}
	}

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/items", teamID),
		map[string]any{"part_id": partID, "quantity": 1}, tokens["purchaser"])
	if status != http.StatusCreated {
		t.Errorf("purchaser POST /cart/items: expected 201, got %d", status)
	}
}

func TestRoles_DistributorConfigsRedactedBelowAdmin(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	teamID, tokens := rolesTeam(t)
	distID := testutil.GetTestDistributorID(t, testDB)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPut,
		fmt.Sprintf("/api/v1/teams/%s/distributor-configs/%s", teamID, distID),
		map[string]any{"enabled": true, "preferred": true, "cache_duration_seconds": 1234},
		tokens["admin"])
	if status != http.StatusOK {
		t.Fatalf("admin upsert config: expected 200, got %d", status)
	}

	first := func(role string) map[string]any {
		t.Helper()
		body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
			fmt.Sprintf("/api/v1/teams/%s/distributor-configs", teamID), nil, tokens[role])
		if status != http.StatusOK {
			t.Fatalf("%s GET /distributor-configs: expected 200, got %d: %v", role, status, body)
		}
		items, _ := body["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("%s: expected 1 config, got %d", role, len(items))
		}
		item, _ := items[0].(map[string]any)
		return item
	}

	for _, role := range []string{"viewer", "member", "purchaser"} {
		item := first(role)
		if item["distributor_id"] != distID || item["enabled"] != true || item["preferred"] != true {
			t.Errorf("%s: availability fields missing or wrong: %v", role, item)
		}
		if _, ok := item["has_credentials"]; !ok {
			t.Errorf("%s: has_credentials should be visible (it decides whether lookups work)", role)
		}
		for _, key := range []string{"cache_duration_seconds", "api_credentials", "rate_limit"} {
			if _, ok := item[key]; ok {
				t.Errorf("%s: config specific %q must not be returned below admin: %v", role, key, item)
			}
		}
	}

	admin := first("admin")
	if got, _ := admin["cache_duration_seconds"].(float64); got != 1234 {
		t.Errorf("admin: expected cache_duration_seconds=1234, got %v", admin["cache_duration_seconds"])
	}

	// Writes stay admin-only.
	for _, role := range []string{"viewer", "member", "purchaser"} {
		_, status := testutil.Do(t, testEnv.Server, http.MethodPut,
			fmt.Sprintf("/api/v1/teams/%s/distributor-configs/%s", teamID, distID),
			map[string]any{"enabled": false}, tokens[role])
		if status != http.StatusForbidden {
			t.Errorf("%s PUT /distributor-configs: expected 403, got %d", role, status)
		}
	}
}

func TestRoles_MemberCanDeleteOwnScanSession(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	teamID, tokens := rolesTeam(t)

	create := func(role string) string {
		t.Helper()
		body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
			fmt.Sprintf("/api/v1/teams/%s/scan-sessions", teamID), map[string]any{}, tokens[role])
		if status != http.StatusCreated {
			t.Fatalf("%s POST /scan-sessions: expected 201, got %d: %v", role, status, body)
		}
		id, _ := body["id"].(string)
		return id
	}
	del := func(role, sessionID string) int {
		t.Helper()
		_, status := testutil.Do(t, testEnv.Server, http.MethodDelete,
			fmt.Sprintf("/api/v1/teams/%s/scan-sessions/%s", teamID, sessionID), nil, tokens[role])
		return status
	}

	// Viewers cannot scan at all.
	if _, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/scan-sessions", teamID), map[string]any{}, tokens["viewer"]); status != http.StatusForbidden {
		t.Errorf("viewer POST /scan-sessions: expected 403, got %d", status)
	}

	memberSession := create("member")
	purchaserSession := create("purchaser")

	// Another non-admin cannot delete someone else's session, even a purchaser.
	if status := del("purchaser", memberSession); status != http.StatusForbidden {
		t.Errorf("purchaser deleting member's session: expected 403, got %d", status)
	}
	// The owner can disconnect their own phone.
	if status := del("member", memberSession); status != http.StatusNoContent {
		t.Errorf("member deleting own session: expected 204, got %d", status)
	}
	// Team admins can clean up anyone's.
	if status := del("admin", purchaserSession); status != http.StatusNoContent {
		t.Errorf("admin deleting purchaser's session: expected 204, got %d", status)
	}
}
