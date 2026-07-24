package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// objectsFrom pulls the id → summary map out of an object-refs response.
func objectsFrom(t *testing.T, body map[string]any) map[string]map[string]any {
	t.Helper()
	raw, ok := body["objects"].(map[string]any)
	if !ok {
		t.Fatalf("response has no objects map: %v", body)
	}
	out := make(map[string]map[string]any, len(raw))
	for id, v := range raw {
		obj, _ := v.(map[string]any)
		out[id] = obj
	}
	return out
}

// One request resolves a mixed set of kinds, and every kind is named the way the
// rest of the app names it.
func TestListObjectRefs_MixedKinds(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "objref-mixed@example.com", "pw", "Obj")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Obj Team")
	token := testutil.CreateSession(t, testDB, userID)

	distID := testutil.GetTestDistributorID(t, testDB)
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	if _, err := testDB.Exec(context.Background(),
		`UPDATE orders SET purchase_order_number = 'PO-1042' WHERE id = $1`, orderID,
	); err != nil {
		t.Fatalf("set purchase order number: %v", err)
	}

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Blinky")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "B")
	buildID := testutil.CreateBuild(t, testDB, projectID, revID, teamID, userID, 10, "draft")
	partID := testutil.CreatePart(t, testDB, "global", "STM32F103C8T6", nil)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/object-refs?order=%s&build=%s&part=%s&project=%s&revision=%s",
			teamID, orderID, buildID, partID, projectID, revID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	objects := objectsFrom(t, body)

	cases := []struct {
		id, wantKind, wantLabel, wantHref string
	}{
		{orderID, "order", "TestDistributor #PO-1042", fmt.Sprintf("/teams/%s/orders/%s", teamID, orderID)},
		{buildID, "build", "Blinky rev B", fmt.Sprintf("/teams/%s/builds/%s", teamID, buildID)},
		{partID, "part", "STM32F103C8T6", fmt.Sprintf("/teams/%s/parts/%s", teamID, partID)},
		{projectID, "project", "Blinky", fmt.Sprintf("/teams/%s/projects/%s", teamID, projectID)},
		{revID, "revision", "Blinky rev B", fmt.Sprintf("/teams/%s/projects/%s/revisions/%s", teamID, projectID, revID)},
	}
	for _, c := range cases {
		obj, ok := objects[c.id]
		if !ok {
			t.Errorf("%s: not resolved", c.wantKind)
			continue
		}
		if obj["kind"] != c.wantKind {
			t.Errorf("%s: kind = %v", c.wantKind, obj["kind"])
		}
		if obj["label"] != c.wantLabel {
			t.Errorf("%s: label = %v, want %q", c.wantKind, obj["label"], c.wantLabel)
		}
		if obj["href"] != c.wantHref {
			t.Errorf("%s: href = %v, want %q", c.wantKind, obj["href"], c.wantHref)
		}
	}

	// The build's quantity is a qualifier, not part of its identity — so the
	// label reads in a sentence and a dense row can still tell two builds apart.
	if got := objects[buildID]["qualifier"]; got != "×10" {
		t.Errorf("build qualifier = %v, want ×10", got)
	}
}

// A generic part has no MPN, so its short description is its name. Without this
// fallback the chip renders a blank label.
func TestListObjectRefs_GenericPartUsesDescription(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "objref-generic@example.com", "pw", "Obj")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Obj Team")
	token := testutil.CreateSession(t, testDB, userID)

	var partID string
	if err := testDB.QueryRow(context.Background(),
		`INSERT INTO parts (scope, mpn, short_description) VALUES ('global', NULL, '0603 10nF 50V X7R') RETURNING id`,
	).Scan(&partID); err != nil {
		t.Fatalf("create generic part: %v", err)
	}

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/object-refs?part=%s", teamID, partID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}

	obj, ok := objectsFrom(t, body)[partID]
	if !ok {
		t.Fatalf("generic part not resolved")
	}
	if obj["label"] != "0603 10nF 50V X7R" {
		t.Errorf("label = %v, want the short description", obj["label"])
	}
}

// A team may not resolve another team's objects, however it obtained the id.
func TestListObjectRefs_CrossTeamIsolation(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	distID := testutil.GetTestDistributorID(t, testDB)

	ownerID := testutil.CreateUser(t, testDB, "objref-owner@example.com", "pw", "Owner")
	ownTeamID := testutil.CreateTeam(t, testDB, ownerID, "member", "Own Team")

	otherID := testutil.CreateUser(t, testDB, "objref-other@example.com", "pw", "Other")
	otherTeamID := testutil.CreateTeam(t, testDB, otherID, "member", "Other Team")
	foreignOrderID := testutil.CreateOrder(t, testDB, otherTeamID, distID, otherID)
	foreignProjectID := testutil.CreateProject(t, testDB, otherTeamID, otherID, "Secret")

	token := testutil.CreateSession(t, testDB, ownerID)
	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/object-refs?order=%s&project=%s",
			ownTeamID, foreignOrderID, foreignProjectID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}

	objects := objectsFrom(t, body)
	if len(objects) != 0 {
		t.Errorf("expected no objects resolved across teams, got %v", objects)
	}
}

// An id that no longer exists is absent rather than an error — a note outlives
// the object it links to, and the chip renders as unresolved.
func TestListObjectRefs_UnknownIDIsOmitted(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "objref-missing@example.com", "pw", "Obj")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Obj Team")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/object-refs?order=00000000-0000-0000-0000-000000000000&part=not-a-uuid", teamID),
		nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if objects := objectsFrom(t, body); len(objects) != 0 {
		t.Errorf("expected no objects, got %v", objects)
	}
}

func TestListObjectRefs_RejectsOversizedBatch(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "objref-big@example.com", "pw", "Obj")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Obj Team")
	token := testutil.CreateSession(t, testDB, userID)

	ids := make([]string, 201)
	for i := range ids {
		ids[i] = "00000000-0000-0000-0000-000000000000"
	}
	_, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/object-refs?part=%s", teamID, strings.Join(ids, ",")), nil, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for an oversized batch, got %d", status)
	}
}
