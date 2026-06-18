package handler_test

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Yamakaja/partrack/internal/llm"
	"github.com/Yamakaja/partrack/internal/testutil"
)

// fakeLLM returns canned classify/extract JSON, picking which by the system
// prompt, and counts calls so the test can prove caching avoids re-calling.
type fakeLLM struct {
	mu       sync.Mutex
	calls    int
	classify string
	extract  string
}

func (f *fakeLLM) Chat(_ context.Context, req llm.Request) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "Extract parameters") {
		return f.extract, nil
	}
	return f.classify, nil
}

func (f *fakeLLM) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestLLMEscalation exercises the LLM fallback end-to-end: a weird value the
// deterministic parser can't read ("9k10%") is escalated in the preview, the LLM
// classifies it generic + extracts params, the import persists it as 'llm' and
// resolves it auto_generic, and a second preview is served from interpretation
// memory without calling the LLM again.
func TestLLMEscalation(t *testing.T) {
	fake := &fakeLLM{
		classify: `{"route":"generic","category":"resistors","confidence":0.9}`,
		// SI-normalised values (as the extract prompt now asks): resistance in Ω,
		// tolerance as the percent number, scientific notation allowed.
		extract: `{"params":[{"slug":"resistance","value":"9000"},{"slug":"tolerance","value":"10"}]}`,
	}
	testEnv.Handler.LLM = fake
	t.Cleanup(func() {
		testEnv.Handler.LLM = nil
		testutil.Truncate(t, testDB)
	})

	userID := testutil.CreateUser(t, testDB, "llm@example.com", "pw", "LLM")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "LLM Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-LLM")
	token := testutil.CreateSession(t, testDB, userID)

	// Seed a matching generic resistor (resistors / 0603 / 9k) so the import resolves.
	if _, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/parts/generic", map[string]any{
		"team_id":     teamID,
		"category_id": resistorsCategoryID,
		"package":     "0603",
		"params":      []map[string]any{{"slug": "resistance", "value": "9k"}},
	}, token); status != http.StatusCreated {
		t.Fatalf("seed generic: got %d", status)
	}

	previewItem := map[string]any{
		"refdes": "R1", "footprint": "R_0603_1608Metric", "value": "9k10%",
		"row": map[string]string{"Reference": "R1", "Value": "9k10%", "Footprint": "0603"},
	}

	// 1. Preview escalates the weird row to the LLM.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/bom/interpret-preview", map[string]any{
		"team_id": teamID,
		"items":   []map[string]any{previewItem},
	}, token)
	if status != http.StatusOK {
		t.Fatalf("preview: got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	interp := items[0].(map[string]any)
	if interp["source"] != "llm" {
		t.Fatalf("expected source=llm, got %v", interp["source"])
	}
	if interp["route"] != "parametric" || interp["category"] != "resistors" {
		t.Errorf("expected parametric/resistors, got route=%v category=%v", interp["route"], interp["category"])
	}
	if !hasParam(interp, "resistance") {
		t.Errorf("expected a resistance param, got %v", interp["params"])
	}
	// The SI value from the LLM is carried through to value_si unchanged.
	if got := paramValueSI(interp, "resistance"); got != 9000 {
		t.Errorf("resistance value_si = %v, want 9000", got)
	}
	if got := paramValueSI(interp, "tolerance"); got != 10 {
		t.Errorf("tolerance value_si = %v, want 10", got)
	}
	if fake.callCount() != 2 {
		t.Errorf("expected 2 LLM calls (classify+extract), got %d", fake.callCount())
	}

	// 2. Import the line carrying the LLM interpretation as an 'llm' override.
	ibody, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions", teamID, projectID),
		map[string]any{
			"label": "v1.0", "set_active": true,
			"items": []any{map[string]any{
				"reference_designators": "R1", "quantity": 1,
				"value": "9k10%", "footprint": "R_0603_1608Metric",
				"interpretation": interp, "interpretation_source": "llm",
			}},
		}, token)
	if status != http.StatusAccepted {
		t.Fatalf("import: got %d: %v", status, ibody)
	}
	revID, _ := ibody["bom_revision_id"].(string)

	// The line is persisted as source 'llm' and resolves auto_generic.
	line := pollResolvedLine(t, teamID, projectID, token, revID)
	if line["interpretation_source"] != "llm" {
		t.Errorf("expected interpretation_source=llm, got %v", line["interpretation_source"])
	}
	if line["resolution_state"] != "auto_generic" {
		t.Errorf("expected resolution_state=auto_generic, got %v", line["resolution_state"])
	}

	// 3. The same weird value is now served from interpretation memory — no new
	//    LLM call (callCount stays at 2).
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/bom/interpret-preview", map[string]any{
		"team_id": teamID,
		"items":   []map[string]any{previewItem},
	}, token)
	if status != http.StatusOK {
		t.Fatalf("preview 2: got %d", status)
	}
	items, _ = body["items"].([]any)
	if items[0].(map[string]any)["source"] != "reused" {
		t.Errorf("expected source=reused on 2nd preview, got %v", items[0].(map[string]any)["source"])
	}
	if fake.callCount() != 2 {
		t.Errorf("expected no new LLM calls (cached), got %d total", fake.callCount())
	}
}

// TestLLMSpecificFallback exercises the specific-part path: a row with no mapped
// MPN that the LLM reads as a specific part. The discovered MPN (substring-
// validated against the row) is overlaid onto the line at import, resolving it
// auto_mpn.
func TestLLMSpecificFallback(t *testing.T) {
	fake := &fakeLLM{
		classify: `{"route":"specific","manufacturer":"Texas Instruments","mpn":"LM358","confidence":0.95}`,
	}
	testEnv.Handler.LLM = fake
	t.Cleanup(func() {
		testEnv.Handler.LLM = nil
		testutil.Truncate(t, testDB)
	})

	userID := testutil.CreateUser(t, testDB, "llm-spec@example.com", "pw", "LLM")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "LLM Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-LLM-SPEC")
	token := testutil.CreateSession(t, testDB, userID)

	// A catalog part with this MPN, so the overlaid MPN resolves auto_mpn.
	testutil.CreatePart(t, testDB, "team", "LM358", &teamID)

	// The MPN lives in an unmapped "Comment" column — no mapped MPN field.
	previewItem := map[string]any{
		"refdes": "U1", "value": "",
		"row": map[string]string{"Reference": "U1", "Comment": "LM358 dual op-amp"},
	}
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/bom/interpret-preview", map[string]any{
		"team_id": teamID,
		"items":   []map[string]any{previewItem},
	}, token)
	if status != http.StatusOK {
		t.Fatalf("preview: got %d: %v", status, body)
	}
	interp := body["items"].([]any)[0].(map[string]any)
	if interp["source"] != "llm" || interp["route"] != "identity" {
		t.Fatalf("expected llm/identity, got source=%v route=%v", interp["source"], interp["route"])
	}
	if interp["mpn"] != "LM358" {
		t.Errorf("expected discovered mpn=LM358, got %v", interp["mpn"])
	}

	// Import: the discovered MPN is overlaid onto the line and resolves auto_mpn.
	ibody, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions", teamID, projectID),
		map[string]any{
			"label": "v1.0", "set_active": true,
			"items": []any{map[string]any{
				"reference_designators": "U1", "quantity": 1,
				"interpretation": interp, "interpretation_source": "llm",
			}},
		}, token)
	if status != http.StatusAccepted {
		t.Fatalf("import: got %d: %v", status, ibody)
	}
	revID, _ := ibody["bom_revision_id"].(string)

	line := pollResolvedLine(t, teamID, projectID, token, revID)
	if line["resolution_state"] != "auto_mpn" {
		t.Errorf("expected resolution_state=auto_mpn, got %v", line["resolution_state"])
	}
	if line["mpn"] != "LM358" {
		t.Errorf("expected line mpn=LM358 (overlaid), got %v", line["mpn"])
	}
	if line["interpretation_source"] != "llm" {
		t.Errorf("expected interpretation_source=llm, got %v", line["interpretation_source"])
	}
}

func hasParam(interp map[string]any, slug string) bool {
	params, _ := interp["params"].([]any)
	for _, p := range params {
		if m, ok := p.(map[string]any); ok && m["slug"] == slug {
			return true
		}
	}
	return false
}

// paramValueSI returns the value_si of the named param, or NaN if absent/null.
func paramValueSI(interp map[string]any, slug string) float64 {
	params, _ := interp["params"].([]any)
	for _, p := range params {
		m, ok := p.(map[string]any)
		if !ok || m["slug"] != slug {
			continue
		}
		if v, ok := m["value_si"].(float64); ok {
			return v
		}
		return math.NaN()
	}
	return math.NaN()
}
