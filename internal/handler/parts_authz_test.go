package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// TestParamEdit_GlobalRequiresAdmin verifies that manual user-parameter edits on a
// global part are rejected for non-admins and allowed for admins.
func TestParamEdit_GlobalRequiresAdmin(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	member := testutil.CreateUser(t, testDB, "param-member@example.com", "pw", "Member")
	testutil.CreateTeam(t, testDB, member, "admin", "Member Team") // team admin, not instance admin
	memberToken := testutil.CreateSession(t, testDB, member)
	globalPart := testutil.CreatePart(t, testDB, "global", "GLOBAL-RES", nil)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/parts/%s/parameters", globalPart),
		map[string]any{"name": "resistance", "value": "10k"}, memberToken)
	if status != http.StatusForbidden {
		t.Errorf("non-admin create param on global part: expected 403, got %d", status)
	}

	admin := testutil.CreateAdminUser(t, testDB, "param-admin@example.com", "pw", "Admin")
	adminToken := testutil.CreateSession(t, testDB, admin)
	_, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/parts/%s/parameters", globalPart),
		map[string]any{"name": "resistance", "value": "10k"}, adminToken)
	if status != http.StatusCreated {
		t.Errorf("admin create param on global part: expected 201, got %d", status)
	}
}

// TestParamEdit_TeamPartCrossTeam verifies user-parameter edits on a team-scoped
// part require membership in the owning team.
func TestParamEdit_TeamPartCrossTeam(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	owner := testutil.CreateUser(t, testDB, "param-owner@example.com", "pw", "Owner")
	teamB := testutil.CreateTeam(t, testDB, owner, "member", "Owner Team")
	ownerToken := testutil.CreateSession(t, testDB, owner)
	teamPart := testutil.CreatePart(t, testDB, "team", "TEAM-PART", &teamB)

	outsider := testutil.CreateUser(t, testDB, "param-out@example.com", "pw", "Out")
	testutil.CreateTeam(t, testDB, outsider, "admin", "Outsider Team")
	outsiderToken := testutil.CreateSession(t, testDB, outsider)

	// Outsider (not a member of teamB) is rejected.
	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/parts/%s/parameters", teamPart),
		map[string]any{"name": "tol", "value": "1%"}, outsiderToken)
	if status != http.StatusForbidden {
		t.Errorf("outsider create param on team part: expected 403, got %d", status)
	}

	// Owner (member of teamB) is allowed.
	_, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/parts/%s/parameters", teamPart),
		map[string]any{"name": "tol", "value": "1%"}, ownerToken)
	if status != http.StatusCreated {
		t.Errorf("owner create param on team part: expected 201, got %d", status)
	}
}

// TestListingEdit_OtherTeamPartRejected verifies a team cannot add a listing to
// another team's team-scoped part, while global parts remain shared-editable.
func TestListingEdit_OtherTeamPartRejected(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	owner := testutil.CreateUser(t, testDB, "lst-owner@example.com", "pw", "Owner")
	teamB := testutil.CreateTeam(t, testDB, owner, "member", "Owner Team")
	teamPartB := testutil.CreatePart(t, testDB, "team", "TEAMB-PART", &teamB)

	attacker := testutil.CreateUser(t, testDB, "lst-attacker@example.com", "pw", "Attacker")
	teamA := testutil.CreateTeam(t, testDB, attacker, "member", "Attacker Team")
	attackerToken := testutil.CreateSession(t, testDB, attacker)
	distID := testutil.GetTestDistributorID(t, testDB)
	globalPart := testutil.CreatePart(t, testDB, "global", "GLOBAL-LST", nil)

	// teamA member targeting teamB's part → 403.
	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/parts/%s/listings", teamA, teamPartB),
		map[string]any{"sku": "X1", "distributor_id": distID}, attackerToken)
	if status != http.StatusForbidden {
		t.Errorf("listing add on other team's part: expected 403, got %d", status)
	}

	// teamA member on a global (shared) part → allowed (202 enqueues enrichment).
	_, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/parts/%s/listings", teamA, globalPart),
		map[string]any{"sku": "X1", "distributor_id": distID}, attackerToken)
	if status != http.StatusAccepted {
		t.Errorf("listing add on global part: expected 202, got %d", status)
	}
}
