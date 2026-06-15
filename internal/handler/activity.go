package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListActivity handles GET /api/v1/teams/{teamId}/activity-log.
// Default scope returns the caller's own activity. ?scope=team returns the
// team-wide feed (feed='team' rows), gated to purchaser+. Cursor-paginated.
func (h *Handler) ListActivity(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	user := middleware.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}
	var userID pgtype.UUID
	if err := userID.Scan(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Team-wide feed is gated to purchaser+. Instance admins bypass the team-role
	// check (consistent with requireTeamMembership in authz.go).
	scopeTeam := r.URL.Query().Get("scope") == "team"
	if scopeTeam && !user.IsAdmin {
		membership, err := h.checkTeamMembership(r.Context(), teamID, userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		if membership == nil || !roleAtLeast(membership.Role, "purchaser") {
			writeError(w, http.StatusForbidden, "purchaser role required for team activity", "FORBIDDEN")
			return
		}
	}

	var actions []string
	if raw := r.URL.Query().Get("action"); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			if t := strings.TrimSpace(s); t != "" {
				actions = append(actions, t)
			}
		}
	}

	limit := int32(50)
	if s := r.URL.Query().Get("limit"); s != "" {
		var v int32
		if _, err := fmt.Sscanf(s, "%d", &v); err == nil && v > 0 {
			limit = min(v, 100)
		}
	}

	var cursorTime pgtype.Timestamptz
	var cursorID pgtype.UUID
	if ct := r.URL.Query().Get("cursor_time"); ct != "" {
		t, err := time.Parse(time.RFC3339Nano, ct)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor_time", "BAD_REQUEST")
			return
		}
		cursorTime.Scan(t) //nolint:errcheck
		if ci := r.URL.Query().Get("cursor_id"); ci != "" {
			cursorID.Scan(ci) //nolint:errcheck
		}
	}

	rows, err := h.Queries.ListActivity(r.Context(), dbgen.ListActivityParams{
		TeamID:      teamID,
		ScopeTeam:   scopeTeam,
		ActorUserID: userID,
		Actions:     actions,
		CursorTime:  cursorTime,
		CursorID:    cursorID,
		Limit:       limit + 1, // fetch one extra to detect a next page
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type subjectOut struct {
		Type  string `json:"type"`
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	type activityItem struct {
		ID                      string          `json:"id"`
		Action                  string          `json:"action"`
		Feed                    string          `json:"feed"`
		Source                  string          `json:"source"`
		ActorLabel              *string         `json:"actor_label"`
		Subject                 *subjectOut     `json:"subject"`
		Summary                 *string         `json:"summary"`
		Payload                 json.RawMessage `json:"payload,omitempty"`
		CreatedAt               string          `json:"created_at"`
		PartManufacturer        *string         `json:"part_manufacturer"`
		PartImageURL            *string         `json:"part_image_url"`
		InventoryEntryID        *string         `json:"inventory_entry_id"`
		InventoryQuantityOnHand *int32          `json:"inventory_quantity_on_hand"`
	}

	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]activityItem, len(rows))
	for i, row := range rows {
		item := activityItem{
			ID:               row.ID.String(),
			Action:           row.Action,
			Feed:             row.Feed,
			Source:           row.Source,
			ActorLabel:       row.ActorLabel,
			Summary:          row.Summary,
			Payload:          row.Payload,
			CreatedAt:        row.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
			PartManufacturer: row.PartManufacturer,
			PartImageURL:     row.PartImageUrl,
		}
		if row.SubjectID.Valid && row.SubjectType != nil {
			item.Subject = &subjectOut{
				Type:  *row.SubjectType,
				ID:    row.SubjectID.String(),
				Label: derefStr(row.SubjectLabel),
			}
		}
		if row.InventoryEntryID.Valid {
			s := row.InventoryEntryID.String()
			item.InventoryEntryID = &s
			item.InventoryQuantityOnHand = row.InventoryQuantityOnHand
		}
		items[i] = item
	}

	var nextCursorTime, nextCursorID *string
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		t := last.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
		id := last.ID.String()
		nextCursorTime = &t
		nextCursorID = &id
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":            items,
		"next_cursor_time": nextCursorTime,
		"next_cursor_id":   nextCursorID,
	})
}
