package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Yamakaja/partrack/internal/apitypes"
	"github.com/Yamakaja/partrack/internal/bomfile"
)

// forecastStub serves the two endpoints --resolve needs, recording the body it
// was sent so the request the CLI builds can be asserted rather than assumed.
type forecastStub struct {
	*httptest.Server
	lastRequest apitypes.BOMResolvePreviewRequest
}

func newForecastStub(t *testing.T, projects []apitypes.ProjectListItem, reply apitypes.BOMResolvePreview) *forecastStub {
	t.Helper()
	s := &forecastStub{}
	mux := http.NewServeMux()

	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Errorf("encoding stub response: %v", err)
		}
	}

	mux.HandleFunc("/api/v1/teams", func(w http.ResponseWriter, r *http.Request) {
		write(w, apitypes.List[apitypes.Team]{
			Total: 1, Items: []apitypes.Team{{ID: "team-uuid-1", Name: "Lab", Role: "member"}},
		})
	})
	mux.HandleFunc("/api/v1/teams/team-uuid-1/projects", func(w http.ResponseWriter, r *http.Request) {
		write(w, apitypes.List[apitypes.ProjectListItem]{Total: int64(len(projects)), Items: projects})
	})
	mux.HandleFunc("/api/v1/teams/team-uuid-1/bom/resolve-preview", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&s.lastRequest); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		write(w, reply)
	})

	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func forecastApp(t *testing.T, url string) *app {
	t.Helper()
	a := newTestApp(t)
	cfg, err := LoadConfig(Overrides{URL: url, ConfigDir: t.TempDir(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Token = "ptk_test"
	a.cfg = cfg
	return a
}

func itemsFor(t *testing.T, csv string) []apitypes.BOMImportItem {
	t.Helper()
	sheet, err := bomfile.Parse(strings.NewReader(csv), bomfile.ParseOptions{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return bomfile.BuildItems(sheet, bomfile.AutoMapHeaders(sheet.Headers), bomfile.BuildOptions{}).Items
}

// The forecast posts the same items an import would, unchanged. If the CLI
// reshaped them here, the preview would be answering a different question than
// the import will be asked.
func TestForecastPostsTheImportPayload(t *testing.T) {
	stub := newForecastStub(t, nil, apitypes.BOMResolvePreview{
		Items:   []apitypes.BOMResolvePreviewItem{{Index: 0, State: "auto_mpn"}},
		Summary: map[string]int{"auto_mpn": 1},
	})
	prev := ap
	t.Cleanup(func() { ap = prev })
	ap = forecastApp(t, stub.URL)

	items := itemsFor(t, "Ref,Qty,MPN,Value\nR1,2,RC0402FR-0710KL,10k\n")
	out, projectName, err := resolveForecast(context.Background(), items, "")
	if err != nil {
		t.Fatalf("resolveForecast: %v", err)
	}
	if projectName != "" {
		t.Errorf("no project was asked for, got %q", projectName)
	}
	if len(out.Items) != 1 || out.Items[0].State != "auto_mpn" {
		t.Errorf("response not decoded: %+v", out)
	}

	sent := stub.lastRequest
	if sent.ProjectID != "" {
		t.Errorf("project_id should be absent, got %q", sent.ProjectID)
	}
	if len(sent.Items) != 1 {
		t.Fatalf("sent %d items, want 1", len(sent.Items))
	}
	if sent.Items[0].ReferenceDesignators != "R1" || sent.Items[0].Quantity != 2 {
		t.Errorf("items were reshaped in transit: %+v", sent.Items[0])
	}
	if sent.Items[0].MPN == nil || *sent.Items[0].MPN != "RC0402FR-0710KL" {
		t.Errorf("mpn lost: %+v", sent.Items[0])
	}
}

func TestForecastResolvesProjectByName(t *testing.T) {
	projects := []apitypes.ProjectListItem{
		{ID: "p-1", Name: "Rev C Mainboard"},
		{ID: "p-2", Name: "Sensor Head"},
	}
	stub := newForecastStub(t, projects, apitypes.BOMResolvePreview{Summary: map[string]int{}})
	prev := ap
	t.Cleanup(func() { ap = prev })
	ap = forecastApp(t, stub.URL)

	items := itemsFor(t, "Ref,Qty\nR1,1\n")

	// Exact name, case-insensitively.
	if _, name, err := resolveForecast(context.Background(), items, "rev c mainboard"); err != nil {
		t.Fatalf("by name: %v", err)
	} else if name != "Rev C Mainboard" {
		t.Errorf("resolved to %q", name)
	}
	if stub.lastRequest.ProjectID != "p-1" {
		t.Errorf("project_id = %q, want p-1", stub.lastRequest.ProjectID)
	}

	// By id.
	if _, _, err := resolveForecast(context.Background(), items, "p-2"); err != nil {
		t.Fatalf("by id: %v", err)
	}
	if stub.lastRequest.ProjectID != "p-2" {
		t.Errorf("project_id = %q, want p-2", stub.lastRequest.ProjectID)
	}

	// Substring, unambiguous.
	if _, name, err := resolveForecast(context.Background(), items, "Sensor"); err != nil {
		t.Fatalf("by substring: %v", err)
	} else if name != "Sensor Head" {
		t.Errorf("resolved to %q", name)
	}

	// No match, and an ambiguous match, must not silently pick.
	if _, _, err := resolveForecast(context.Background(), items, "nothing-like-this"); err == nil {
		t.Error("expected a not-found error")
	} else if ee, ok := err.(*ExitError); !ok || ee.Code != ExitNotFound {
		t.Errorf("want ExitNotFound, got %v", err)
	}
}

func TestForecastAmbiguousProjectListsCandidates(t *testing.T) {
	stub := newForecastStub(t, []apitypes.ProjectListItem{
		{ID: "p-1", Name: "Mainboard Rev B"},
		{ID: "p-2", Name: "Mainboard Rev C"},
	}, apitypes.BOMResolvePreview{Summary: map[string]int{}})
	prev := ap
	t.Cleanup(func() { ap = prev })
	ap = forecastApp(t, stub.URL)

	_, _, err := resolveForecast(context.Background(), itemsFor(t, "Ref,Qty\nR1,1\n"), "Mainboard")
	if err == nil {
		t.Fatal("expected an ambiguity error")
	}
	ee, ok := err.(*ExitError)
	if !ok || ee.Code != ExitAmbiguous {
		t.Fatalf("want ExitAmbiguous, got %v", err)
	}
	for _, name := range []string{"Mainboard Rev B", "Mainboard Rev C"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("candidates not listed: %v", err)
		}
	}
}

// Every state the server can return must render as something. A state with no
// explanation would print a bare enum value at the user.
func TestStateMeaningCoversEveryState(t *testing.T) {
	for _, state := range []string{
		"auto_sku", "auto_mpn", "auto_mpn_fuzzy", "auto_prior_revision",
		"auto_generic", "ambiguous", "importing", "",
	} {
		if stateMeaning(state) == "" {
			t.Errorf("state %q has no explanation", state)
		}
	}
}

func TestPartLabelPrefersMPNAndShowsStock(t *testing.T) {
	n := int32(42)
	got := partLabel(apitypes.BOMResolveCandidate{
		PartID: "p-1", Mpn: sp("RC0402FR-0710KL"), QuantityOnHand: &n,
	})
	if !strings.Contains(got, "RC0402FR-0710KL") || !strings.Contains(got, "42 in stock") {
		t.Errorf("partLabel = %q", got)
	}

	// No MPN (a generic part): fall back to the description, then the id.
	if got := partLabel(apitypes.BOMResolveCandidate{PartID: "p-2", Description: sp("10k 0402")}); got != "10k 0402" {
		t.Errorf("partLabel = %q, want the description", got)
	}
	if got := partLabel(apitypes.BOMResolveCandidate{PartID: "p-3"}); got != "p-3" {
		t.Errorf("partLabel = %q, want the id", got)
	}
}
