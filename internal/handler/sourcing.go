package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// sourcingPolicyDistributor is one distributor's rank within the team's sourcing policy.
type sourcingPolicyDistributor struct {
	DistributorID   string `json:"distributor_id"`
	DistributorName string `json:"distributor_name"`
	Enabled         bool   `json:"enabled"`
	Priority        *int32 `json:"priority"`
}

// sourcingPolicyResponse is the full policy view returned by GET/PUT.
type sourcingPolicyResponse struct {
	Strategy         string                      `json:"strategy"`
	CostTolerancePct *string                     `json:"cost_tolerance_pct"`
	CostToleranceAbs *string                     `json:"cost_tolerance_abs"`
	Distributors     []sourcingPolicyDistributor `json:"distributors"`
}

// buildSourcingPolicyResponse assembles the current policy for a team.
func (h *Handler) buildSourcingPolicyResponse(r *http.Request, teamID pgtype.UUID) (*sourcingPolicyResponse, error) {
	resp := &sourcingPolicyResponse{Strategy: "cost", Distributors: []sourcingPolicyDistributor{}}

	settings, err := h.Queries.GetTeamSourcingSettings(r.Context(), teamID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		resp.Strategy = settings.Strategy
		resp.CostTolerancePct = numericString(settings.CostTolerancePct)
		resp.CostToleranceAbs = numericString(settings.CostToleranceAbs)
	}

	configs, err := h.Queries.ListDistributorConfigs(r.Context(), teamID)
	if err != nil {
		return nil, err
	}
	for _, c := range configs {
		resp.Distributors = append(resp.Distributors, sourcingPolicyDistributor{
			DistributorID:   c.DistributorID.String(),
			DistributorName: c.DistributorName,
			Enabled:         c.Enabled,
			Priority:        c.Priority,
		})
	}
	return resp, nil
}

// GetSourcingPolicy handles GET /api/v1/teams/{teamId}/sourcing-policy
func (h *Handler) GetSourcingPolicy(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	resp, err := h.buildSourcingPolicyResponse(r, teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateSourcingPolicy handles PUT /api/v1/teams/{teamId}/sourcing-policy
// Replaces the team's sourcing strategy, cost guardrail, and full distributor ranking.
// Body: {strategy, cost_tolerance_pct?, cost_tolerance_abs?, distributor_priority: [distributorId,...]}
func (h *Handler) UpdateSourcingPolicy(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	var req struct {
		Strategy            string   `json:"strategy"`
		CostTolerancePct    *float64 `json:"cost_tolerance_pct"`
		CostToleranceAbs    *float64 `json:"cost_tolerance_abs"`
		DistributorPriority []string `json:"distributor_priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if req.Strategy != "cost" && req.Strategy != "preferred_in_stock" {
		writeError(w, http.StatusBadRequest, "strategy must be 'cost' or 'preferred_in_stock'", "BAD_REQUEST")
		return
	}
	if (req.CostTolerancePct != nil && *req.CostTolerancePct < 0) ||
		(req.CostToleranceAbs != nil && *req.CostToleranceAbs < 0) {
		writeError(w, http.StatusBadRequest, "cost tolerances must be non-negative", "BAD_REQUEST")
		return
	}

	pctNum, err := floatPtrToNumeric(req.CostTolerancePct)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cost_tolerance_pct", "BAD_REQUEST")
		return
	}
	absNum, err := floatPtrToNumeric(req.CostToleranceAbs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cost_tolerance_abs", "BAD_REQUEST")
		return
	}

	// Parse distributor IDs up front so a bad ID fails before any write.
	distIDs := make([]pgtype.UUID, len(req.DistributorPriority))
	for i, s := range req.DistributorPriority {
		if err := distIDs[i].Scan(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid distributor ID in ranking", "BAD_REQUEST")
			return
		}
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	q := h.Queries.WithTx(tx)

	if _, err := q.UpsertTeamSourcingSettings(r.Context(), dbgen.UpsertTeamSourcingSettingsParams{
		TeamID:           teamID,
		Strategy:         req.Strategy,
		CostTolerancePct: pctNum,
		CostToleranceAbs: absNum,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := q.ClearTeamDistributorPriorities(r.Context(), teamID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	for i, distID := range distIDs {
		rank := int32(i + 1)
		if err := q.SetDistributorPriority(r.Context(), dbgen.SetDistributorPriorityParams{
			Priority:      &rank,
			TeamID:        teamID,
			DistributorID: distID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Listing resolution depends on the policy; drop the cached resolution for this team.
	h.CartResolver.Invalidate(teamID.String())
	h.publishCartChanged(r, teamID)

	resp, err := h.buildSourcingPolicyResponse(r, teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// floatPtrToNumeric converts an optional float to a pgtype.Numeric (invalid when nil).
func floatPtrToNumeric(v *float64) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if v == nil {
		return n, nil
	}
	if err := n.Scan(strconv.FormatFloat(*v, 'f', -1, 64)); err != nil {
		return n, err
	}
	return n, nil
}
