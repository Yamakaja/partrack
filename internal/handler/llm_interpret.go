package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"sort"
	"strings"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/Yamakaja/partrack/internal/llm"
	"github.com/Yamakaja/partrack/internal/paramparse"
	"github.com/jackc/pgx/v5/pgtype"
)

// curatedCategory is a category the LLM may classify a generic part into: one with
// a primary value magnitude (value_param) and/or a set of primary parameters
// (primary_params). valueParam is "" for categories that have parameters but no
// single magnitude (e.g. headers: positions/rows/pitch).
type curatedCategory struct {
	id          int32
	slug        string
	displayName string
	valueParam  string
	primary     []string // primary_params, value_param first when present
}

// classifyOut / extractOut are the JSON shapes the LLM is asked to return.
type classifyOut struct {
	Route      string  `json:"route"` // generic | specific | ignore | unknown
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	// Specific-part identity (the model returns whichever it can read from the row).
	Manufacturer string `json:"manufacturer"`
	Mpn          string `json:"mpn"`
	Distributor  string `json:"distributor"`
	Sku          string `json:"sku"`
}

type extractOut struct {
	Params []struct {
		Slug  string `json:"slug"`
		Value string `json:"value"`
	} `json:"params"`
}

// aiProvenance is the ephemeral record of how the LLM handled one row — surfaced in
// the import review (the AI indicator) but never persisted. Reasoning is the trace
// when the provider returns one (token count comes from most providers).
type aiProvenance struct {
	Model           string `json:"model,omitempty"`
	Summary         string `json:"summary,omitempty"`
	Reasoning       string `json:"reasoning,omitempty"`
	ReasoningTokens int    `json:"reasoning_tokens,omitempty"`
}

// maxReasoningChars caps a persisted/serialised reasoning trace so a verbose model
// can't bloat the preview payload.
const maxReasoningChars = 4000

// add folds one LLM call's reasoning into the provenance (summing tokens, joining
// traces, capped).
func (p *aiProvenance) add(r llm.Response) {
	p.ReasoningTokens += r.ReasoningTokens
	if r.Reasoning == "" {
		return
	}
	if p.Reasoning != "" {
		p.Reasoning += "\n\n"
	}
	p.Reasoning += r.Reasoning
	if len(p.Reasoning) > maxReasoningChars {
		p.Reasoning = p.Reasoning[:maxReasoningChars] + "…"
	}
}

// escalateInterpretation runs the two-step LLM fallback for a BOM row the
// deterministic parser couldn't read cleanly. It returns the LLM-derived
// interpretation, an ephemeral provenance record (model + reasoning + a one-line
// decision summary), and ok=true; or (base, prov, false) on any failure — the LLM
// never blocks interpretation. Caller must ensure h.LLM != nil.
//
// row is the full raw BOM row (original header → cell value), giving the model
// everything the deterministic parser discards. base is the deterministic
// reading, passed as a non-binding hint. curated and dists are loaded once by the
// caller (the same lists for every row).
func (h *Handler) escalateInterpretation(ctx context.Context, teamID pgtype.UUID, row map[string]string, base genspec.Interpretation, curated []curatedCategory, dists []dbgen.Distributor) (genspec.Interpretation, aiProvenance, bool) {
	prov := aiProvenance{Model: h.Config.LLM.Model}
	if len(curated) == 0 {
		return base, prov, false
	}

	debug := h.Config.LLM.Debug

	// Step 1 — classify (and, for a specific part, read its identity).
	var cls classifyOut
	clsResp, err := h.llmJSON(ctx, classifySystemPrompt(curated, dists), classifyUserPrompt(row, base), &cls)
	prov.add(clsResp)
	if err != nil {
		slog.Warn("llm classify failed", "err", err)
		return base, prov, false
	}
	if debug {
		log.Printf("llm [interpret] classify parsed → route=%q category=%q mpn=%q manufacturer=%q distributor=%q sku=%q confidence=%.2f",
			cls.Route, cls.Category, cls.Mpn, cls.Manufacturer, cls.Distributor, cls.Sku, cls.Confidence)
	}

	switch strings.ToLower(strings.TrimSpace(cls.Route)) {
	case "ignore":
		base.Route, base.Escalate = genspec.RouteIgnore, false
		prov.Summary = "Read as do-not-populate / non-procurable."
		if debug {
			log.Printf("llm [interpret] result → ignore")
		}
		return base, prov, true
	case "specific":
		it := specificInterpretation(base, cls, row, dists)
		prov.Summary = specificSummary(it)
		if debug {
			log.Printf("llm [interpret] result → specific (after anti-hallucination): mpn=%q manufacturer=%q sku=%q distributor_id=%q%s",
				it.Mpn, it.Manufacturer, it.Sku, it.DistributorID, droppedIdentityNote(cls, it))
		}
		return it, prov, true
	case "generic":
		// handled below
	default:
		base.Route, base.Escalate = genspec.RouteUnknown, false
		prov.Summary = "Could not confidently classify this row — left for review."
		if debug {
			log.Printf("llm [interpret] result → unknown (unrecognized route %q)", cls.Route)
		}
		return base, prov, true
	}

	cat, ok := findCurated(curated, cls.Category)
	if !ok {
		if debug {
			log.Printf("llm [interpret] generic but category %q is not curated → keeping base reading (route=%s)", cls.Category, base.Route)
		}
		return base, prov, false
	}

	// Step 2 — extract parameters for the chosen category.
	defs := h.paramDefs(ctx, cat.primary)
	excerpts := h.categoryExcerpts(ctx, teamID, cat, defs)
	var ext extractOut
	extResp, err := h.llmJSON(ctx, extractSystemPrompt(cat, defs, excerpts), extractUserPrompt(row, base), &ext)
	prov.add(extResp)
	if err != nil {
		slog.Warn("llm extract failed", "err", err, "category", cat.slug)
		return base, prov, false
	}

	// Params is initialised non-nil so a package-only (or fully-dropped) extraction
	// serialises as [] rather than null — the frontend reads .params.length.
	it := genspec.Interpretation{
		Route:      genspec.RouteParametric,
		Class:      base.Class,
		Category:   cat.slug,
		Mounting:   base.Mounting,
		Confidence: clampConfidence(cls.Confidence),
		Flags:      []string{"llm"},
		Params:     []genspec.Param{},
	}
	// Carry the footprint-derived package over; the LLM may override it below.
	it.SetPackage(base.PackageValue())
	allowed := sliceSet(cat.primary)
	var dropped []string
	for _, p := range ext.Params {
		slug := strings.TrimSpace(p.Slug)
		val := strings.TrimSpace(p.Value)
		if slug == "" || val == "" {
			continue
		}
		if slug == genspec.PackageSlug {
			if c := genspec.NormalizePackage(val); c != "" {
				it.SetPackage(c)
			} else if it.PackageValue() == "" {
				it.SetPackage(val)
			}
			continue
		}
		if !allowed[slug] {
			dropped = append(dropped, slug)
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
	droppedNote := ""
	if len(dropped) > 0 {
		droppedNote = fmt.Sprintf(" (dropped out-of-profile: %s)", strings.Join(dropped, ", "))
	}
	prov.Summary = fmt.Sprintf("Read as a generic %s part: %s%s", cat.slug, formatParams(it.Params), droppedNote)
	if debug {
		log.Printf("llm [interpret] result → generic category=%s params=[%s]%s",
			it.Category, formatParams(it.Params), droppedNote)
	}
	return it, prov, true
}

// specificSummary describes an identity reading for the AI indicator.
func specificSummary(it genspec.Interpretation) string {
	switch {
	case it.Mpn != "" && it.Manufacturer != "":
		return fmt.Sprintf("Identified a specific part: %s (%s).", it.Mpn, it.Manufacturer)
	case it.Mpn != "":
		return fmt.Sprintf("Identified a specific part: MPN %s.", it.Mpn)
	case it.Sku != "":
		return fmt.Sprintf("Identified a specific part: SKU %s.", it.Sku)
	default:
		return "Read as a specific part, but no usable identifier survived validation."
	}
}

// formatParams renders an interpretation's params as "slug=raw" pairs for debug logs.
func formatParams(params []genspec.Param) string {
	parts := make([]string, 0, len(params))
	for _, p := range params {
		parts = append(parts, p.Slug+"="+p.Raw)
	}
	return strings.Join(parts, ", ")
}

// droppedIdentityNote reports, for debug logs, which identity fields the LLM
// returned but the anti-hallucination guard rejected (because they did not appear
// verbatim in the row, or named an unknown distributor).
func droppedIdentityNote(cls classifyOut, it genspec.Interpretation) string {
	var dropped []string
	if strings.TrimSpace(cls.Mpn) != "" && it.Mpn == "" {
		dropped = append(dropped, "mpn")
	}
	if strings.TrimSpace(cls.Sku) != "" && it.Sku == "" {
		dropped = append(dropped, "sku")
	}
	if len(dropped) == 0 {
		return ""
	}
	return " (rejected as not-in-row/unknown: " + strings.Join(dropped, ", ") + ")"
}

// curatedCategories returns the parametric categories (value_param set).
func (h *Handler) curatedCategories(ctx context.Context) ([]curatedCategory, error) {
	rows, err := h.Queries.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	var out []curatedCategory
	for _, c := range rows {
		var primary []string
		_ = json.Unmarshal(c.PrimaryParams, &primary)
		valueParam := ""
		if c.ValueParam != nil {
			valueParam = *c.ValueParam
		}
		// A category is a generic target if it has a primary value magnitude OR a set
		// of primary parameters the LLM can extract. The latter covers categories with
		// no single "value" — e.g. headers (positions/rows/pitch, read from the
		// footprint) — which would otherwise be invisible to the LLM and unclassifiable.
		if valueParam == "" && len(primary) == 0 {
			continue
		}
		out = append(out, curatedCategory{
			id: c.ID, slug: c.Slug, displayName: c.DisplayName,
			valueParam: valueParam, primary: primary,
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
// llmJSON sends a JSON-constrained prompt, unmarshals the reply into v, and returns
// the raw llm.Response (so callers can read reasoning provenance). On a non-JSON
// reply it retries once asking for JSON only.
func (h *Handler) llmJSON(ctx context.Context, system, user string, v any) (llm.Response, error) {
	req := llm.Request{
		JSONObject: true,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		},
	}
	reply, err := h.LLM.Chat(ctx, req)
	if err != nil {
		return llm.Response{}, err
	}
	if obj, ok := llm.ExtractJSON(reply.Content); ok {
		if json.Unmarshal([]byte(obj), v) == nil {
			return reply, nil
		}
	}
	// Repair: ask once more for JSON only.
	req.Messages = append(req.Messages,
		llm.Message{Role: llm.RoleAssistant, Content: reply.Content},
		llm.Message{Role: llm.RoleUser, Content: "Return only the JSON object, nothing else."},
	)
	reply, err = h.LLM.Chat(ctx, req)
	if err != nil {
		return llm.Response{}, err
	}
	obj, ok := llm.ExtractJSON(reply.Content)
	if !ok {
		return reply, fmt.Errorf("llm: no JSON object in reply")
	}
	return reply, json.Unmarshal([]byte(obj), v)
}

// specificInterpretation builds an identity Interpretation from the classify
// output. It keeps only identity values that literally appear in the row (anti-
// hallucination) and a distributor that maps to a known one. These fields ride on
// the Interpretation and are overlaid onto the line's columns at import.
func specificInterpretation(base genspec.Interpretation, cls classifyOut, row map[string]string, dists []dbgen.Distributor) genspec.Interpretation {
	base.Route = genspec.RouteIdentity
	base.Escalate = false
	base.Confidence = clampConfidence(cls.Confidence)
	base.Flags = []string{"llm"}
	base.Params = nil
	base.Residue = nil
	if mpn := strings.TrimSpace(cls.Mpn); mpn != "" && rowContains(row, mpn) {
		base.Mpn = mpn
		if mfr := strings.TrimSpace(cls.Manufacturer); mfr != "" && rowContains(row, mfr) {
			base.Manufacturer = mfr
		}
	}
	if sku := strings.TrimSpace(cls.Sku); sku != "" && rowContains(row, sku) {
		if id, ok := distributorIDByName(dists, cls.Distributor); ok {
			base.Sku = sku
			base.DistributorID = id
		}
	}
	return base
}

// rowContains reports whether val appears (case-insensitively) in any cell of the
// row — the anti-hallucination guard for LLM-returned identity fields.
func rowContains(row map[string]string, val string) bool {
	v := strings.ToLower(strings.TrimSpace(val))
	if v == "" {
		return false
	}
	for _, cell := range row {
		if strings.Contains(strings.ToLower(cell), v) {
			return true
		}
	}
	return false
}

// distributorIDByName maps an LLM-returned distributor name to a known
// distributor id (case-insensitive exact or substring match).
func distributorIDByName(dists []dbgen.Distributor, name string) (string, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return "", false
	}
	for _, d := range dists {
		dn := strings.ToLower(d.Name)
		if dn == n || strings.Contains(dn, n) || strings.Contains(n, dn) {
			return d.ID.String(), true
		}
	}
	return "", false
}

// ── prompt construction ───────────────────────────────────────────────────────

func classifySystemPrompt(curated []curatedCategory, dists []dbgen.Distributor) string {
	var b strings.Builder
	b.WriteString("You classify one electronic-BOM line item. Decide whether it is:\n")
	b.WriteString("- \"specific\": a particular part identified by a manufacturer part number (MPN and ideally manufacturer) or a distributor SKU (and ideally Distributor name);\n")
	b.WriteString("- \"generic\": a commodity defined by a category + parameters (e.g. a 10kΩ 0603 resistor, a 0.1in 2x4 pin header);\n")
	b.WriteString("- \"ignore\": do-not-populate / virtual / mechanical;\n")
	b.WriteString("- \"unknown\": not enough information.\n")
	b.WriteString("If generic, choose exactly one category from this list and return its slug — the quoted identifier; the name in parentheses is only a human-readable label, not what you return:\n")
	for _, c := range curated {
		fmt.Fprintf(&b, "- '%s' (%s)\n", c.slug, c.displayName)
	}
	b.WriteString("If specific, return the manufacturer + mpn AND/OR a distributor + sku — but ONLY values that literally appear in the line below; never invent a part number. Choose the distributor from:\n")
	if len(dists) == 0 {
		b.WriteString("- (none configured)\n")
	}
	for _, d := range dists {
		fmt.Fprintf(&b, "- %s\n", d.Name)
	}
	b.WriteString("Respond with ONLY a JSON object: {\"route\":\"generic|specific|ignore|unknown\",\"category\":\"<slug or empty>\",\"manufacturer\":\"\",\"mpn\":\"\",\"distributor\":\"\",\"sku\":\"\",\"confidence\":0.0-1.0}\n")
	b.WriteString("Examples: {\"route\":\"generic\",\"category\":\"resistors\",\"confidence\":0.9} | {\"route\":\"specific\",\"manufacturer\":\"Texas Instruments\",\"mpn\":\"LM358\",\"confidence\":0.95}")
	return b.String()
}

func classifyUserPrompt(row map[string]string, base genspec.Interpretation) string {
	var b strings.Builder
	b.WriteString("BOM line fields:\n")
	b.WriteString(renderRow(row))
	if len(base.Residue) > 0 {
		fmt.Fprintf(&b, "Tokens we could not parse: %s\n", strings.Join(base.Residue, " "))
	}
	return b.String()
}

func extractSystemPrompt(cat curatedCategory, defs map[string]dbgen.ParameterDefinition, excerpts map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Extract parameters for a generic part from category '%s'. Read them from the WHOLE line — the value field may be compact (e.g. \"9k10%%\" = 9kΩ 10%%, \"5V/10uF\"), and the footprint often encodes parameters too (e.g. a pin-header footprint gives positions, rows, and pitch).\n", cat.displayName)
	b.WriteString("Use ONLY these parameters, and return each by its slug — the leading identifier; the parenthesised text (name, unit) is just a label. Omit any not present in the line:\n")
	for _, slug := range cat.primary {
		line := "- " + slug
		if def, ok := defs[slug]; ok {
			meta := def.DisplayName
			if def.UnitSymbol != nil && *def.UnitSymbol != "" {
				meta += ", unit " + *def.UnitSymbol
			}
			if meta != "" {
				line += " (" + meta + ")"
			}
		}
		if ex, ok := excerpts[slug]; ok {
			line += " — " + ex
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("Respond with ONLY a JSON object: {\"params\":[{\"slug\":\"<slug>\",\"value\":\"<number-or-code>\"}]}\n")
	b.WriteString("Rules: if a parameter's value is not clearly available, DO NOT GUESS — omit it. Respect each parameter's type: an enum (e.g. a colour) must be one of its example values ('Green' is a colour; 'LED' is not). Give every value as a plain number in that parameter's stated unit, WITHOUT the unit and WITHOUT any SI prefix — e.g. resistance 9 kΩ → '9000', capacitance 10 µF → '1e-5', a 10% tolerance → '10' (a percentage is the number of percent), a package → its bare code like '0603'. Use the example ranges above as a guide for the expected scale. Do not invent parameters.")
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
