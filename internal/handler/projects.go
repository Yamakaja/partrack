package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type projectListItem struct {
	ID                     string  `json:"id"`
	Name                   string  `json:"name"`
	Description            *string `json:"description"`
	Status                 string  `json:"status"`
	ActiveBomRevisionID    *string `json:"active_bom_revision_id"`
	ActiveBomRevisionLabel *string `json:"active_bom_revision_label"`
	CreatedByDisplayName   string  `json:"created_by_display_name"`
	CreatedAt              string  `json:"created_at"`
}

type projectDetail struct {
	ID                     string          `json:"id"`
	Name                   string          `json:"name"`
	Description            *string         `json:"description"`
	Status                 string          `json:"status"`
	ActiveBomRevisionID    *string         `json:"active_bom_revision_id"`
	ActiveBomRevisionLabel *string         `json:"active_bom_revision_label"`
	BomImportConfig        json.RawMessage `json:"bom_import_config"`
	CreatedByDisplayName   string          `json:"created_by_display_name"`
	CreatedAt              string          `json:"created_at"`
}

func uuidPtrString(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := u.String()
	return &s
}

// ListProjects handles GET /api/v1/teams/{teamId}/projects
// Query: status?, limit, offset
func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	var statusFilter dbgen.NullProjectStatus
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = dbgen.NullProjectStatus{
			ProjectStatus: dbgen.ProjectStatus(s),
			Valid:         true,
		}
	}

	limit, offset := parsePaginationParams(r)

	total, err := h.Queries.CountProjects(r.Context(), dbgen.CountProjectsParams{
		TeamID: teamID,
		Status: statusFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	rows, err := h.Queries.ListProjects(r.Context(), dbgen.ListProjectsParams{
		TeamID: teamID,
		Status: statusFilter,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	items := make([]projectListItem, len(rows))
	for i, p := range rows {
		items[i] = projectListItem{
			ID:                     p.ID.String(),
			Name:                   p.Name,
			Description:            p.Description,
			Status:                 string(p.Status),
			ActiveBomRevisionID:    uuidPtrString(p.ActiveBomRevisionID),
			ActiveBomRevisionLabel: p.ActiveBomRevisionLabel,
			CreatedByDisplayName:   p.CreatedByDisplayName,
			CreatedAt:              p.CreatedAt.Time.UTC().Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// CreateProject handles POST /api/v1/teams/{teamId}/projects
// Body: {name, description?}
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "BAD_REQUEST")
		return
	}

	p, err := h.Queries.CreateProject(r.Context(), dbgen.CreateProjectParams{
		TeamID:      teamID,
		Name:        body.Name,
		Description: body.Description,
		CreatedBy:   userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":                       p.ID.String(),
		"name":                     p.Name,
		"description":              p.Description,
		"status":                   string(p.Status),
		"active_bom_revision_id":   uuidPtrString(p.ActiveBomRevisionID),
		"bom_import_config":        json.RawMessage(p.BomImportConfig),
		"created_at":               p.CreatedAt.Time.UTC().Format(time.RFC3339),
	})
}

// GetProject handles GET /api/v1/teams/{teamId}/projects/{projectId}
func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var projectID pgtype.UUID
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id", "BAD_REQUEST")
		return
	}

	p, err := h.Queries.GetProject(r.Context(), dbgen.GetProjectParams{
		ID:     projectID,
		TeamID: teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, projectDetail{
		ID:                     p.ID.String(),
		Name:                   p.Name,
		Description:            p.Description,
		Status:                 string(p.Status),
		ActiveBomRevisionID:    uuidPtrString(p.ActiveBomRevisionID),
		ActiveBomRevisionLabel: p.ActiveBomRevisionLabel,
		BomImportConfig:        json.RawMessage(p.BomImportConfig),
		CreatedByDisplayName:   p.CreatedByDisplayName,
		CreatedAt:              p.CreatedAt.Time.UTC().Format(time.RFC3339),
	})
}

// UpdateProject handles PATCH /api/v1/teams/{teamId}/projects/{projectId}
// Patchable: name, description, status, active_bom_revision_id, bom_import_config
func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}
	var projectID pgtype.UUID
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project id", "BAD_REQUEST")
		return
	}

	// Decode into raw map to detect presence of bom_import_config key.
	var rawBody map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	// Parse typed fields from raw map.
	var name *string
	if v, ok := rawBody["name"]; ok {
		if err := json.Unmarshal(v, &name); err != nil {
			writeError(w, http.StatusBadRequest, "invalid name", "BAD_REQUEST")
			return
		}
	}
	var description *string
	if v, ok := rawBody["description"]; ok {
		if err := json.Unmarshal(v, &description); err != nil {
			writeError(w, http.StatusBadRequest, "invalid description", "BAD_REQUEST")
			return
		}
	}
	var statusFilter dbgen.NullProjectStatus
	if v, ok := rawBody["status"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid status", "BAD_REQUEST")
			return
		}
		statusFilter = dbgen.NullProjectStatus{
			ProjectStatus: dbgen.ProjectStatus(s),
			Valid:         true,
		}
	}
	var activeBomRevisionID pgtype.UUID
	if v, ok := rawBody["active_bom_revision_id"]; ok {
		var s *string
		if err := json.Unmarshal(v, &s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid active_bom_revision_id", "BAD_REQUEST")
			return
		}
		if s != nil {
			if err := activeBomRevisionID.Scan(*s); err != nil {
				writeError(w, http.StatusBadRequest, "invalid active_bom_revision_id", "BAD_REQUEST")
				return
			}
		}
	}

	// Handle three-state bom_import_config: absent=keep, null=clear, object=set.
	configRaw, hasConfig := rawBody["bom_import_config"]

	var (
		updatedID          pgtype.UUID
		updatedName        string
		updatedDescription *string
		updatedStatus      dbgen.ProjectStatus
		updatedBomRevID    pgtype.UUID
		updatedConfig      []byte
		updatedCreatedAt   pgtype.Timestamptz
	)

	if hasConfig {
		// Pass bom_import_config directly (nil for JSON null).
		var configBytes []byte
		if string(configRaw) != "null" {
			configBytes = []byte(configRaw)
		}
		row, err := h.Queries.UpdateProject(r.Context(), dbgen.UpdateProjectParams{
			Name:                name,
			Description:         description,
			Status:              statusFilter,
			ActiveBomRevisionID: activeBomRevisionID,
			BomImportConfig:     configBytes,
			ID:                  projectID,
			TeamID:              teamID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "project not found", "NOT_FOUND")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		updatedID = row.ID
		updatedName = row.Name
		updatedDescription = row.Description
		updatedStatus = row.Status
		updatedBomRevID = row.ActiveBomRevisionID
		updatedConfig = row.BomImportConfig
		updatedCreatedAt = row.CreatedAt
	} else {
		row, err := h.Queries.UpdateProjectKeepConfig(r.Context(), dbgen.UpdateProjectKeepConfigParams{
			Name:                name,
			Description:         description,
			Status:              statusFilter,
			ActiveBomRevisionID: activeBomRevisionID,
			ID:                  projectID,
			TeamID:              teamID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "project not found", "NOT_FOUND")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		updatedID = row.ID
		updatedName = row.Name
		updatedDescription = row.Description
		updatedStatus = row.Status
		updatedBomRevID = row.ActiveBomRevisionID
		updatedConfig = row.BomImportConfig
		updatedCreatedAt = row.CreatedAt
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":                     updatedID.String(),
		"name":                   updatedName,
		"description":            updatedDescription,
		"status":                 string(updatedStatus),
		"active_bom_revision_id": uuidPtrString(updatedBomRevID),
		"bom_import_config":      json.RawMessage(updatedConfig),
		"created_at":             updatedCreatedAt.Time.UTC().Format(time.RFC3339),
	})
}
