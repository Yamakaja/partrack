package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// seedUnmappedDistributorParam creates a global part with two distributor-sourced
// parameters (initially unmapped) and an unmapped mapping row for them. Returns
// the mapping id and the resistance definition id.
func seedUnmappedDistributorParam(t *testing.T, ctx context.Context) (mappingID, partID string, resistanceID int32) {
	t.Helper()
	partID = testutil.CreatePart(t, testDB, "global", "REPARSE-MPN", nil)
	distID := testutil.GetTestDistributorID(t, testDB)

	if err := testDB.QueryRow(ctx,
		`SELECT id FROM parameter_definitions WHERE slug = 'resistance'`).Scan(&resistanceID); err != nil {
		t.Fatalf("lookup resistance def: %v", err)
	}

	if err := testDB.QueryRow(ctx,
		`INSERT INTO distributor_parameter_mappings (distributor_id, distributor_param_id, distributor_param_name, parameter_id)
		 VALUES ($1, 'P-RES', 'Resistance', NULL) RETURNING id`, distID).Scan(&mappingID); err != nil {
		t.Fatalf("seed mapping: %v", err)
	}

	for _, v := range []string{"10k", "4.7k"} {
		if _, err := testDB.Exec(ctx,
			`INSERT INTO part_parameters (part_id, name, value, source_type, source_distributor_id)
			 VALUES ($1, 'Resistance', $2, 'distributor', $3)`, partID, v, distID); err != nil {
			t.Fatalf("seed part parameter %q: %v", v, err)
		}
	}
	return mappingID, partID, resistanceID
}

// TestLinkMappingBackfillsAndReparses verifies that linking an unmapped
// distributor parameter back-fills existing part_parameters with the canonical
// parameter_id and parses their numeric values, without waiting for re-enrichment.
func TestLinkMappingBackfillsAndReparses(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	ctx := context.Background()

	mappingID, partID, resistanceID := seedUnmappedDistributorParam(t, ctx)

	userID := testutil.CreateAdminUser(t, testDB, "reparse-admin@example.com", "pw", "Admin")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/admin/distributor-parameter-mappings/%s", mappingID),
		map[string]any{"parameter_id": resistanceID}, token)
	if status != http.StatusOK {
		t.Fatalf("link mapping: expected 200, got %d: %v", status, body)
	}
	if got := body["reparsed_count"]; got != float64(2) {
		t.Errorf("reparsed_count = %v, want 2", got)
	}

	// Both rows now carry the canonical parameter_id and a parsed numeric value.
	rows, err := testDB.Query(ctx,
		`SELECT value, parameter_id, numeric_value FROM part_parameters
		 WHERE part_id = $1 AND source_type = 'distributor' ORDER BY value`, partID)
	if err != nil {
		t.Fatalf("query part parameters: %v", err)
	}
	defer rows.Close()
	want := map[string]float64{"10k": 10000, "4.7k": 4700}
	seen := 0
	for rows.Next() {
		var value string
		var paramID *int32
		var numeric *float64
		if err := rows.Scan(&value, &paramID, &numeric); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen++
		if paramID == nil || *paramID != resistanceID {
			t.Errorf("%q: parameter_id = %v, want %d", value, paramID, resistanceID)
		}
		if numeric == nil || *numeric != want[value] {
			t.Errorf("%q: numeric_value = %v, want %v", value, numeric, want[value])
		}
	}
	if seen != 2 {
		t.Errorf("scanned %d rows, want 2", seen)
	}
}

// TestParseMappingPreview verifies the admin parse-preview parses a mapping's real
// sample values under a chosen value_type.
func TestParseMappingPreview(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	ctx := context.Background()

	mappingID, _, _ := seedUnmappedDistributorParam(t, ctx)

	userID := testutil.CreateAdminUser(t, testDB, "preview-admin@example.com", "pw", "Admin")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/admin/distributor-parameter-mappings/%s/parse-preview?value_type=numeric", mappingID),
		nil, token)
	if status != http.StatusOK {
		t.Fatalf("parse-preview: expected 200, got %d: %v", status, body)
	}
	results, ok := body["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("results: got %v, want 2 entries", body["results"])
	}
	want := map[string]float64{"10k": 10000, "4.7k": 4700}
	for _, r := range results {
		m := r.(map[string]any)
		v := m["value"].(string)
		if m["parsed"] != true {
			t.Errorf("%q: parsed = %v, want true", v, m["parsed"])
		}
		if m["numeric_value"] != want[v] {
			t.Errorf("%q: numeric_value = %v, want %v", v, m["numeric_value"], want[v])
		}
	}
}
