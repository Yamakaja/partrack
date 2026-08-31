package handler_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// dashboardActions returns the actions the dashboard's activity slice reports.
func dashboardActions(t *testing.T, teamID, token string) []string {
	t.Helper()
	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/dashboard", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("dashboard: expected 200, got %d: %v", status, body)
	}
	raw, _ := body["recent_activity"].([]any)
	out := make([]string, 0, len(raw))
	for _, it := range raw {
		if m, ok := it.(map[string]any); ok {
			if a, _ := m["action"].(string); a != "" {
				out = append(out, a)
			}
		}
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// awaitDashboardAction polls until the dashboard reports want. Activity is
// batch-inserted, so a read straight after the mutation legitimately sees
// nothing yet. Returns the full action list once want lands.
func awaitDashboardAction(t *testing.T, teamID, token, want string) []string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		actions := dashboardActions(t, teamID, token)
		if contains(actions, want) {
			return actions
		}
		if time.Now().After(deadline) {
			t.Fatalf("dashboard never reported %q; saw %v", want, actions)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Receiving an order fully records one order-level milestone alongside the
// per-line detail, and only the milestone reaches the dashboard. Without the
// split, receiving a 30-line order would bury everything else on the page.
func TestDashboardActivity_ReportsOrderReceiptOnceNotPerLine(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "dash-recv@example.com", "pw", "Dash")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Dash Team")
	token := testutil.CreateSession(t, testDB, userID)

	distID := testutil.GetTestDistributorID(t, testDB)
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)

	// Two lines, so a per-line event would show up twice.
	var lineItemIDs []string
	for i, mpn := range []string{"DASH-RECV-01", "DASH-RECV-02"} {
		partID := testutil.CreatePart(t, testDB, "global", mpn, nil)
		listingID := testutil.CreateListing(t, testDB, partID, distID, fmt.Sprintf("DASH-SKU-%02d", i))
		lineItemIDs = append(lineItemIDs, testutil.CreateOrderLineItem(t, testDB, orderID, listingID, 5))
		testutil.CreateInventoryEntry(t, testDB, teamID, partID)
	}
	testutil.PlaceOrder(t, testDB, orderID)

	for _, itemID := range lineItemIDs {
		body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
			fmt.Sprintf("/api/v1/teams/%s/orders/%s/items/%s/receive", teamID, orderID, itemID),
			map[string]any{"quantity_received": 5}, token)
		if status != http.StatusOK {
			t.Fatalf("receive: expected 200, got %d: %v", status, body)
		}
	}

	// The full feed keeps every line receipt (wait for the batch first).
	awaitDashboardAction(t, teamID, token, "order.received")
	feed, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/activity-log?scope=team", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("activity-log: expected 200, got %d: %v", status, feed)
	}
	items, _ := feed["items"].([]any)
	perLine := 0
	for _, it := range items {
		if m, ok := it.(map[string]any); ok && m["action"] == "order.item_received" {
			perLine++
		}
	}
	if perLine != 2 {
		t.Errorf("full feed: expected 2 per-line receipts, got %d", perLine)
	}

	// ...while the dashboard reports the order arriving, once.
	actions := awaitDashboardAction(t, teamID, token, "order.received")
	if contains(actions, "order.item_received") {
		t.Errorf("dashboard should not replay per-line receipts, got %v", actions)
	}
}

// Routine bookkeeping stays in the feed and off the dashboard.
func TestDashboardActivity_ExcludesRoutineBookkeeping(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "dash-noise@example.com", "pw", "Dash")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Dash Team")
	token := testutil.CreateSession(t, testDB, userID)

	partID := testutil.CreatePart(t, testDB, "global", "DASH-NOISE-01", nil)
	entryID := testutil.CreateInventoryEntry(t, testDB, teamID, partID)

	// A stock correction: recorded, but not news.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory/%s/adjustments", teamID, entryID),
		map[string]any{"delta": 25, "reason": "recount"}, token)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("adjust: unexpected status %d: %v", status, body)
	}

	// A project: a milestone.
	projBody, projStatus := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects", teamID),
		map[string]any{"name": "Dash Milestone"}, token)
	if projStatus != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %v", projStatus, projBody)
	}

	actions := awaitDashboardAction(t, teamID, token, "project.created")
	if contains(actions, "inventory.adjusted") {
		t.Errorf("dashboard should not report routine stock corrections, got %v", actions)
	}

	// The exclusion is only meaningful if the adjustment was recorded at all —
	// otherwise this test would pass on a stock adjustment that silently
	// logged nothing.
	feed, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/activity-log?scope=team", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("activity-log: expected 200, got %d: %v", status, feed)
	}
	items, _ := feed["items"].([]any)
	var feedActions []string
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			if a, _ := m["action"].(string); a != "" {
				feedActions = append(feedActions, a)
			}
		}
	}
	if !contains(feedActions, "inventory.adjusted") {
		t.Errorf("full feed should still record the stock correction, got %v", feedActions)
	}
}
