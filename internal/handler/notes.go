package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/Yamakaja/partrack/internal/apitypes"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// maxNoteBytes caps a note body. Notes are prose + a few embeds; this is
// generous for markdown while bounding memory and abuse.
const maxNoteBytes = 256 << 10 // 256 KiB

// A note body embeds two kinds of references that the manifest resolves:
//   - images as `attachment:<uuid>` (an `attachment:` URL scheme in markdown)
//   - object links as `:<kind>[label]{#<uuid>}` (remark text directives), where
//     kind is one of part/project/revision/order/build/storage.
//
// We extract the UUIDs with regexes rather than a full markdown parse — the
// resolver only needs the IDs, and the renderer re-parses the body anyway.
const uuidPat = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`

var (
	attachmentRefRe = regexp.MustCompile(`attachment:(` + uuidPat + `)`)
	// group 1 = kind, group 2 = uuid
	objRefRe = regexp.MustCompile(`:(part|project|revision|order|build|storage)\[[^\]]*\]\{#(` + uuidPat + `)`)
)

type noteManifestAttachment = apitypes.NoteManifestAttachment

// noteManifestObject is a normalized, render-ready reference to any linkable
// partrack object — the server computes the label, href, and (optional) status
// so the frontend chip stays kind-agnostic.
type noteManifestObject = apitypes.NoteManifestObject

type noteManifest = apitypes.NoteManifest

type noteResponse = apitypes.Note

// uniqueRefs returns the distinct (lowercased) capture-group-1 matches of re in
// body, preserving first-seen order.
func uniqueRefs(re *regexp.Regexp, body string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		id := strings.ToLower(m[1])
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// objRefsByKind returns the distinct object-link UUIDs in body, grouped by kind.
func objRefsByKind(body string) map[string][]string {
	byKind := map[string][]string{}
	seen := map[string]struct{}{}
	for _, m := range objRefRe.FindAllStringSubmatch(body, -1) {
		kind, id := m[1], strings.ToLower(m[2])
		key := kind + ":" + id
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		byKind[kind] = append(byKind[kind], id)
	}
	return byKind
}

func toPgUUIDs(ids []string) []pgtype.UUID {
	out := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		var u pgtype.UUID
		if u.Scan(id) == nil {
			out = append(out, u)
		}
	}
	return out
}

// resolveNoteManifest batch-resolves the attachment and object-link references
// found in body into a manifest the frontend uses to render images and chips
// without per-reference round-trips. All lookups are team-scoped.
func (h *Handler) resolveNoteManifest(ctx context.Context, teamID pgtype.UUID, body string) (noteManifest, error) {
	m := noteManifest{
		Attachments: map[string]noteManifestAttachment{},
		Objects:     map[string]noteManifestObject{},
	}

	if ids := uniqueRefs(attachmentRefRe, body); len(ids) > 0 {
		rows, err := h.Queries.GetAttachmentsByIDs(ctx, dbgen.GetAttachmentsByIDsParams{
			TeamID: teamID,
			Ids:    toPgUUIDs(ids),
		})
		if err != nil {
			return m, err
		}
		for _, row := range rows {
			url, err := h.Blobs.URL(ctx, row.BlobSha256)
			if err != nil {
				continue
			}
			m.Attachments[row.ID.String()] = noteManifestAttachment{
				URL:         url,
				ContentType: row.ContentType,
				Width:       row.Width,
				Height:      row.Height,
			}
		}
	}

	if err := h.resolveObjectRefs(ctx, teamID, body, m.Objects); err != nil {
		return m, err
	}

	return m, nil
}

// resolveObjectRefs fills objects with normalized entries for every resolvable
// object-link in body. Identity and routing come from the shared batch resolver
// (see object_refs.go), so a note chip names an order exactly as the activity
// feed and the object-refs endpoint do.
func (h *Handler) resolveObjectRefs(ctx context.Context, teamID pgtype.UUID, body string, objects map[string]noteManifestObject) error {
	summaries, err := h.resolveObjectSummaries(ctx, teamID, objRefsByKind(body))
	if err != nil {
		return err
	}
	for id, s := range summaries {
		// A revision whose project is unknown has no route; the chip renders as
		// unresolved rather than linking nowhere.
		if s.Href == "" {
			continue
		}
		objects[id] = noteManifestObject{
			Kind:   string(s.Kind),
			Label:  s.Label,
			Href:   s.Href,
			Status: strPtr(s.Status),
		}
	}
	return nil
}

// GetPartNote handles GET /api/v1/teams/{teamId}/parts/{partId}/note.
// Returns the team's note for the part (empty body if none yet) plus a resolved
// manifest of its embedded attachments and part references.
func (h *Handler) GetPartNote(w http.ResponseWriter, r *http.Request) {
	var teamID, partID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	var body string
	var updatedAt, updatedBy *string
	flag := string(dbgen.PartFlagNone)
	note, err := h.Queries.GetPartNote(r.Context(), dbgen.GetPartNoteParams{TeamID: teamID, PartID: partID})
	switch {
	case err == nil:
		body = note.Body
		flag = string(note.Flag)
		updatedAt = timestamptzString(note.UpdatedAt)
		updatedBy = uuidPtrString(note.UpdatedBy)
	case errors.Is(err, pgx.ErrNoRows):
		// No note yet — return an empty one so the editor opens blank.
	default:
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	manifest, err := h.resolveNoteManifest(r.Context(), teamID, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, noteResponse{Body: body, Manifest: manifest, Flag: &flag, UpdatedAt: updatedAt, UpdatedBy: updatedBy})
}

// PutPartNote handles PUT /api/v1/teams/{teamId}/parts/{partId}/note (member+).
// Upserts the team's note for the part. The part must be visible to the team
// (global or team-owned) so a note can't be attached to another team's part.
func (h *Handler) PutPartNote(w http.ResponseWriter, r *http.Request) {
	var teamID, partID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	// Guard part accessibility (cross-team IDOR): only global parts or this
	// team's own parts may be annotated. A 404 avoids leaking another team's
	// part existence.
	part, err := h.Queries.GetPart(r.Context(), partID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}
	if part.Scope == dbgen.PartScopeTeam && (!part.TeamID.Valid || part.TeamID != teamID) {
		writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
		return
	}

	var req struct {
		Body string `json:"body"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxNoteBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	var updatedBy pgtype.UUID
	if u := middleware.UserFromContext(r.Context()); u != nil {
		_ = updatedBy.Scan(u.ID)
	}

	note, err := h.Queries.UpsertPartNote(r.Context(), dbgen.UpsertPartNoteParams{
		TeamID:    teamID,
		PartID:    partID,
		Body:      req.Body,
		UpdatedBy: updatedBy,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	manifest, err := h.resolveNoteManifest(r.Context(), teamID, note.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	h.publishEvent(r, "part_changed", teamID.String(), map[string]string{"part_id": partID.String()})

	flag := string(note.Flag)
	writeJSON(w, http.StatusOK, noteResponse{
		Body:      note.Body,
		Manifest:  manifest,
		Flag:      &flag,
		UpdatedAt: timestamptzString(note.UpdatedAt),
		UpdatedBy: uuidPtrString(note.UpdatedBy),
	})
}

// PutPartFlag handles PUT /api/v1/teams/{teamId}/parts/{partId}/flag (member+).
// Sets the team's opinion flag on the part (none/preferred/approved/banned),
// independent of the note body. Same part-accessibility guard as the note.
func (h *Handler) PutPartFlag(w http.ResponseWriter, r *http.Request) {
	var teamID, partID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}

	var req struct {
		Flag string `json:"flag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	flag := dbgen.PartFlag(req.Flag)
	switch flag {
	case dbgen.PartFlagNone, dbgen.PartFlagPreferred, dbgen.PartFlagApproved, dbgen.PartFlagBanned:
	default:
		writeError(w, http.StatusBadRequest, "invalid flag", "BAD_REQUEST")
		return
	}

	part, err := h.Queries.GetPart(r.Context(), partID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}
	if part.Scope == dbgen.PartScopeTeam && (!part.TeamID.Valid || part.TeamID != teamID) {
		writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
		return
	}

	var updatedBy pgtype.UUID
	if u := middleware.UserFromContext(r.Context()); u != nil {
		_ = updatedBy.Scan(u.ID)
	}

	saved, err := h.Queries.SetPartFlag(r.Context(), dbgen.SetPartFlagParams{
		TeamID:    teamID,
		PartID:    partID,
		Flag:      flag,
		UpdatedBy: updatedBy,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	h.publishEvent(r, "part_changed", teamID.String(), map[string]string{"part_id": partID.String()})
	writeJSON(w, http.StatusOK, map[string]string{"flag": string(saved)})
}

// GetProjectNote handles GET /api/v1/teams/{teamId}/projects/{projectId}/note.
// The note lives on the project row (a project is owned by one team), so the
// query is team-scoped and a missing project is a 404.
func (h *Handler) GetProjectNote(w http.ResponseWriter, r *http.Request) {
	var teamID, projectID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project ID", "BAD_REQUEST")
		return
	}

	row, err := h.Queries.GetProjectNote(r.Context(), dbgen.GetProjectNoteParams{ID: projectID, TeamID: teamID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "project not found", "NOT_FOUND")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	manifest, err := h.resolveNoteManifest(r.Context(), teamID, row.Notes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, noteResponse{
		Body:      row.Notes,
		Manifest:  manifest,
		UpdatedAt: timestamptzString(row.NotesUpdatedAt),
		UpdatedBy: uuidPtrString(row.NotesUpdatedBy),
	})
}

// PutProjectNote handles PUT /api/v1/teams/{teamId}/projects/{projectId}/note (member+).
func (h *Handler) PutProjectNote(w http.ResponseWriter, r *http.Request) {
	var teamID, projectID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project ID", "BAD_REQUEST")
		return
	}

	var req struct {
		Body string `json:"body"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxNoteBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	var updatedBy pgtype.UUID
	if u := middleware.UserFromContext(r.Context()); u != nil {
		_ = updatedBy.Scan(u.ID)
	}

	row, err := h.Queries.UpdateProjectNote(r.Context(), dbgen.UpdateProjectNoteParams{
		ID:             projectID,
		TeamID:         teamID,
		Notes:          req.Body,
		NotesUpdatedBy: updatedBy,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "project not found", "NOT_FOUND")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	manifest, err := h.resolveNoteManifest(r.Context(), teamID, row.Notes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	h.publishEvent(r, "project_changed", teamID.String(), map[string]string{"project_id": projectID.String()})

	writeJSON(w, http.StatusOK, noteResponse{
		Body:      row.Notes,
		Manifest:  manifest,
		UpdatedAt: timestamptzString(row.NotesUpdatedAt),
		UpdatedBy: uuidPtrString(row.NotesUpdatedBy),
	})
}
