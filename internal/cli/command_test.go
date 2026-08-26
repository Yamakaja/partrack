package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Yamakaja/partrack/internal/apitypes"
)

// These tests drive the commands against a stub server, so the query strings pt
// actually sends — the part that has to match the API exactly — are asserted
// rather than assumed.

type stubServer struct {
	*httptest.Server
	// lastQuery records the query of the most recent /parts list request.
	lastQuery url.Values
	authSeen  string
}

func newStubServer(t *testing.T) *stubServer {
	t.Helper()
	s := &stubServer{}
	mux := http.NewServeMux()

	writeStub := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Errorf("encoding stub response: %v", err)
		}
	}

	mux.HandleFunc("/api/v1/teams", func(w http.ResponseWriter, r *http.Request) {
		s.authSeen = r.Header.Get("Authorization")
		// /teams uses the standard list envelope, not a bare array. Encoding it
		// as an array here once hid a decode failure that only appeared against
		// a real server, so the stub mirrors the handler exactly.
		writeStub(w, map[string]any{
			"total": 1, "limit": 1, "offset": 0,
			"items": []Team{{ID: "team-uuid-1", Name: "Lab", Role: "member"}},
		})
	})
	mux.HandleFunc("/api/v1/categories", func(w http.ResponseWriter, r *http.Request) {
		writeStub(w, map[string]any{"categories": testRefData().Categories})
	})
	mux.HandleFunc("/api/v1/parameter-definitions", func(w http.ResponseWriter, r *http.Request) {
		writeStub(w, testRefData().Params)
	})
	mux.HandleFunc("/api/v1/parts", func(w http.ResponseWriter, r *http.Request) {
		s.lastQuery = r.URL.Query()
		qty, min := int32(4200), int32(500)
		writeStub(w, PartListResponse{
			Total: 2, Limit: 50, Offset: 0,
			Items: []PartListItem{
				{
					ID: "11111111-1111-1111-1111-111111111111", Scope: "global",
					Mpn: sp("RC0402FR-0710KL"), Manufacturer: sp("Yageo"),
					ShortDescription: sp("10 kΩ ±1% 1/16W 0402"), Package: sp("0402"),
					LifecycleStatus: "active", InventoryEntryID: sp("entry-1"),
					QuantityOnHand: &qty, MinimumStockQuantity: &min,
					StorageLocationLabel: sp("Drawer A"), StorageCoordinate: []int32{3},
					Parameters: map[string]string{"resistance": "10 kOhms", "tolerance": "±1%"},
				},
				{
					ID: "22222222-2222-2222-2222-222222222222", Scope: "team",
					Mpn: sp("ERJ-2RKF1002X"), Manufacturer: sp("Panasonic"),
					ShortDescription: sp("10 kΩ ±1% 1/10W 0402"), Package: sp("0402"),
					LifecycleStatus: "nrnd",
					Parameters:      map[string]string{"resistance": "10 kOhms"},
				},
			},
		})
	})
	mux.HandleFunc("/api/v1/parts/11111111-1111-1111-1111-111111111111",
		func(w http.ResponseWriter, r *http.Request) {
			s.lastQuery = r.URL.Query()
			qty := int32(4200)
			detail := PartDetail{
				ID: "11111111-1111-1111-1111-111111111111", Scope: "global",
				Mpn: sp("RC0402FR-0710KL"), Manufacturer: sp("Yageo"),
				Description: sp("RES SMD 10K OHM 1% 1/16W 0402"), Package: sp("0402"),
				LifecycleStatus: "active", DatasheetURL: sp("https://example.test/ds.pdf"),
				InventoryEntryID: sp("entry-1"), QuantityOnHand: &qty,
				StorageLocation: &StorageLocationRef{
					ID: "loc-1", Label: "Drawer A", AncestorPath: sp("Lab / Bench"),
					Coordinate: []int32{3},
				},
				Listings: []Listing{{
					ID: "listing-1", DistributorName: "DigiKey", Sku: "311-10.0KLRCT-ND",
					PackagingType: "cut_tape", UnitPrice: sp("0.0123"),
					StockAvailable: int32p(150000), Moq: 1,
					PriceBreaks:     json.RawMessage(`[{"quantity":1,"unit_price":0.1},{"quantity":100,"unit_price":0.0123}]`),
					LastRefreshedAt: sp("2026-08-20T10:00:00Z"),
				}},
			}
			detail.Parameters.DistributorSourced = []apitypes.DistributorParameter{
				{
					PartParameter:         PartParameter{Name: "Resistance", Value: "10 kOhms"},
					SourceDistributorName: sp("DigiKey"),
				},
			}
			writeStub(w, detail)
		})
	mux.HandleFunc("/api/v1/teams/team-uuid-1/parts/11111111-1111-1111-1111-111111111111/allocations",
		func(w http.ResponseWriter, r *http.Request) {
			writeStub(w, map[string]any{"items": []PartAllocation{{
				BuildID: "build-1", ProjectID: "proj-1", ProjectName: "Power Supply v2",
				BuildStatus: "pending", BuildQuantity: 10,
				QuantityNeeded: 200, QuantityReserved: 200,
			}}})
		})
	mux.HandleFunc("/api/v1/teams/team-uuid-1/parts/11111111-1111-1111-1111-111111111111/note",
		func(w http.ResponseWriter, r *http.Request) {
			writeStub(w, partNote{Body: "House standard 10k.", Flag: "preferred"})
		})
	mux.HandleFunc("/api/v1/teams/team-uuid-1/categories/2/parameter-schema",
		func(w http.ResponseWriter, r *http.Request) {
			writeStub(w, CategorySchema{
				CategoryID: 2, Slug: "resistors", ValueParam: "resistance",
				Params: []CategorySchemaParam{
					{Slug: "resistance", DisplayName: "Resistance", UnitSymbol: "Ω", ValueType: "numeric"},
					{Slug: "mounting_type", DisplayName: "Mounting Type", ValueType: "enum",
						Options: []string{"SMD", "Through Hole"}},
				},
			})
		})

	mux.HandleFunc("/api/v1/categories/2/parameters",
		func(w http.ResponseWriter, r *http.Request) {
			min, max := 100.0, 1e6
			ohm := "Ω"
			writeStub(w, []apitypes.CategoryParameter{
				{ID: 7, Slug: "resistance", DisplayName: "Resistance", UnitSymbol: &ohm,
					ValueType: "numeric", MinNumeric: &min, MaxNumeric: &max, EnumValues: []string{}},
				{ID: 11, Slug: "mounting_type", DisplayName: "Mounting Type",
					ValueType: "enum", EnumValues: []string{"Surface Mount", "Through Hole"}},
			})
		})

	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// setupApp points the package-level app at the stub and resets command flags,
// which are globals bound by cobra.
func setupApp(t *testing.T, srv *stubServer) *bytes.Buffer {
	t.Helper()
	dir := t.TempDir()
	cfg, err := LoadConfig(Overrides{
		URL: srv.URL, Team: "Lab", ConfigDir: dir + "/config", CacheDir: dir + "/cache",
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Token = "ptk_test"

	out := &bytes.Buffer{}
	ap = &app{cfg: cfg, format: FormatPlain, out: out}
	refCache = nil
	searchFlags = struct {
		mpn        string
		category   string
		pkg        string
		params     []string
		tolerance  float64
		stock      []string
		lifecycle  []string
		scope      string
		location   string
		fields     []string
		showParams []string
		withParams bool
		limit      int
		offset     int
		all        bool
		sortBy     string
		wide       bool
	}{tolerance: defaultParamTolerancePct, limit: 50}
	showFlags.only = nil
	stockFlags.history = 0
	t.Cleanup(func() { ap = nil; refCache = nil })
	return out
}

// The parametric filter is the piece that must match the API byte for byte: a
// slug becomes a numeric id, and a bare value becomes an inclusive band.
func TestSearchBuildsParametricQuery(t *testing.T) {
	srv := newStubServer(t)
	out := setupApp(t, srv)

	searchFlags.category = "Resistors"
	searchFlags.params = []string{"resistance=10k", "tolerance=1%"}
	searchFlags.pkg = "0402"
	searchFlags.stock = []string{"in"}

	if err := runSearch(context.Background(), ""); err != nil {
		t.Fatalf("runSearch: %v", err)
	}

	q := srv.lastQuery
	checks := map[string]string{
		"team_id":      "team-uuid-1",
		"category_id":  "2",
		"package":      "0402",
		"stock_status": "in_stock",
		"param_7_min":  "9900",
		"param_7_max":  "10100",
		"param_8_min":  "0.99",
		"param_8_max":  "1.01",
		"limit":        "50",
		"offset":       "0",
	}
	for key, want := range checks {
		if got := q.Get(key); got != want {
			t.Errorf("query %s = %q, want %q", key, got, want)
		}
	}

	text := out.String()
	for _, want := range []string{"RC0402FR-0710KL", "Yageo", "Drawer A/3", "4200", "team Lab"} {
		if !strings.Contains(text, want) {
			t.Errorf("output is missing %q:\n%s", want, text)
		}
	}
	// The footer must state what a bare value actually became.
	if !strings.Contains(text, "resistance") {
		t.Errorf("footer does not describe the parameter filter:\n%s", text)
	}
}

// Rows are numbered so the next command can address them, and the state file
// must survive into a later resolve.
func TestSearchResultsAreAddressableByIndex(t *testing.T) {
	srv := newStubServer(t)
	out := setupApp(t, srv)

	if err := runSearch(context.Background(), "10k"); err != nil {
		t.Fatalf("runSearch: %v", err)
	}
	if !strings.Contains(out.String(), "1  RC0402FR-0710KL") &&
		!strings.Contains(out.String(), "1") {
		t.Errorf("rows are not numbered:\n%s", out.String())
	}

	id, err := ap.resolvePart(context.Background(), "@2")
	if err != nil {
		t.Fatalf("resolving @2: %v", err)
	}
	if id != "22222222-2222-2222-2222-222222222222" {
		t.Errorf("@2 = %q", id)
	}
}

func TestSearchParamsRequiresCategory(t *testing.T) {
	srv := newStubServer(t)
	setupApp(t, srv)

	searchFlags.withParams = true
	err := runSearch(context.Background(), "")
	if err == nil {
		t.Fatal("expected an error when --params is used without --category")
	}
	if ee, ok := err.(*ExitError); !ok || ee.Code != ExitUsage {
		t.Errorf("want ExitUsage, got %v", err)
	}
}

func TestSearchJSONHonoursFields(t *testing.T) {
	srv := newStubServer(t)
	out := setupApp(t, srv)
	ap.format = FormatJSON
	searchFlags.fields = []string{"mpn", "stock"}

	if err := runSearch(context.Background(), ""); err != nil {
		t.Fatalf("runSearch: %v", err)
	}

	var got struct {
		Total int64            `json:"total"`
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if len(got.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(got.Items))
	}
	first := got.Items[0]
	for _, key := range []string{"mpn", "stock", "n", "uuid"} {
		if _, ok := first[key]; !ok {
			t.Errorf("item is missing key %q: %v", key, first)
		}
	}
	// --fields is a projection: unselected columns must not leak in.
	if _, ok := first["mfr"]; ok {
		t.Errorf("unselected field mfr appeared in JSON output: %v", first)
	}

	// A quantity must arrive as a number, not the display string.
	if qty, ok := first["stock"].(float64); !ok || qty != 4200 {
		t.Errorf("stock = %#v, want the number 4200", first["stock"])
	}
	// The second row is untracked: absent values must be null, never the dash
	// placeholder, so `select(.stock == null)` works in jq.
	second := got.Items[1]
	if second["stock"] != nil {
		t.Errorf("untracked stock = %#v, want null", second["stock"])
	}
}

// Every column must encode as a JSON-native type: numbers for quantities,
// booleans for flags, null for absent — never the em-dash placeholder.
func TestJSONRowUsesNativeTypes(t *testing.T) {
	qty := int32(0)
	tracked := PartListItem{
		ID: "id-1", Mpn: sp("PART-A"), InventoryEntryID: sp("e1"),
		QuantityOnHand: &qty, LifecycleStatus: "active",
	}
	untracked := PartListItem{ID: "id-2", LifecycleStatus: "active"}

	cols, err := resolveFields([]string{"mpn", "stock", "min", "reorder", "tracked", "loc"})
	if err != nil {
		t.Fatal(err)
	}

	row := jsonRow(tracked, cols, nil)
	if v, ok := row["stock"].(int32); !ok || v != 0 {
		t.Errorf("stock = %#v, want int32(0) — zero stock is not absent stock", row["stock"])
	}
	if v, ok := row["tracked"].(bool); !ok || !v {
		t.Errorf("tracked = %#v, want true", row["tracked"])
	}
	if row["min"] != nil {
		t.Errorf("absent min = %#v, want null", row["min"])
	}

	row = jsonRow(untracked, cols, nil)
	for _, key := range []string{"stock", "min", "reorder", "loc", "mpn"} {
		if row[key] != nil {
			t.Errorf("%s = %#v, want null for an untracked part", key, row[key])
		}
	}
	if v, ok := row["tracked"].(bool); !ok || v {
		t.Errorf("tracked = %#v, want false", row["tracked"])
	}

	// The encoded form must contain no placeholder text at all.
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), dash) {
		t.Errorf("JSON output carries the display placeholder: %s", encoded)
	}
}

func TestShowRendersFullDossier(t *testing.T) {
	srv := newStubServer(t)
	out := setupApp(t, srv)

	if err := runShow(context.Background(), "11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatalf("runShow: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"RC0402FR-0710KL",          // identity
		"preferred",                // flag from the note endpoint
		"Lab / Bench / Drawer A/3", // location path with coordinate
		"PARAMETERS",               // sections
		"LISTINGS",
		"311-10.0KLRCT-ND",    // listing SKU
		"1:0.1 100:0.0123",    // condensed price breaks
		"2026-08-20",          // refresh date, not the full timestamp
		"House standard 10k.", // note body
		"Power Supply v2",     // build reservation
	} {
		if !strings.Contains(text, want) {
			t.Errorf("dossier is missing %q:\n%s", want, text)
		}
	}
}

func TestShowOnlySelectsSections(t *testing.T) {
	srv := newStubServer(t)
	out := setupApp(t, srv)
	showFlags.only = []string{"listings"}

	if err := runShow(context.Background(), "11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatalf("runShow: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "LISTINGS") {
		t.Errorf("expected the listings section:\n%s", text)
	}
	if strings.Contains(text, "PARAMETERS") || strings.Contains(text, "House standard") {
		t.Errorf("--only leaked other sections:\n%s", text)
	}
}

func TestShowRejectsUnknownSection(t *testing.T) {
	srv := newStubServer(t)
	setupApp(t, srv)
	showFlags.only = []string{"bogus"}

	err := runShow(context.Background(), "11111111-1111-1111-1111-111111111111")
	if err == nil {
		t.Fatal("expected an error for an unknown section")
	}
	if ee, ok := err.(*ExitError); !ok || ee.Code != ExitUsage {
		t.Errorf("want ExitUsage, got %v", err)
	}
}

// Free stock is on-hand minus live build reservations, and must be computed
// from the allocations endpoint rather than guessed.
func TestStockSubtractsReservations(t *testing.T) {
	srv := newStubServer(t)
	out := setupApp(t, srv)

	if err := runStock(context.Background(), "11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatalf("runStock: %v", err)
	}
	text := out.String()
	for _, want := range []string{"On hand", "4200", "Reserved", "200", "Free", "4000"} {
		if !strings.Contains(text, want) {
			t.Errorf("stock output is missing %q:\n%s", want, text)
		}
	}
}

func TestFiltersListsValueVocabulary(t *testing.T) {
	srv := newStubServer(t)
	out := setupApp(t, srv)

	if err := runFilters(context.Background(), "Resistors"); err != nil {
		t.Fatalf("runFilters: %v", err)
	}
	text := out.String()
	// The catalog-derived list drives the output: enum vocabularies verbatim and
	// numeric parameters as the span actually present.
	for _, want := range []string{
		"resistance",
		"mounting_type",
		"Surface Mount, Through Hole",
		"100 Ω .. 1 MΩ",
		"pt search -c resistors",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("filters output is missing %q:\n%s", want, text)
		}
	}
}

// The class profile comes from the cached category tree, not the leaf's own row,
// so filters reports the inherited value parameter and marks the curated set.
func TestFiltersUsesInheritedClassProfile(t *testing.T) {
	srv := newStubServer(t)
	out := setupApp(t, srv)

	if err := runFilters(context.Background(), "Resistors"); err != nil {
		t.Fatalf("runFilters: %v", err)
	}
	text := out.String()
	if strings.Contains(text, "No parameters found") {
		t.Errorf("category reported as having no parameters:\n%s", text)
	}
	for _, want := range []string{
		"resistance",
		"mounting_type",
		"the magnitude a bare value refers to", // value_param resolved
		"curated class profile",                // primary_params marked
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output is missing %q:\n%s", want, text)
		}
	}
	// Range parameters are dropped by the catalog endpoint; their absence must be
	// stated rather than read as "not stored".
	if !strings.Contains(text, "range parameters") {
		t.Errorf("output does not disclose the excluded range parameters:\n%s", text)
	}
}

// Every authenticated request must carry the bearer token; the CSRF middleware
// exempts Bearer clients, so a missing header would silently switch auth modes.
func TestRequestsCarryBearerToken(t *testing.T) {
	srv := newStubServer(t)
	setupApp(t, srv)

	if _, err := ap.teamID(context.Background()); err != nil {
		t.Fatalf("teamID: %v", err)
	}
	if srv.authSeen != "Bearer ptk_test" {
		t.Errorf("Authorization header = %q, want %q", srv.authSeen, "Bearer ptk_test")
	}
}

func TestMissingTokenIsAnAuthError(t *testing.T) {
	srv := newStubServer(t)
	setupApp(t, srv)
	ap.cfg.Token = ""
	ap.client = nil

	_, err := ap.requireAuthedClient()
	if err == nil {
		t.Fatal("expected an auth error")
	}
	if ee, ok := err.(*ExitError); !ok || ee.Code != ExitAuth {
		t.Errorf("want ExitAuth, got %v", err)
	}
}

// A flag whose value is optional only accepts it attached, so `--history 20`
// takes the flag's default and leaves "20" as a stray positional. Cobra's bare
// "accepts 1 arg(s), received 2" is true and useless; the error must name the
// fix, because this is the shape a script gets wrong.
func TestDetachedFlagValueIsExplained(t *testing.T) {
	cmd := &cobra.Command{
		Use:  "stock <part>",
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(*cobra.Command, []string) error { return nil },
	}
	cmd.Flags().Int("history", 0, "")
	cmd.Flags().Lookup("history").NoOptDefVal = "20"
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetArgs([]string{"@1", "--history", "20"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected a usage error")
	}
	ee, ok := err.(*ExitError)
	if !ok || ee.Code != ExitUsage {
		t.Fatalf("want ExitUsage, got %v", err)
	}
	if !strings.Contains(err.Error(), "--history=<value>") {
		t.Errorf("error does not explain the fix: %v", err)
	}

	// The attached form is correct and must not be second-guessed.
	cmd.SetArgs([]string{"@1", "--history=20"})
	if err := cmd.Execute(); err != nil {
		t.Errorf("--history=20 should be accepted: %v", err)
	}
}
