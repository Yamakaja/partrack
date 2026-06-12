package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

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
//   - object links as `:part[label]{#<uuid>}` (remark text directives)
//
// We extract the UUIDs with regexes rather than a full markdown parse — the
// resolver only needs the IDs, and the renderer re-parses the body anyway.
const uuidPat = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`

var (
	attachmentRefRe = regexp.MustCompile(`attachment:(` + uuidPat + `)`)
	partRefRe       = regexp.MustCompile(`:part\[[^\]]*\]\{#(` + uuidPat + `)`)
)

type noteManifestAttachment struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Width       *int32 `json:"width,omitempty"`
	Height      *int32 `json:"height,omitempty"`
}

type noteManifestPart struct {
	MPN             *string `json:"mpn"`
	Manufacturer    *string `json:"manufacturer"`
	LifecycleStatus string  `json:"lifecycle_status"`
}

type noteManifest struct {
	Attachments map[string]noteManifestAttachment `json:"attachments"`
	Parts       map[string]noteManifestPart       `json:"parts"`
}

type noteResponse struct {
	Body      string       `json:"body"`
	Manifest  noteManifest `json:"manifest"`
	UpdatedAt *string      `json:"updated_at,omitempty"`
	UpdatedBy *string      `json:"updated_by,omitempty"`
}

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

// resolveNoteManifest batch-resolves the attachment and part references found in
// body into a manifest the frontend uses to render images and chips without
// per-reference round-trips. Both lookups are team-scoped.
func (h *Handler) resolveNoteManifest(ctx context.Context, teamID pgtype.UUID, body string) (noteManifest, error) {
	m := noteManifest{
		Attachments: map[string]noteManifestAttachment{},
		Parts:       map[string]noteManifestPart{},
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

	if ids := uniqueRefs(partRefRe, body); len(ids) > 0 {
		rows, err := h.Queries.GetPartsForManifest(ctx, dbgen.GetPartsForManifestParams{
			Ids:    toPgUUIDs(ids),
			TeamID: teamID,
		})
		if err != nil {
			return m, err
		}
		for _, row := range rows {
			m.Parts[row.ID.String()] = noteManifestPart{
				MPN:             row.Mpn,
				Manufacturer:    row.Manufacturer,
				LifecycleStatus: string(row.LifecycleStatus),
			}
		}
	}

	return m, nil
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
	note, err := h.Queries.GetPartNote(r.Context(), dbgen.GetPartNoteParams{TeamID: teamID, PartID: partID})
	switch {
	case err == nil:
		body = note.Body
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

	writeJSON(w, http.StatusOK, noteResponse{Body: body, Manifest: manifest, UpdatedAt: updatedAt, UpdatedBy: updatedBy})
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

	writeJSON(w, http.StatusOK, noteResponse{
		Body:      note.Body,
		Manifest:  manifest,
		UpdatedAt: timestamptzString(note.UpdatedAt),
		UpdatedBy: uuidPtrString(note.UpdatedBy),
	})
}
