package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// idorFixture sets up two independent teams. The "attacker" is an admin of teamA;
// all target resources live in teamB. Cross-team access is attempted by calling
// /teams/{teamA}/... with teamB's resource IDs — which RequireTeamRole alone would
// permit (the attacker is a valid member of teamA), so the per-handler ownership
// guards are what must reject it.
type idorFixture struct {
	attackerToken string
	attackerTeam  string
	victimToken   string
	victimTeam    string
	projectB      string
	revB          string
	itemB         string
	entryB        string
	partB         string
}

func newIDORFixture(t *testing.T) idorFixture {
	t.Helper()

	attacker := testutil.CreateUser(t, testDB, "idor-attacker@example.com", "pw", "Attacker")
	teamA := testutil.CreateTeam(t, testDB, attacker, "admin", "Team A")
	attackerToken := testutil.CreateSession(t, testDB, attacker)

	victim := testutil.CreateUser(t, testDB, "idor-victim@example.com", "pw", "Victim")
	teamB := testutil.CreateTeam(t, testDB, victim, "admin", "Team B")
	victimToken := testutil.CreateSession(t, testDB, victim)

	projectB := testutil.CreateProject(t, testDB, teamB, victim, "Victim PCB")
	revB := testutil.CreateRevision(t, testDB, projectB, victim, "v1.0")
	itemB := testutil.CreateBOMLineItem(t, testDB, revB, "R1", 1)
	partB := testutil.CreatePart(t, testDB, "team", "VICTIM-MPN", &teamB)
	entryB := testutil.CreateInventoryEntry(t, testDB, teamB, partB)

	return idorFixture{
		attackerToken: attackerToken, attackerTeam: teamA,
		victimToken: victimToken, victimTeam: teamB,
		projectB: projectB, revB: revB, itemB: itemB, entryB: entryB, partB: partB,
	}
}

// TestIDOR_BOMSubtreeCrossTeam verifies the project/revision/BOM subtree guards
// reject access to another team's resources (404) while the owning team still
// gets a normal response.
func TestIDOR_BOMSubtreeCrossTeam(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	f := newIDORFixture(t)

	cases := []struct {
		name, method, path string
		body               any
	}{
		{"ListRevisions", http.MethodGet,
			fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions", f.attackerTeam, f.projectB), nil},
		{"GetRevision", http.MethodGet,
			fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s", f.attackerTeam, f.projectB, f.revB), nil},
		{"UpdateRevision", http.MethodPatch,
			fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s", f.attackerTeam, f.projectB, f.revB),
			map[string]any{"label": "pwned"}},
		{"StockCheck", http.MethodGet,
			fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/stock-check?build_quantity=1", f.attackerTeam, f.projectB, f.revB), nil},
		{"UpdateBOMItem", http.MethodPatch,
			fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/items/%s", f.attackerTeam, f.projectB, f.revB, f.itemB),
			map[string]any{"ignored": true}},
		{"AddSubstitute", http.MethodPost,
			fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/items/%s/substitutes", f.attackerTeam, f.projectB, f.revB, f.itemB),
			map[string]any{"part_id": f.partB}},
		{"ListSubstitutes", http.MethodGet,
			fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/substitutes", f.attackerTeam, f.projectB, f.revB), nil},
		{"ListLineItemSubstitutes", http.MethodGet,
			fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/items/%s/substitutes", f.attackerTeam, f.projectB, f.revB, f.itemB), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, status := testutil.Do(t, testEnv.Server, c.method, c.path, c.body, f.attackerToken)
			if status != http.StatusNotFound {
				t.Errorf("attacker %s: expected 404, got %d: %v", c.name, status, body)
			}
		})
	}

	// Positive control: the owning team can read its own revision.
	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s", f.victimTeam, f.projectB, f.revB), nil, f.victimToken)
	if status != http.StatusOK {
		t.Fatalf("victim GetRevision: expected 200, got %d: %v", status, body)
	}
}

// TestIDOR_LedgerCrossTeam verifies the ledger/inventory guards reject another
// team's inventory entry.
func TestIDOR_LedgerCrossTeam(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	f := newIDORFixture(t)
	testutil.SetStockQty(t, testDB, f.entryB, 100)

	// Checkout against teamB's inventory entry via teamA must not deduct it.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/checkouts", f.attackerTeam),
		map[string]any{"items": []any{
			map[string]any{"inventory_entry_id": f.entryB, "quantity": 5},
		}}, f.attackerToken)
	if status != http.StatusNotFound {
		t.Errorf("attacker checkout on foreign entry: expected 404, got %d: %v", status, body)
	}

	// Reading teamB's stock ledger via teamA must be rejected.
	body, status = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/inventory/%s/adjustments", f.attackerTeam, f.entryB), nil, f.attackerToken)
	if status != http.StatusNotFound {
		t.Errorf("attacker ListAdjustments on foreign entry: expected 404, got %d: %v", status, body)
	}
}

// TestIDOR_GlobalPartsTeamParam verifies that the global /parts routes reject a
// ?team_id= the caller is not a member of.
func TestIDOR_GlobalPartsTeamParam(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	f := newIDORFixture(t)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/parts/%s?team_id=%s", f.partB, f.victimTeam), nil, f.attackerToken)
	if status != http.StatusForbidden {
		t.Errorf("attacker GetPart ?team_id=victim: expected 403, got %d: %v", status, body)
	}

	body, status = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/parts?team_id=%s", f.victimTeam), nil, f.attackerToken)
	if status != http.StatusForbidden {
		t.Errorf("attacker ListParts ?team_id=victim: expected 403, got %d: %v", status, body)
	}

	// Positive control: the owning team may pass its own team_id.
	_, status = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/parts/%s?team_id=%s", f.partB, f.victimTeam), nil, f.victimToken)
	if status != http.StatusOK {
		t.Errorf("victim GetPart ?team_id=own: expected 200, got %d", status)
	}
}
