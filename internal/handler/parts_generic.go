package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/genericpart"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/paramparse"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// genericParamInput is one {slug, value} parameter from a generic-create request.
type genericParamInput struct {
	Slug  string `json:"slug"`
	Value string `json:"value"`
}

// resolvedGenericParam is a request parameter resolved against the registry and
// SI-parsed, ready to persist and to form the dedup signature.
type resolvedGenericParam struct {
	slug       string
	value      string
	paramID    *int32
	valueType  string
	unitSymbol string
	numeric    *float64
	numericMax *float64
	enumValues []string
}

// CreateGenericPart handles POST /api/v1/parts/generic.
//
// It is the explicit, user-initiated creation of a generic (parametric) part —
// a plain team-scoped parts row with no MPN, a category, a canonical package, and
// a set of value parameters. It is create-or-get: if a generic part with the same
// category + canonical package + parameter signature already exists, it is
// returned instead of duplicating. Satisfaction never reaches this path
// automatically; only this endpoint creates generic parts.
func (h *Handler) CreateGenericPart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID     string              `json:"team_id"`
		CategoryID int32               `json:"category_id"`
		Package    string              `json:"package"`
		Params     []genericParamInput `json:"params"`
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

	canonical := genspec.NormalizePackage(req.Package)
	if canonical == "" {
		canonical = strings.TrimSpace(req.Package)
	}

	// Resolve + SI-parse each parameter against the registry.
	resolved, err := h.resolveGenericParams(r.Context(), req.Params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// create-or-get: compare the request signature against existing generics.
	wantSig := requestSignature(resolved)
	var pkgPtr *string
	if canonical != "" {
		pkgPtr = &canonical
	}
	candidates, err := h.Queries.ListGenericCandidatesWithParams(r.Context(), dbgen.ListGenericCandidatesWithParamsParams{
		TeamID:           teamID,
		CategoryID:       &req.CategoryID,
		CanonicalPackage: pkgPtr,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if existing := matchExistingGeneric(candidates, wantSig); existing.Valid {
		writeJSON(w, http.StatusOK, map[string]any{"part_id": existing.String(), "existing": true})
		return
	}

	// Synthesize a human MPN + description from the category's generic config so the
	// part is identifiable everywhere (lists, BOM, search). Categories without a
	// generic_code yield no identity and the part is created bare, as before.
	var mpnPtr, descPtr, mfrPtr *string
	if cat, err := h.Queries.GetCategory(r.Context(), req.CategoryID); err == nil {
		mpnPtr, descPtr, mfrPtr = synthesizeGenericIdentity(cat, canonical, resolved)
	}

	// Create the generic part, its parameters, and a zero-qty inventory entry.
	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	qtx := h.Queries.WithTx(tx)

	partID, err := h.insertGenericPart(r.Context(), qtx, teamID, userID, req.CategoryID, req.Package, pkgPtr, resolved, mpnPtr, descPtr, mfrPtr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishEvent(r, "part_changed", teamID.String(), map[string]string{"part_id": partID.String()})
	w.Header().Set("Location", "/api/v1/parts/"+partID.String())
	writeJSON(w, http.StatusCreated, map[string]any{"part_id": partID.String(), "existing": false})
}

// requestSignature builds the order-independent parameter signature of a
// create-generic request. Mapped params key on the parameter id; unmapped ones
// key on the raw value, so the same comparison works against existing parts.
func requestSignature(params []resolvedGenericParam) string {
	keys := make([]string, 0, len(params))
	for _, p := range params {
		keys = append(keys, paramSigKey(p.paramID, p.numeric, p.value))
	}
	sort.Strings(keys)
	return strings.Join(keys, "|")
}

// matchExistingGeneric returns the part whose parameter signature equals want, or
// an invalid UUID if none matches.
func matchExistingGeneric(rows []dbgen.ListGenericCandidatesWithParamsRow, want string) pgtype.UUID {
	return genericSignatureIndex(rows)[want]
}

// genericSignatureIndex groups candidate rows by part and indexes them by parameter
// signature, so a batch can match many requested signatures against one DB read. The
// first part wins on a signature collision (existing duplicates are already a no-op).
func genericSignatureIndex(rows []dbgen.ListGenericCandidatesWithParamsRow) map[string]pgtype.UUID {
	byPart := map[string][]string{}
	order := []pgtype.UUID{}
	seen := map[string]bool{}
	for _, row := range rows {
		id := row.PartID.String()
		if !seen[id] {
			seen[id] = true
			order = append(order, row.PartID)
		}
		// A part with no parameters yields one all-NULL row from the LEFT JOIN.
		if row.ParameterID == nil && row.Value == nil {
			continue
		}
		byPart[id] = append(byPart[id], paramSigKey(row.ParameterID, row.NumericValue, derefStr(row.Value)))
	}
	idx := make(map[string]pgtype.UUID, len(order))
	for _, partID := range order {
		keys := byPart[partID.String()]
		sort.Strings(keys)
		sig := strings.Join(keys, "|")
		if _, exists := idx[sig]; !exists {
			idx[sig] = partID
		}
	}
	return idx
}

func paramSigKey(paramID *int32, numeric *float64, value string) string {
	var prefix string
	if paramID != nil {
		prefix = "p" + strconv.Itoa(int(*paramID))
	} else {
		prefix = "v"
	}
	if numeric != nil {
		return prefix + ":" + strconv.FormatFloat(*numeric, 'g', -1, 64)
	}
	return prefix + ":" + strings.ToLower(strings.TrimSpace(value))
}

func strPtrOrNil(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// resolveGenericParams resolves each {slug, value} against the parameter registry,
// SI-parsing numerics (or splitting enum sets) so the result is ready both to persist
// and to form the dedup signature. Empty entries are skipped; unknown slugs are kept
// verbatim (matched on raw value). It errors only on an unexpected DB failure.
func (h *Handler) resolveGenericParams(ctx context.Context, params []genericParamInput) ([]resolvedGenericParam, error) {
	resolved := make([]resolvedGenericParam, 0, len(params))
	for _, p := range params {
		if p.Slug == "" || p.Value == "" {
			continue
		}
		rp := resolvedGenericParam{slug: p.Slug, value: p.Value}
		if def, err := h.Queries.GetParameterBySlug(ctx, p.Slug); err == nil {
			id := def.ID
			rp.paramID = &id
			rp.valueType = def.ValueType
			rp.unitSymbol = derefStr(def.UnitSymbol)
			if def.ValueType == "enum_set" {
				rp.enumValues = paramparse.ParseEnumSet(p.Value)
			} else {
				pr := paramparse.Parse(p.Value, def.ValueType)
				rp.numeric, rp.numericMax = pr.Value, pr.ValueMax
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		resolved = append(resolved, rp)
	}
	return resolved, nil
}

// insertGenericPart creates a generic parts row, its user parameters, and a zero-qty
// inventory entry, all on the supplied transaction. It returns the new part's ID.
func (h *Handler) insertGenericPart(ctx context.Context, qtx *dbgen.Queries, teamID, userID pgtype.UUID, categoryID int32, rawPackage string, pkgPtr *string, resolved []resolvedGenericParam, mpnPtr, descPtr, mfrPtr *string) (pgtype.UUID, error) {
	part, err := qtx.CreatePart(ctx, dbgen.CreatePartParams{
		Scope:            dbgen.PartScopeTeam,
		TeamID:           teamID,
		Mpn:              mpnPtr,
		Manufacturer:     mfrPtr,
		Description:      descPtr,
		CategoryID:       &categoryID,
		Package:          strPtrOrNil(rawPackage),
		CanonicalPackage: pkgPtr,
		LifecycleStatus:  dbgen.PartLifecycleStatusActive,
		CreatedBy:        userID,
	})
	if err != nil {
		return pgtype.UUID{}, err
	}
	for _, rp := range resolved {
		if _, err := qtx.UpsertUserParameter(ctx, dbgen.UpsertUserParameterParams{
			PartID:          part.ID,
			Name:            rp.slug,
			Value:           rp.value,
			ParameterID:     rp.paramID,
			NumericValue:    rp.numeric,
			NumericValueMax: rp.numericMax,
			EnumValues:      rp.enumValues,
		}); err != nil {
			return pgtype.UUID{}, err
		}
	}
	if _, err := qtx.CreateInventoryEntry(ctx, dbgen.CreateInventoryEntryParams{
		TeamID: teamID,
		PartID: part.ID,
	}); err != nil {
		return pgtype.UUID{}, err
	}
	return part.ID, nil
}

// synthesizeGenericIdentity synthesizes the MPN / description / manufacturer for a
// generic part from its category's generic config (generic_code + value_param +
// primary_params). Returns all-nil when the category has no generic_code (synthesis
// disabled) — the part is then created without a synthesized identity, as before.
func synthesizeGenericIdentity(cat dbgen.GetCategoryRow, canonicalPkg string, resolved []resolvedGenericParam) (mpn, description, manufacturer *string) {
	var order []string
	if len(cat.PrimaryParams) > 0 {
		_ = json.Unmarshal(cat.PrimaryParams, &order)
	}
	params := make([]genericpart.Param, len(resolved))
	for i, rp := range resolved {
		params[i] = genericpart.Param{
			Slug:       rp.slug,
			Numeric:    rp.numeric,
			UnitSymbol: rp.unitSymbol,
			Value:      rp.value,
		}
	}
	id, ok := genericpart.Synthesize(derefStr(cat.GenericCode), derefStr(cat.ValueParam), order, cat.DisplayName, canonicalPkg, params)
	if !ok {
		return nil, nil, nil
	}
	mfr := "Generic"
	return &id.MPN, &id.Description, &mfr
}
