package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/Yamakaja/partrack/internal/llm"
	"github.com/Yamakaja/partrack/internal/paramparse"
	"github.com/jackc/pgx/v5/pgtype"
)

// curatedCategory is a category an admin has marked parametric (value_param set),
// i.e. a target the LLM may classify a generic part into.
type curatedCategory struct {
	id          int32
	slug        string
	displayName string
	valueParam  string
	primary     []string // primary_params, value_param first
}

// classifyOut / extractOut are the JSON shapes the LLM is asked to return.
type classifyOut struct {
	Route      string  `json:"route"` // generic | specific | ignore | unknown
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
}

type extractOut struct {
	Params []struct {
		Slug  string `json:"slug"`
		Value string `json:"value"`
	} `json:"params"`
}

// escalateInterpretation runs the two-step LLM fallback for a BOM row the
// deterministic parser couldn't read cleanly. It returns the LLM-derived
// interpretation and ok=true, or (base, false) on any failure — the LLM never
// blocks interpretation. Caller must ensure h.LLM != nil.
//
// row is the full raw BOM row (original header → cell value), giving the model
// everything the deterministic parser discards. base is the deterministic
// reading, passed as a non-binding hint.
func (h *Handler) escalateInterpretation(ctx context.Context, teamID pgtype.UUID, row map[string]string, base genspec.Interpretation) (genspec.Interpretation, bool) {
	curated, err := h.curatedCategories(ctx)
	if err != nil || len(curated) == 0 {
		return base, false
	}

	// Step 1 — classify.
	var cls classifyOut
	if err := h.llmJSON(ctx, classifySystemPrompt(curated), classifyUserPrompt(row, base), &cls); err != nil {
		slog.Warn("llm classify failed", "err", err)
		return base, false
	}

	switch strings.ToLower(strings.TrimSpace(cls.Route)) {
	case "ignore":
		base.Route, base.Escalate = genspec.RouteIgnore, false
		return base, true
	case "specific":
		// Identity resolution uses the line's mapped MPN/SKU columns; we only fix
		// the route. (Injecting an LLM-discovered MPN is future work.)
		base.Route, base.Escalate = genspec.RouteIdentity, false
		return base, true
	case "generic":
		// handled below
	default:
		base.Route, base.Escalate = genspec.RouteUnknown, false
		return base, true
	}

	cat, ok := findCurated(curated, cls.Category)
	if !ok {
		return base, false
	}

	// Step 2 — extract parameters for the chosen category.
	defs := h.paramDefs(ctx, cat.primary)
	excerpts := h.categoryExcerpts(ctx, teamID, cat, defs)
	var ext extractOut
	if err := h.llmJSON(ctx, extractSystemPrompt(cat, defs, excerpts), extractUserPrompt(row, base), &ext); err != nil {
		slog.Warn("llm extract failed", "err", err, "category", cat.slug)
		return base, false
	}

	it := genspec.Interpretation{
		Route:      genspec.RouteParametric,
		Class:      base.Class,
		Category:   cat.slug,
		Package:    base.Package,
		Mounting:   base.Mounting,
		Confidence: clampConfidence(cls.Confidence),
		Flags:      []string{"llm"},
	}
	allowed := sliceSet(cat.primary)
	for _, p := range ext.Params {
		slug := strings.TrimSpace(p.Slug)
		val := strings.TrimSpace(p.Value)
		if slug == "" || val == "" {
			continue
		}
		if slug == "package" {
			if c := genspec.NormalizePackage(val); c != "" {
				it.Package = c
			} else if it.Package == "" {
				it.Package = val
			}
			continue
		}
		if !allowed[slug] {
			continue // hallucinated / out-of-profile slug
		}
		prm := genspec.Param{Slug: slug, Raw: val}
		if def, ok := defs[slug]; ok {
			if r := paramparse.Parse(val, def.ValueType); r.Value != nil {
				v := *r.Value
				prm.ValueSI = &v
			}
		}
		it.Params = append(it.Params, prm)
	}
	return it, true
}

// curatedCategories returns the parametric categories (value_param set).
func (h *Handler) curatedCategories(ctx context.Context) ([]curatedCategory, error) {
	rows, err := h.Queries.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	var out []curatedCategory
	for _, c := range rows {
		if c.ValueParam == nil || *c.ValueParam == "" {
			continue
		}
		var primary []string
		_ = json.Unmarshal(c.PrimaryParams, &primary)
		out = append(out, curatedCategory{
			id: c.ID, slug: c.Slug, displayName: c.DisplayName,
			valueParam: *c.ValueParam, primary: primary,
		})
	}
	return out, nil
}

// paramDefs resolves the registry definition for each slug (for value_type).
func (h *Handler) paramDefs(ctx context.Context, slugs []string) map[string]dbgen.ParameterDefinition {
	out := make(map[string]dbgen.ParameterDefinition, len(slugs))
	for _, s := range slugs {
		if s == "package" {
			continue
		}
		if def, err := h.Queries.GetParameterBySlug(ctx, s); err == nil {
			out[s] = def
		}
	}
	return out
}

// categoryExcerpts builds a compact, per-slug vocabulary hint from real catalog
// data: numeric ranges, distinct enum values, and the package codes actually in
// use. Best-effort — an empty catalog yields no hints (the model still works).
func (h *Handler) categoryExcerpts(ctx context.Context, teamID pgtype.UUID, cat curatedCategory, defs map[string]dbgen.ParameterDefinition) map[string]string {
	out := map[string]string{}
	if rows, err := h.Queries.GetCategoryParameters(ctx, dbgen.GetCategoryParametersParams{ID: cat.id, TeamID: teamID}); err == nil {
		for _, r := range rows {
			switch r.ValueType {
			case "numeric":
				lo, _ := r.MinNumeric.(float64)
				hi, _ := r.MaxNumeric.(float64)
				if lo != 0 || hi != 0 {
					out[r.Slug] = fmt.Sprintf("roughly %g..%g", lo, hi)
				}
			default:
				if vals := toStringSlice(r.EnumValues); len(vals) > 0 {
					out[r.Slug] = "e.g. " + strings.Join(capList(vals, 12), ", ")
				}
			}
		}
	}
	if pkgs, err := h.Queries.ListCategoryPackages(ctx, dbgen.ListCategoryPackagesParams{CategoryID: cat.id, TeamID: teamID}); err == nil {
		var list []string
		for _, p := range pkgs {
			if p != nil {
				list = append(list, *p)
			}
		}
		if len(list) > 0 {
			out["package"] = "e.g. " + strings.Join(capList(list, 16), ", ")
		}
	}
	return out
}

// llmJSON sends a system+user prompt, extracts the JSON object from the reply,
// and unmarshals it into v. One repair retry is attempted on a parse failure.
func (h *Handler) llmJSON(ctx context.Context, system, user string, v any) error {
	req := llm.Request{
		JSONObject: true,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		},
	}
	reply, err := h.LLM.Chat(ctx, req)
	if err != nil {
		return err
	}
	if obj, ok := llm.ExtractJSON(reply); ok {
		if json.Unmarshal([]byte(obj), v) == nil {
			return nil
		}
	}
	// Repair: ask once more for JSON only.
	req.Messages = append(req.Messages,
		llm.Message{Role: llm.RoleAssistant, Content: reply},
		llm.Message{Role: llm.RoleUser, Content: "Return only the JSON object, nothing else."},
	)
	reply, err = h.LLM.Chat(ctx, req)
	if err != nil {
		return err
	}
	obj, ok := llm.ExtractJSON(reply)
	if !ok {
		return fmt.Errorf("llm: no JSON object in reply")
	}
	return json.Unmarshal([]byte(obj), v)
}

// ── prompt construction ───────────────────────────────────────────────────────

func classifySystemPrompt(curated []curatedCategory) string {
	var b strings.Builder
	b.WriteString("You classify one electronic-BOM line item. Decide whether it is:\n")
	b.WriteString("- \"specific\": a particular part identified by manufacturer part number or distributor SKU;\n")
	b.WriteString("- \"generic\": a commodity defined by a category + parameters (e.g. a 10kΩ 0603 resistor);\n")
	b.WriteString("- \"ignore\": do-not-populate / virtual / mechanical;\n")
	b.WriteString("- \"unknown\": not enough information.\n")
	b.WriteString("If generic, pick exactly one category slug from this list:\n")
	for _, c := range curated {
		fmt.Fprintf(&b, "- %s — %s (value = %s)\n", c.slug, c.displayName, c.valueParam)
	}
	b.WriteString("Respond with ONLY a JSON object: {\"route\":\"generic|specific|ignore|unknown\",\"category\":\"<slug or empty>\",\"confidence\":0.0-1.0}\n")
	b.WriteString("Example: {\"route\":\"generic\",\"category\":\"resistors\",\"confidence\":0.9}")
	return b.String()
}

func classifyUserPrompt(row map[string]string, base genspec.Interpretation) string {
	var b strings.Builder
	b.WriteString("BOM line fields:\n")
	b.WriteString(renderRow(row))
	if base.Category != "" {
		fmt.Fprintf(&b, "Our tentative reading: category=%s.\n", base.Category)
	}
	if len(base.Residue) > 0 {
		fmt.Fprintf(&b, "Tokens we could not parse: %s\n", strings.Join(base.Residue, " "))
	}
	return b.String()
}

func extractSystemPrompt(cat curatedCategory, defs map[string]dbgen.ParameterDefinition, excerpts map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Extract parameters for a generic %s. The value field may use unusual or compact formatting (e.g. \"9k10%%\" = 9kΩ 10%%, \"5V/10uF\"); read it carefully.\n", cat.displayName)
	b.WriteString("Use ONLY these parameter slugs (omit any not present in the line):\n")
	for _, slug := range cat.primary {
		line := "- " + slug
		if def, ok := defs[slug]; ok && def.UnitSymbol != nil && *def.UnitSymbol != "" {
			line += " (unit " + *def.UnitSymbol + ")"
		}
		if ex, ok := excerpts[slug]; ok {
			line += " — " + ex
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("Respond with ONLY a JSON object: {\"params\":[{\"slug\":\"<slug>\",\"value\":\"<value as written>\"}]}\n")
	b.WriteString("Give each value with its unit (e.g. \"9k\", \"10%\", \"16V\", \"0603\"). Do not invent parameters.")
	return b.String()
}

func extractUserPrompt(row map[string]string, base genspec.Interpretation) string {
	var b strings.Builder
	b.WriteString("BOM line fields:\n")
	b.WriteString(renderRow(row))
	if len(base.Residue) > 0 {
		fmt.Fprintf(&b, "Tokens our parser could not place: %s\n", strings.Join(base.Residue, " "))
	}
	return b.String()
}

// ── helpers ───────────────────────────────────────────────────────────────────

func renderRow(row map[string]string) string {
	keys := make([]string, 0, len(row))
	for k, v := range row {
		if strings.TrimSpace(v) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s: %s\n", k, row[k])
	}
	return b.String()
}

func findCurated(curated []curatedCategory, slug string) (curatedCategory, bool) {
	slug = strings.TrimSpace(slug)
	for _, c := range curated {
		if c.slug == slug {
			return c, true
		}
	}
	return curatedCategory{}, false
}

func sliceSet(s []string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return m
}

func capList(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func clampConfidence(c float64) float64 {
	switch {
	case c <= 0:
		return 0.7
	case c > 1:
		return 1
	default:
		return c
	}
}
