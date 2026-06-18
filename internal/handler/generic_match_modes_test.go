package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// categoryIDBySlug returns a seeded category's id (migration 000015).
func categoryIDBySlug(t *testing.T, slug string) int32 {
	t.Helper()
	var id int32
	if err := testDB.QueryRow(context.Background(),
		`SELECT id FROM categories WHERE slug = $1`, slug).Scan(&id); err != nil {
		t.Fatalf("category %q: %v", slug, err)
	}
	return id
}

// paramDefID returns the id of a parameter definition, creating it if absent.
// Some params (pin-header positions/rows/pitch) are not in the migration seed —
// in production they are minted on first distributor enrichment — so the test
// mints them idempotently.
func paramDefID(t *testing.T, slug, valueType string) int32 {
	t.Helper()
	var id int32
	if err := testDB.QueryRow(context.Background(),
		`INSERT INTO parameter_definitions (slug, display_name, value_type)
		 VALUES ($1, $1, $2)
		 ON CONFLICT (slug) DO UPDATE SET slug = EXCLUDED.slug
		 RETURNING id`, slug, valueType).Scan(&id); err != nil {
		t.Fatalf("param def %q: %v", slug, err)
	}
	return id
}

// seedCatalogPart inserts a catalog part with an optional canonical package and a
// set of numeric parameters (parameter_id → numeric value), returning its id.
func seedCatalogPart(t *testing.T, scope string, teamID *string, mpn string, categoryID int32, canonicalPkg *string, params map[int32]float64) string {
	t.Helper()
	var id string
	if err := testDB.QueryRow(context.Background(),
		`INSERT INTO parts (scope, mpn, team_id, category_id, canonical_package, lifecycle_status)
		 VALUES ($1::part_scope, $2, $3, $4, $5, 'active') RETURNING id`,
		scope, mpn, teamID, categoryID, canonicalPkg,
	).Scan(&id); err != nil {
		t.Fatalf("seed catalog part %q: %v", mpn, err)
	}
	for pid, v := range params {
		if _, err := testDB.Exec(context.Background(),
			`INSERT INTO part_parameters (part_id, parameter_id, name, value, numeric_value, source_type)
			 VALUES ($1::uuid, $2, 'p', $3, $4, 'user')`,
			id, pid, fmt.Sprintf("%g", v), v,
		); err != nil {
			t.Fatalf("seed param %d on %q: %v", pid, mpn, err)
		}
	}
	return id
}

// importLine imports a single BOM line carrying an explicit interpretation, and
// returns the new revision id.
func importLine(t *testing.T, teamID, projectID, token string, item map[string]any) string {
	t.Helper()
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions", teamID, projectID),
		map[string]any{"label": "v1.0", "set_active": true, "items": []any{item}}, token)
	if status != http.StatusAccepted {
		t.Fatalf("import: expected 202, got %d: %v", status, body)
	}
	revID, _ := body["bom_revision_id"].(string)
	return revID
}

// TestBOMImport_PackageOnlyGeneric verifies that a generic line carrying only a
// category + package (no primary magnitude — e.g. a bare "LED") resolves to the
// unique catalog part in that category + package.
func TestBOMImport_PackageOnlyGeneric(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "pkgonly@example.com", "pw", "Pkg")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Pkg Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-PKG")
	token := testutil.CreateSession(t, testDB, userID)

	ledCat := categoryIDBySlug(t, "led-indication-discrete")
	pkg := "0402"
	ledPart := seedCatalogPart(t, "team", &teamID, "XZ-LED-0402", ledCat, &pkg, nil)

	interp := map[string]any{
		"route": "parametric", "class": "LED", "category": "led-indication-discrete",
		"mounting": "smd",
		"params":   []any{map[string]any{"slug": "package", "raw": "0402", "value_si": nil}},
	}
	rev := importLine(t, teamID, projectID, token, map[string]any{
		"reference_designators": "D1", "quantity": 1,
		"value": "LED", "footprint": "LED_SMD:LED_0402_1005Metric",
		"interpretation": interp,
	})

	line := pollResolvedLine(t, teamID, projectID, token, rev)
	if line["resolution_state"] != "auto_generic" {
		t.Errorf("resolution_state = %v, want auto_generic", line["resolution_state"])
	}
	if line["resolved_part_id"] != ledPart {
		t.Errorf("resolved_part_id = %v, want %s", line["resolved_part_id"], ledPart)
	}
}

// TestBOMImport_GenericAmbiguous verifies that a generic value line with more than
// one satisfying catalog part is left ambiguous (not auto-resolved), and that the
// review-gate candidate picker then surfaces both.
func TestBOMImport_GenericAmbiguous(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "amb@example.com", "pw", "Amb")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Amb Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-AMB")
	token := testutil.CreateSession(t, testDB, userID)

	chipR := categoryIDBySlug(t, "chip-resistor-surface-mount")
	res := paramDefID(t, "resistance", "numeric")
	pkg := "0603"
	// Two distinct 10k/0603 catalog parts → no unique match.
	p1 := seedCatalogPart(t, "global", nil, "RES-10K-A", chipR, &pkg, map[int32]float64{res: 10000})
	p2 := seedCatalogPart(t, "global", nil, "RES-10K-B", chipR, &pkg, map[int32]float64{res: 10000})

	rev := importLine(t, teamID, projectID, token, map[string]any{
		"reference_designators": "R1", "quantity": 1,
		"value": "10k", "footprint": "R_0603_1608Metric",
	})

	item := pollResolvedLine(t, teamID, projectID, token, rev)
	if item["resolution_state"] != "ambiguous" {
		t.Errorf("resolution_state = %v, want ambiguous", item["resolution_state"])
	}
	if item["resolved_part_id"] != nil {
		t.Errorf("resolved_part_id = %v, want nil", item["resolved_part_id"])
	}

	// The candidate picker offers both.
	itemID, _ := item["id"].(string)
	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/items/%s/generic-candidates",
			teamID, projectID, rev, itemID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("candidates: got %d: %v", status, body)
	}
	got := map[string]bool{}
	for _, c := range body["items"].([]any) {
		got[c.(map[string]any)["part_id"].(string)] = true
	}
	if !got[p1] || !got[p2] {
		t.Errorf("expected both %s and %s among candidates, got %v", p1, p2, got)
	}
}

// TestBOMImport_SignatureGeneric verifies multi-parameter (no-package) matching: a
// pin-header line described by positions/rows/pitch resolves to the unique catalog
// header with that exact signature, and a header with a different position count is
// not matched.
func TestBOMImport_SignatureGeneric(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sig@example.com", "pw", "Sig")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Sig Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-SIG")
	token := testutil.CreateSession(t, testDB, userID)

	hdrCat := categoryIDBySlug(t, "headers-male-pins")
	pos := paramDefID(t, "number_of_positions", "numeric")
	rows := paramDefID(t, "number_of_rows", "numeric")
	pitch := paramDefID(t, "pitch_mating", "numeric")

	// 1x04 @ 2.54mm — the intended match — and a 1x06 that must NOT match.
	want := seedCatalogPart(t, "global", nil, "HDR-1x04", hdrCat, nil,
		map[int32]float64{pos: 4, rows: 1, pitch: 0.00254})
	_ = seedCatalogPart(t, "global", nil, "HDR-1x06", hdrCat, nil,
		map[int32]float64{pos: 6, rows: 1, pitch: 0.00254})

	num := func(v float64) *float64 { return &v }
	interp := map[string]any{
		"route": "parametric", "class": "J", "category": "headers-male-pins",
		"mounting": "tht",
		"params": []any{
			map[string]any{"slug": "number_of_positions", "raw": "4", "value_si": num(4)},
			map[string]any{"slug": "number_of_rows", "raw": "1", "value_si": num(1)},
			map[string]any{"slug": "pitch_mating", "raw": "2.54mm", "value_si": num(0.00254)},
		},
	}
	rev := importLine(t, teamID, projectID, token, map[string]any{
		"reference_designators": "J1", "quantity": 1,
		"value": "Conn_01x04_Pin", "footprint": "Connector_PinHeader_2.54mm:PinHeader_1x04_P2.54mm_Vertical",
		"interpretation": interp,
	})

	line := pollResolvedLine(t, teamID, projectID, token, rev)
	if line["resolution_state"] != "auto_generic" {
		t.Errorf("resolution_state = %v, want auto_generic", line["resolution_state"])
	}
	if line["resolved_part_id"] != want {
		t.Errorf("resolved_part_id = %v, want %s (HDR-1x04)", line["resolved_part_id"], want)
	}
}
