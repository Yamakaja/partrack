package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// awaitActivity polls the activity feed for the newest row with the given
// action. The recorder batches inserts (a couple of seconds), so a read
// immediately after the mutation legitimately sees nothing yet.
func awaitActivity(t *testing.T, teamID, token, action string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
			fmt.Sprintf("/api/v1/teams/%s/activity-log?action=%s", teamID, action), nil, token)
		if status == http.StatusOK {
			if items, _ := body["items"].([]any); len(items) > 0 {
				row, _ := items[0].(map[string]any)
				return row
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("activity row %q never appeared", action)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func subjectLabel(t *testing.T, row map[string]any) string {
	t.Helper()
	subject, ok := row["subject"].(map[string]any)
	if !ok {
		t.Fatalf("activity row carries no subject: %v", row)
	}
	label, _ := subject["label"].(string)
	return label
}

// An order is identified to users by distributor + purchase order number. Before
// the subject carried a label the feed rendered a bare "the order".
func TestActivityLabel_OrderCancelled(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "act-lbl-ord@example.com", "pw", "Act")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Act Team")
	distID := testutil.GetTestDistributorID(t, testDB)
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	token := testutil.CreateSession(t, testDB, userID)

	if _, err := testDB.Exec(context.Background(),
		`UPDATE orders SET purchase_order_number = 'PO-1042' WHERE id = $1`, orderID,
	); err != nil {
		t.Fatalf("set purchase order number: %v", err)
	}

	if _, status := testutil.Do(t, testEnv.Server, http.MethodDelete,
		fmt.Sprintf("/api/v1/teams/%s/orders/%s", teamID, orderID), nil, token); status != http.StatusNoContent {
		t.Fatalf("expected 204 on cancel, got %d", status)
	}

	if got, want := subjectLabel(t, awaitActivity(t, teamID, token, "order.cancelled")), "TestDistributor #PO-1042"; got != want {
		t.Errorf("order subject label = %q, want %q", got, want)
	}
}

// With no purchase order number and no distributor reference, an order falls
// back to its short id — the same handle the order detail page shows as a title.
func TestActivityLabel_OrderCancelled_NoReference(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "act-lbl-ord2@example.com", "pw", "Act")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Act Team")
	distID := testutil.GetTestDistributorID(t, testDB)
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	token := testutil.CreateSession(t, testDB, userID)

	if _, status := testutil.Do(t, testEnv.Server, http.MethodDelete,
		fmt.Sprintf("/api/v1/teams/%s/orders/%s", teamID, orderID), nil, token); status != http.StatusNoContent {
		t.Fatalf("expected 204 on cancel, got %d", status)
	}

	if got, want := subjectLabel(t, awaitActivity(t, teamID, token, "order.cancelled")),
		"TestDistributor "+orderID[:8]; got != want {
		t.Errorf("order subject label = %q, want %q", got, want)
	}
}

// A build is identified by the project and the BOM revision it assembles.
func TestActivityLabel_BuildCancelled(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "act-lbl-bld@example.com", "pw", "Act")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Act Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Blinky")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "B")
	buildID := testutil.CreateBuild(t, testDB, projectID, revID, teamID, userID, 2, "draft")

	if _, status := testutil.Do(t, testEnv.Server, http.MethodDelete,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s", teamID, buildID), nil, token); status != http.StatusNoContent {
		t.Fatalf("expected 204 on cancel, got %d", status)
	}

	if got, want := subjectLabel(t, awaitActivity(t, teamID, token, "build.cancelled")), "Blinky rev B"; got != want {
		t.Errorf("build subject label = %q, want %q", got, want)
	}
}
