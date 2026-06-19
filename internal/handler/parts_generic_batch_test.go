package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// TestBatchCreateGenericParts_DryRunThenCreate exercises the full batch lifecycle:
// an E-series dry run plans without writing, the real run creates every part, a
// repeat run is a pure no-op (create-or-get), and the single-create path dedups
// against a batch-created part (proving signature consistency across paths).
func TestBatchCreateGenericParts_DryRunThenCreate(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "batch@example.com", "pw", "Batch")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Batch Team")
	token := testutil.CreateSession(t, testDB, userID)

	batch := func(dryRun bool) (map[string]any, int) {
		return testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/parts/generic/batch", map[string]any{
			"team_id":     teamID,
			"category_id": resistorsCategoryID,
			"package":     "0603",
			"values":      map[string]any{"mode": "eseries", "series": "E12", "lower": "1k", "upper": "10k"},
			"dry_run":     dryRun,
		}, token)
	}

	countParts := func() int {
		var n int
		if err := testDB.QueryRow(context.Background(),
			"SELECT count(*) FROM parts WHERE team_id = $1", teamID).Scan(&n); err != nil {
			t.Fatalf("count parts: %v", err)
		}
		return n
	}

	// 1. Dry run plans 13 E12 values across [1k, 10k] and writes nothing.
	body, status := batch(true)
	if status != http.StatusOK {
		t.Fatalf("dry run: expected 200, got %d: %v", status, body)
	}
	if body["dry_run"] != true {
		t.Errorf("dry run: expected dry_run=true, got %v", body["dry_run"])
	}
	if body["total"] != float64(13) || body["to_create"] != float64(13) || body["existing"] != float64(0) {
		t.Errorf("dry run: total/to_create/existing = %v/%v/%v, want 13/13/0", body["total"], body["to_create"], body["existing"])
	}
	if n := countParts(); n != 0 {
		t.Fatalf("dry run wrote %d parts, expected 0", n)
	}
	// The plan carries synthesized identities.
	items, _ := body["items"].([]any)
	foundMPN := false
	for _, it := range items {
		m := it.(map[string]any)
		if m["mpn"] == "GEN-RES-10K-0603" {
			foundMPN = true
			if m["existing"] != false {
				t.Errorf("dry run: 10k item existing=%v, want false", m["existing"])
			}
		}
	}
	if !foundMPN {
		t.Errorf("dry run: missing GEN-RES-10K-0603 in plan: %v", items)
	}

	// 2. Real run creates all 13.
	body, status = batch(false)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %v", status, body)
	}
	if body["created"] != float64(13) || body["existing"] != float64(0) {
		t.Errorf("create: created/existing = %v/%v, want 13/0", body["created"], body["existing"])
	}
	if n := countParts(); n != 13 {
		t.Fatalf("after create: %d parts, expected 13", n)
	}

	// 3. Re-running is a pure no-op (create-or-get).
	body, status = batch(false)
	if status != http.StatusCreated {
		t.Fatalf("recreate: expected 201, got %d: %v", status, body)
	}
	if body["created"] != float64(0) || body["existing"] != float64(13) {
		t.Errorf("recreate: created/existing = %v/%v, want 0/13", body["created"], body["existing"])
	}
	if n := countParts(); n != 13 {
		t.Fatalf("after recreate: %d parts, expected 13 (no duplicates)", n)
	}

	// 4. The single-create path dedups against a batch-created part — the 10k 0603
	//    generic already exists, so this returns it (200, existing=true).
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/parts/generic", map[string]any{
		"team_id":     teamID,
		"category_id": resistorsCategoryID,
		"package":     "0603",
		"params":      []map[string]any{{"slug": "resistance", "value": "10k"}},
	}, token)
	if status != http.StatusOK {
		t.Fatalf("cross-path dedup: expected 200, got %d: %v", status, body)
	}
	if body["existing"] != true {
		t.Errorf("cross-path dedup: expected existing=true, got %v", body["existing"])
	}
}

// TestBatchCreateGenericParts_ListMode covers the explicit-list value set and the
// application of a fixed secondary parameter to every created part.
func TestBatchCreateGenericParts_ListMode(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "batchlist@example.com", "pw", "Batch")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Batch Team")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/parts/generic/batch", map[string]any{
		"team_id":     teamID,
		"category_id": resistorsCategoryID,
		"package":     "0402",
		"values":      map[string]any{"mode": "list", "list": []any{"4k7, 10k, 4k7"}},
		"params":      []map[string]any{{"slug": "tolerance", "value": "1%"}},
		"dry_run":     false,
	}, token)
	if status != http.StatusCreated {
		t.Fatalf("list create: expected 201, got %d: %v", status, body)
	}
	// "4k7" appears twice but is deduped → 2 distinct parts.
	if body["created"] != float64(2) {
		t.Errorf("list create: created=%v, want 2", body["created"])
	}
	items, _ := body["items"].([]any)
	got := map[string]bool{}
	for _, it := range items {
		got[it.(map[string]any)["mpn"].(string)] = true
	}
	for _, want := range []string{"GEN-RES-4K7-0402-1%", "GEN-RES-10K-0402-1%"} {
		if !got[want] {
			t.Errorf("list create: missing %s in %v", want, got)
		}
	}
}

// TestBatchCreateGenericParts_RejectsUnconfiguredCategory verifies the endpoint is
// restricted to categories that have a generic_code.
func TestBatchCreateGenericParts_RejectsUnconfiguredCategory(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "batchreject@example.com", "pw", "Batch")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Batch Team")
	token := testutil.CreateSession(t, testDB, userID)

	var catID int32
	if err := testDB.QueryRow(context.Background(),
		"SELECT id FROM categories WHERE generic_code IS NULL LIMIT 1").Scan(&catID); err != nil {
		t.Fatalf("find non-generic category: %v", err)
	}

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/parts/generic/batch", map[string]any{
		"team_id":     teamID,
		"category_id": catID,
		"package":     "0603",
		"values":      map[string]any{"mode": "eseries", "series": "E12", "lower": "1k", "upper": "10k"},
	}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for unconfigured category, got %d: %v", status, body)
	}
}
