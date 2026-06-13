package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/paramparse"
	"github.com/Yamakaja/partrack/internal/testutil"
	"github.com/jackc/pgx/v5/pgtype"
)

func uuid(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("scan uuid %q: %v", s, err)
	}
	return u
}

// TestEnumSetStorageAndSearch verifies that an enum_set parameter stores its parsed
// member array and that value search matches via contains-any (overlap).
func TestEnumSetStorageAndSearch(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	ctx := context.Background()
	q := dbgen.New(testDB)

	// parameter_definitions is seeded reference data (not truncated between tests),
	// so upsert idempotently rather than plain-insert.
	var protocolsID int32
	if err := testDB.QueryRow(ctx,
		`INSERT INTO parameter_definitions (slug, display_name, value_type)
		 VALUES ('protocols', 'Protocols', 'enum_set')
		 ON CONFLICT (slug) DO UPDATE SET value_type = 'enum_set' RETURNING id`).Scan(&protocolsID); err != nil {
		t.Fatalf("seed enum_set def: %v", err)
	}

	distID := uuid(t, testutil.GetTestDistributorID(t, testDB))
	partA := testutil.CreatePart(t, testDB, "global", "ENUMSET-A", nil)
	partB := testutil.CreatePart(t, testDB, "global", "ENUMSET-B", nil)

	insert := func(partID, value string) {
		t.Helper()
		if _, err := q.InsertDistributorParameter(ctx, dbgen.InsertDistributorParameterParams{
			PartID:              uuid(t, partID),
			Name:                "Protocols",
			Value:               value,
			SourceDistributorID: distID,
			ParameterID:         &protocolsID,
			EnumValues:          paramparse.ParseEnumSet(value),
		}); err != nil {
			t.Fatalf("insert %q: %v", value, err)
		}
	}
	insert(partA, "CANbus, Ethernet, SPI")
	insert(partB, "USB, I2C")

	// The parsed array landed in enum_values.
	var stored []string
	if err := testDB.QueryRow(ctx,
		`SELECT enum_values FROM part_parameters WHERE part_id = $1`, partA).Scan(&stored); err != nil {
		t.Fatalf("read enum_values: %v", err)
	}
	if len(stored) != 3 || stored[0] != "CANbus" || stored[1] != "Ethernet" || stored[2] != "SPI" {
		t.Errorf("enum_values = %v, want [CANbus Ethernet SPI]", stored)
	}

	find := func(values ...string) []string {
		t.Helper()
		ids, err := q.FindPartIDsByParameterValues(ctx, dbgen.FindPartIDsByParameterValuesParams{
			ParameterID: &protocolsID,
			Column2:     values,
		})
		if err != nil {
			t.Fatalf("find %v: %v", values, err)
		}
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = id.String()
		}
		return out
	}

	if got := find("Ethernet"); len(got) != 1 || got[0] != partA {
		t.Errorf("find Ethernet = %v, want [%s]", got, partA)
	}
	if got := find("USB"); len(got) != 1 || got[0] != partB {
		t.Errorf("find USB = %v, want [%s]", got, partB)
	}
	if got := find("Ethernet", "USB"); len(got) != 2 { // contains-any → both
		t.Errorf("find Ethernet,USB = %v, want both parts", got)
	}
	if got := find("Bluetooth"); len(got) != 0 {
		t.Errorf("find Bluetooth = %v, want none", got)
	}
}

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
