package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/eseries"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/paramparse"
	"github.com/jackc/pgx/v5/pgtype"
)

// maxBatchGenericParts caps a single batch so an unbounded range can't fan out into
// thousands of inserts in one transaction.
const maxBatchGenericParts = 500

// batchGenericItem is one planned/created part in a batch response.
type batchGenericItem struct {
	MPN         string `json:"mpn"`
	Description string `json:"description"`
	Package     string `json:"package"`
	Value       string `json:"value"`
	Existing    bool   `json:"existing"`
	PartID      string `json:"part_id,omitempty"`
}

// BatchCreateGenericParts handles POST /api/v1/parts/generic/batch.
//
// It creates a set of generic parts in one go: a single category + package, a value
// set (an E-series across a range, or an explicit list), and optional fixed secondary
// parameters applied to every part. Each (value) becomes one create-or-get generic
// part, so re-running a batch never duplicates. With "dry_run": true it computes the
// plan — synthesized identities and which already exist — without writing, so the UI
// can preview before committing. Restricted to categories that have a generic_code.
func (h *Handler) BatchCreateGenericParts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID     string `json:"team_id"`
		CategoryID int32  `json:"category_id"`
		Package    string `json:"package"`
		Values     struct {
			Mode   string   `json:"mode"`   // "eseries" | "list"
			Series string   `json:"series"` // E6 … E192 (eseries mode)
			Lower  string   `json:"lower"`  // SI-aware bound (eseries mode)
			Upper  string   `json:"upper"`  // SI-aware bound (eseries mode)
			List   []string `json:"list"`   // explicit value strings (list mode)
		} `json:"values"`
		Params []genericParamInput `json:"params"`
		DryRun bool                `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TeamID == "" || req.CategoryID == 0 {
		writeError(w, http.StatusBadRequest, "team_id and category_id are required", "BAD_REQUEST")
		return
	}

	var teamID pgtype.UUID
	if err := teamID.Scan(req.TeamID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team_id", "BAD_REQUEST")
		return
	}
	user := middleware.UserFromContext(r.Context())
	var userID pgtype.UUID
	if err := userID.Scan(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if _, err := h.Queries.CheckTeamMembership(r.Context(), dbgen.CheckTeamMembershipParams{
		TeamID: teamID,
		UserID: userID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this team", "FORBIDDEN")
		return
	}

	// Category must support generic-part synthesis (has a generic_code) and be
	// parametric (has a value_param the value set populates).
	cat, err := h.Queries.GetCategory(r.Context(), req.CategoryID)
	if err != nil {
		writeError(w, http.StatusNotFound, "category not found", "NOT_FOUND")
		return
	}
	if derefStr(cat.GenericCode) == "" {
		writeError(w, http.StatusBadRequest, "category does not support generic parts", "BAD_REQUEST")
		return
	}
	valueParam := derefStr(cat.ValueParam)
	if valueParam == "" {
		writeError(w, http.StatusBadRequest, "category has no value parameter", "BAD_REQUEST")
		return
	}
	def, err := h.Queries.GetParameterBySlug(r.Context(), valueParam)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	unit := derefStr(def.UnitSymbol)

	// Expand the value-set spec into magnitude value strings (natural unit).
	magnitudes, msg := buildBatchMagnitudes(req.Values.Mode, req.Values.Series, req.Values.Lower, req.Values.Upper, req.Values.List, unit)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg, "BAD_REQUEST")
		return
	}
	if len(magnitudes) == 0 {
		writeError(w, http.StatusBadRequest, "no values to create", "BAD_REQUEST")
		return
	}
	if len(magnitudes) > maxBatchGenericParts {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("too many values (%d); max %d per batch", len(magnitudes), maxBatchGenericParts), "BAD_REQUEST")
		return
	}

	// Fixed secondary parameters, resolved once and shared by every part.
	secondary, err := h.resolveGenericParams(r.Context(), req.Params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	canonical := genspec.NormalizePackage(req.Package)
	if canonical == "" {
		canonical = strings.TrimSpace(req.Package)
	}
	var pkgPtr *string
	if canonical != "" {
		pkgPtr = &canonical
	}

	// One read of the existing generics for this (team, category, package); match every
	// requested signature against it in memory (and grow it as the batch creates).
	candidates, err := h.Queries.ListGenericCandidatesWithParams(r.Context(), dbgen.ListGenericCandidatesWithParamsParams{
		TeamID:           teamID,
		CategoryID:       &req.CategoryID,
		CanonicalPackage: pkgPtr,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	index := genericSignatureIndex(candidates)

	// build resolves one magnitude into its full param set, signature, and identity.
	defID := def.ID
	build := func(raw string) (resolved []resolvedGenericParam, sig string, mpn, desc, mfr *string) {
		pr := paramparse.Parse(raw, def.ValueType)
		resolved = make([]resolvedGenericParam, 0, 1+len(secondary))
		resolved = append(resolved, resolvedGenericParam{
			slug:       valueParam,
			value:      raw,
			paramID:    &defID,
			valueType:  def.ValueType,
			unitSymbol: unit,
			numeric:    pr.Value,
			numericMax: pr.ValueMax,
		})
		resolved = append(resolved, secondary...)
		sig = requestSignature(resolved)
		mpn, desc, mfr = synthesizeGenericIdentity(cat, canonical, resolved)
		return
	}

	items := make([]batchGenericItem, 0, len(magnitudes))
	existingCount := 0

	if req.DryRun {
		for _, raw := range magnitudes {
			_, sig, mpn, desc, _ := build(raw)
			it := batchGenericItem{MPN: derefStr(mpn), Description: derefStr(desc), Package: canonical, Value: raw}
			if pid, ok := index[sig]; ok && pid.Valid {
				it.Existing, it.PartID = true, pid.String()
				existingCount++
			}
			items = append(items, it)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"dry_run":   true,
			"total":     len(items),
			"to_create": len(items) - existingCount,
			"existing":  existingCount,
			"items":     items,
		})
		return
	}

	// Real creation: one transaction for the whole batch.
	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	qtx := h.Queries.WithTx(tx)

	created := 0
	for _, raw := range magnitudes {
		resolved, sig, mpn, desc, mfr := build(raw)
		it := batchGenericItem{MPN: derefStr(mpn), Description: derefStr(desc), Package: canonical, Value: raw}
		if pid, ok := index[sig]; ok && pid.Valid {
			it.Existing, it.PartID = true, pid.String()
			existingCount++
			items = append(items, it)
			continue
		}
		pid, err := h.insertGenericPart(r.Context(), qtx, teamID, userID, req.CategoryID, req.Package, pkgPtr, resolved, mpn, desc, mfr)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		index[sig] = pid // dedup identical magnitudes within this same batch
		it.PartID = pid.String()
		created++
		items = append(items, it)
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if created > 0 {
		h.publishEvent(r, "part_changed", teamID.String(), map[string]string{"batch": "generic"})
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"dry_run":  false,
		"total":    len(items),
		"created":  created,
		"existing": existingCount,
		"items":    items,
	})
}

// buildBatchMagnitudes turns a value-set spec into an ordered list of magnitude value
// strings in the parameter's natural unit. It returns a user-facing error message
// (non-empty) on invalid input.
func buildBatchMagnitudes(mode, series, lower, upper string, list []string, unit string) ([]string, string) {
	switch mode {
	case "eseries":
		s, ok := eseries.ByName(series)
		if !ok {
			return nil, "unknown E series: " + series
		}
		lo := paramparse.Parse(lower, "numeric").Value
		hi := paramparse.Parse(upper, "numeric").Value
		if lo == nil || hi == nil || *lo <= 0 || *hi <= 0 {
			return nil, "lower and upper bounds must be positive values"
		}
		if *hi < *lo {
			return nil, "upper bound must be ≥ lower bound"
		}
		vals := s.Expand(*lo, *hi)
		out := make([]string, len(vals))
		for i, v := range vals {
			out[i] = paramparse.FormatEngineering(v, unit)
		}
		return out, ""
	case "list":
		seen := map[string]bool{}
		var out []string
		for _, entry := range list {
			for _, tok := range strings.FieldsFunc(entry, func(r rune) bool { return r == ',' || r == '\n' || r == ';' }) {
				tok = strings.TrimSpace(tok)
				key := strings.ToLower(tok)
				if tok == "" || seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, tok)
			}
		}
		return out, ""
	default:
		return nil, `values.mode must be "eseries" or "list"`
	}
}
