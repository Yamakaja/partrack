package handler

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Yamakaja/partrack/internal/blob"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Documents are file attachments (datasheets, office files, Touchstone S-params,
// …) pinned to a part or project and listed separately from note markdown. They
// reuse the blob/attachment layer: ownership is recorded on the attachment row
// (owner_kind/owner_id); a NULL owner is a note-embedded image, not a document.
//
// Office and Touchstone files have no distinctive magic bytes (zip / plain
// text), so documents are classified by file extension, not by content sniffing.
// That is safe because of how they are served: only PDFs are ever served inline
// (and then verified to actually be PDF); every other type is forced to a
// download (Content-Disposition: attachment), so no uploaded bytes can execute
// as script in our origin.

const defaultMaxDocumentBytes = 25 << 20 // 25 MiB

var defaultAllowedDocumentExtensions = []string{
	".pdf",
	".png", ".jpg", ".jpeg", ".gif", ".webp",
	".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
	".odt", ".ods", ".odp",
	".csv", ".tsv", ".txt", ".md",
	".zip",
	".s1p", ".s2p", ".s3p", ".s4p",
}

// documentContentTypes maps a lowercased extension to the content type used when
// listing and serving the document. Anything allowed but unmapped (e.g. the
// Touchstone variants) falls back to application/octet-stream, which downloads
// cleanly. PDFs and raster images are served inline (see inlineContentTypes);
// everything else downloads.
var documentContentTypes = map[string]string{
	".pdf":  "application/pdf",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".odt":  "application/vnd.oasis.opendocument.text",
	".ods":  "application/vnd.oasis.opendocument.spreadsheet",
	".odp":  "application/vnd.oasis.opendocument.presentation",
	".csv":  "text/csv",
	".tsv":  "text/tab-separated-values",
	".txt":  "text/plain",
	".md":   "text/markdown",
	".zip":  "application/zip",
}

// inlineContentTypes are the served content types safe to render in our own
// origin — the in-app viewer frames the PDF and embeds the images. SVG is
// deliberately absent (it can carry script); office/text/zip are absent so they
// only ever download. An upload whose served type is inline-eligible is checked
// to actually sniff as one of these, so disguised active content (HTML/SVG named
// .pdf/.png) can never be framed/embedded.
var inlineContentTypes = map[string]bool{
	"application/pdf": true,
	"image/png":       true,
	"image/jpeg":      true,
	"image/gif":       true,
	"image/webp":      true,
}

func (h *Handler) maxDocumentBytes() int64 {
	if n := h.Config.Storage.MaxDocumentBytes; n > 0 {
		return n
	}
	return defaultMaxDocumentBytes
}

func (h *Handler) allowedDocumentExtensions() []string {
	if e := h.Config.Storage.AllowedDocumentExtensions; len(e) > 0 {
		return e
	}
	return defaultAllowedDocumentExtensions
}

// documentContentType returns the served content type for a filename, or empty
// if the extension is not document-mappable.
func documentContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ct, ok := documentContentTypes[ext]; ok {
		return ct
	}
	return "application/octet-stream"
}

// documentResponse is the per-row shape for the document list. URLs are built
// client-side from teamId + id; `viewable` flags the in-browser PDF viewer.
type documentResponse struct {
	ID          string  `json:"id"`
	Filename    string  `json:"filename"`
	ContentType string  `json:"content_type"`
	ByteSize    int64   `json:"byte_size"`
	Viewable    bool    `json:"viewable"`
	UploadedBy  *string `json:"uploaded_by,omitempty"`
	UploadedAt  *string `json:"uploaded_at,omitempty"`
}

// requirePartAccessible enforces the cross-team IDOR guard for part-owned
// resources: only global parts or this team's own parts are addressable. It
// writes a 404 and returns false when the part is not accessible.
func (h *Handler) requirePartAccessible(w http.ResponseWriter, r *http.Request, partID, teamID pgtype.UUID) bool {
	part, err := h.Queries.GetPart(r.Context(), partID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
		return false
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return false
	}
	if part.Scope == dbgen.PartScopeTeam && (!part.TeamID.Valid || part.TeamID != teamID) {
		writeError(w, http.StatusNotFound, "part not found", "NOT_FOUND")
		return false
	}
	return true
}

// requireProjectOwned enforces that the project belongs to the team, writing a
// 404 and returning false otherwise.
func (h *Handler) requireProjectOwned(w http.ResponseWriter, r *http.Request, projectID, teamID pgtype.UUID) bool {
	_, err := h.Queries.ProjectExistsForTeam(r.Context(), dbgen.ProjectExistsForTeamParams{ID: projectID, TeamID: teamID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "project not found", "NOT_FOUND")
		return false
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return false
	}
	return true
}

// storeDocument validates, stores, and records a document upload for the given
// owner, then publishes the owner's change event and writes the JSON response.
func (h *Handler) storeDocument(w http.ResponseWriter, r *http.Request, teamID, ownerID pgtype.UUID, ownerKind string) {
	filename := r.URL.Query().Get("filename")
	if filename == "" {
		writeError(w, http.StatusBadRequest, "filename query parameter is required", "BAD_REQUEST")
		return
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if !slices.Contains(h.allowedDocumentExtensions(), ext) {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported file type: "+ext, "UNSUPPORTED_TYPE")
		return
	}

	max := h.maxDocumentBytes()
	data, err := io.ReadAll(io.LimitReader(r.Body, max+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body", "BAD_REQUEST")
		return
	}
	if int64(len(data)) > max {
		writeError(w, http.StatusRequestEntityTooLarge, "upload exceeds size limit", "TOO_LARGE")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "empty upload", "BAD_REQUEST")
		return
	}

	// Inline-served types (PDF, raster images) are verified to actually sniff as
	// an inline-safe type, so disguised active content (HTML/SVG named .pdf/.png)
	// can never be framed or embedded in our origin. Download-only types skip this.
	contentType := documentContentType(filename)
	if inlineContentTypes[contentType] && !inlineContentTypes[http.DetectContentType(data)] {
		writeError(w, http.StatusUnsupportedMediaType, "file content does not match its extension", "UNSUPPORTED_TYPE")
		return
	}

	ref, err := h.Blobs.Put(r.Context(), bytes.NewReader(data))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "STORAGE_ERROR")
		return
	}
	// blobs.content_type stays the sniffed (intrinsic) type; the document layer
	// presents the extension-derived type on its own.
	if err := h.Queries.UpsertBlob(r.Context(), dbgen.UpsertBlobParams{
		Sha256:      ref.SHA256,
		ByteSize:    ref.Size,
		ContentType: http.DetectContentType(data),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	var uploadedBy pgtype.UUID
	if u := middleware.UserFromContext(r.Context()); u != nil {
		_ = uploadedBy.Scan(u.ID)
	}

	kind := ownerKind
	att, err := h.Queries.CreateDocumentAttachment(r.Context(), dbgen.CreateDocumentAttachmentParams{
		TeamID:           teamID,
		BlobSha256:       ref.SHA256,
		OriginalFilename: &filename,
		UploadedBy:       uploadedBy,
		OwnerKind:        &kind,
		OwnerID:          ownerID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	h.publishDocumentChange(r, teamID, ownerKind, ownerID)

	writeJSON(w, http.StatusCreated, documentResponse{
		ID:          att.ID.String(),
		Filename:    filename,
		ContentType: contentType,
		ByteSize:    ref.Size,
		Viewable:    inlineContentTypes[contentType],
		UploadedBy:  uuidPtrString(att.UploadedBy),
		UploadedAt:  timestamptzString(att.UploadedAt),
	})
}

// publishDocumentChange notifies the team that an owner's document set changed,
// reusing the owner's existing entity-change event so the detail page refetches.
func (h *Handler) publishDocumentChange(r *http.Request, teamID pgtype.UUID, ownerKind string, ownerID pgtype.UUID) {
	switch ownerKind {
	case "part":
		h.publishEvent(r, "part_changed", teamID.String(), map[string]string{"part_id": ownerID.String()})
	case "project":
		h.publishEvent(r, "project_changed", teamID.String(), map[string]string{"project_id": ownerID.String()})
	}
}

// listDocuments writes the document list for an owner.
func (h *Handler) listDocuments(w http.ResponseWriter, r *http.Request, teamID, ownerID pgtype.UUID, ownerKind string) {
	kind := ownerKind
	rows, err := h.Queries.ListDocuments(r.Context(), dbgen.ListDocumentsParams{
		TeamID:    teamID,
		OwnerKind: &kind,
		OwnerID:   ownerID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}
	out := make([]documentResponse, 0, len(rows))
	for _, row := range rows {
		filename := derefStr(row.OriginalFilename)
		ct := documentContentType(filename)
		out = append(out, documentResponse{
			ID:          row.ID.String(),
			Filename:    filename,
			ContentType: ct,
			ByteSize:    row.ByteSize,
			Viewable:    inlineContentTypes[ct],
			UploadedBy:  row.UploadedByDisplayName,
			UploadedAt:  timestamptzString(row.UploadedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": out})
}

// UploadPartDocument handles POST /teams/{teamId}/parts/{partId}/documents (member+).
func (h *Handler) UploadPartDocument(w http.ResponseWriter, r *http.Request) {
	var teamID, partID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}
	if !h.requirePartAccessible(w, r, partID, teamID) {
		return
	}
	h.storeDocument(w, r, teamID, partID, "part")
}

// ListPartDocuments handles GET /teams/{teamId}/parts/{partId}/documents (viewer+).
func (h *Handler) ListPartDocuments(w http.ResponseWriter, r *http.Request) {
	var teamID, partID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := partID.Scan(chi.URLParam(r, "partId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part ID", "BAD_REQUEST")
		return
	}
	if !h.requirePartAccessible(w, r, partID, teamID) {
		return
	}
	h.listDocuments(w, r, teamID, partID, "part")
}

// UploadProjectDocument handles POST /teams/{teamId}/projects/{projectId}/documents (member+).
func (h *Handler) UploadProjectDocument(w http.ResponseWriter, r *http.Request) {
	var teamID, projectID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project ID", "BAD_REQUEST")
		return
	}
	if !h.requireProjectOwned(w, r, projectID, teamID) {
		return
	}
	h.storeDocument(w, r, teamID, projectID, "project")
}

// ListProjectDocuments handles GET /teams/{teamId}/projects/{projectId}/documents (viewer+).
func (h *Handler) ListProjectDocuments(w http.ResponseWriter, r *http.Request) {
	var teamID, projectID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := projectID.Scan(chi.URLParam(r, "projectId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project ID", "BAD_REQUEST")
		return
	}
	if !h.requireProjectOwned(w, r, projectID, teamID) {
		return
	}
	h.listDocuments(w, r, teamID, projectID, "project")
}

// ServeDocument handles GET /teams/{teamId}/documents/{docId}/content (viewer+).
//
// Unlike the capability-by-hash /blobs path, this is access-controlled (the
// document is resolved team-scoped) and filename-aware. PDFs are served inline
// so the in-browser viewer can frame them; every other type is forced to a
// download with the original filename, never inline.
func (h *Handler) ServeDocument(w http.ResponseWriter, r *http.Request) {
	var teamID, docID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := docID.Scan(chi.URLParam(r, "docId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid document ID", "BAD_REQUEST")
		return
	}

	doc, err := h.Queries.GetDocument(r.Context(), dbgen.GetDocumentParams{ID: docID, TeamID: teamID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "document not found", "NOT_FOUND")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	filename := derefStr(doc.OriginalFilename)
	contentType := documentContentType(filename)
	// Inline only for PDF/raster images, and only when not explicitly
	// downloading; every other type is forced to attachment so it can never
	// render in our origin.
	inline := inlineContentTypes[contentType] && r.URL.Query().Get("download") != "1"

	etag := `"` + doc.BlobSha256 + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	rc, size, err := h.Blobs.Open(r.Context(), doc.BlobSha256)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeError(w, http.StatusNotFound, "document not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error(), "STORAGE_ERROR")
		return
	}
	defer rc.Close()

	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Content-Disposition", contentDisposition(disposition, filename))
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Immutable bytes (content-addressed), but access-controlled — keep it private.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc)
}

// DeleteDocument handles DELETE /teams/{teamId}/documents/{docId} (member+).
// Removes the document attachment row; the shared blob is retained (no-GC).
func (h *Handler) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	var teamID, docID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}
	if err := docID.Scan(chi.URLParam(r, "docId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid document ID", "BAD_REQUEST")
		return
	}

	// Resolve first so we know the owner for the change event (and to 404 cleanly).
	doc, err := h.Queries.GetDocument(r.Context(), dbgen.GetDocumentParams{ID: docID, TeamID: teamID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "document not found", "NOT_FOUND")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	if _, err := h.Queries.DeleteDocument(r.Context(), dbgen.DeleteDocumentParams{ID: docID, TeamID: teamID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "document not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	h.publishDocumentChange(r, teamID, derefStr(doc.OwnerKind), doc.OwnerID)
	w.WriteHeader(http.StatusNoContent)
}

// contentDisposition builds a Content-Disposition header value with both an
// ASCII-sanitized filename and an RFC 5987 UTF-8 filename* for non-ASCII names.
func contentDisposition(disposition, filename string) string {
	if filename == "" {
		return disposition
	}
	// ASCII fallback: drop control chars, quotes, and backslashes; replace any
	// remaining non-ASCII so the bare filename= stays header-safe.
	var ascii strings.Builder
	for _, c := range filename {
		switch {
		case c < 0x20 || c == 0x7f || c == '"' || c == '\\':
			// skip control/quote/backslash
		case c > 0x7f:
			ascii.WriteByte('_')
		default:
			ascii.WriteRune(c)
		}
	}
	return disposition +
		`; filename="` + ascii.String() + `"` +
		`; filename*=UTF-8''` + url.PathEscape(filename)
}
