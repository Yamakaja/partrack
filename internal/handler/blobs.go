package handler

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"  // register GIF decoder for DecodeConfig
	_ "image/jpeg" // register JPEG decoder for DecodeConfig
	_ "image/png"  // register PNG decoder for DecodeConfig
	"io"
	"net/http"
	"slices"
	"strconv"

	"github.com/Yamakaja/partrack/internal/blob"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// defaultAllowedUploadTypes is the fallback content-type allowlist when none is
// configured (e.g. in tests that construct a bare Config). SVG is deliberately
// absent — it can carry script and is an XSS vector when served inline.
var defaultAllowedUploadTypes = []string{"image/png", "image/jpeg", "image/webp", "image/gif"}

const defaultMaxUploadBytes = 5 << 20 // 5 MiB

func (h *Handler) maxUploadBytes() int64 {
	if n := h.Config.Storage.MaxUploadBytes; n > 0 {
		return n
	}
	return defaultMaxUploadBytes
}

func (h *Handler) allowedUploadTypes() []string {
	if t := h.Config.Storage.AllowedTypes; len(t) > 0 {
		return t
	}
	return defaultAllowedUploadTypes
}

// attachmentResponse is returned on upload and is the shape the frontend editor
// uses to embed an image: it inserts `url` into the markdown body.
type attachmentResponse struct {
	ID          string  `json:"id"`
	URL         string  `json:"url"`
	ContentType string  `json:"content_type"`
	ByteSize    int64   `json:"byte_size"`
	Width       *int32  `json:"width,omitempty"`
	Height      *int32  `json:"height,omitempty"`
	Filename    *string `json:"filename,omitempty"`
}

// UploadAttachment handles POST /api/v1/teams/{teamId}/attachments.
// The request body is the raw image bytes; an optional ?filename= records the
// original name for provenance. The content type is sniffed from the bytes (the
// client's declared type is not trusted) and validated against the allowlist.
func (h *Handler) UploadAttachment(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	// Read the body under the per-upload cap. Reading cap+1 lets us distinguish
	// "exactly at the cap" from "over the cap". Images are small, so buffering in
	// memory is fine; a streaming Put can replace this for large-file backends.
	max := h.maxUploadBytes()
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

	// Sniff the real content type from the bytes; never trust the declared type.
	contentType := http.DetectContentType(data)
	if !slices.Contains(h.allowedUploadTypes(), contentType) {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported content type: "+contentType, "UNSUPPORTED_TYPE")
		return
	}

	// Best-effort image dimensions; left null for formats without a registered
	// decoder (e.g. WebP, which needs an extra dependency).
	var width, height *int32
	if cfg, _, derr := image.DecodeConfig(bytes.NewReader(data)); derr == nil {
		w32, h32 := int32(cfg.Width), int32(cfg.Height)
		width, height = &w32, &h32
	}

	ref, err := h.Blobs.Put(r.Context(), bytes.NewReader(data))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "STORAGE_ERROR")
		return
	}

	if err := h.Queries.UpsertBlob(r.Context(), dbgen.UpsertBlobParams{
		Sha256:      ref.SHA256,
		ByteSize:    ref.Size,
		ContentType: contentType,
		Width:       width,
		Height:      height,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	var filename *string
	if v := r.URL.Query().Get("filename"); v != "" {
		filename = &v
	}
	var uploadedBy pgtype.UUID
	if u := middleware.UserFromContext(r.Context()); u != nil {
		_ = uploadedBy.Scan(u.ID)
	}

	att, err := h.Queries.CreateAttachment(r.Context(), dbgen.CreateAttachmentParams{
		TeamID:           teamID,
		BlobSha256:       ref.SHA256,
		OriginalFilename: filename,
		UploadedBy:       uploadedBy,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DB_ERROR")
		return
	}

	url, err := h.Blobs.URL(r.Context(), ref.SHA256)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "STORAGE_ERROR")
		return
	}

	writeJSON(w, http.StatusCreated, attachmentResponse{
		ID:          att.ID.String(),
		URL:         url,
		ContentType: contentType,
		ByteSize:    ref.Size,
		Width:       width,
		Height:      height,
		Filename:    att.OriginalFilename,
	})
}

// ServeBlob handles GET /api/v1/blobs/{sha256}.
//
// Serving is capability-by-hash: the 256-bit content address is an unguessable
// bearer token, so the endpoint requires authentication but not team membership,
// and performs no DB lookup. Content is immutable under a given hash, so it is
// served with a long immutable cache and a strong ETag; the content type is
// sniffed from the leading bytes to keep this path DB-free.
func (h *Handler) ServeBlob(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "sha256")
	if !blob.ValidHash(hash) {
		writeError(w, http.StatusBadRequest, "invalid blob id", "BAD_REQUEST")
		return
	}

	etag := `"` + hash + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	rc, size, err := h.Blobs.Open(r.Context(), hash)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeError(w, http.StatusNotFound, "blob not found", "NOT_FOUND")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error(), "STORAGE_ERROR")
		return
	}
	defer rc.Close()

	// Sniff content type from the first chunk, then emit that chunk plus the rest.
	head := make([]byte, 512)
	n, err := io.ReadFull(rc, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusInternalServerError, err.Error(), "STORAGE_ERROR")
		return
	}
	head = head[:n]

	w.Header().Set("Content-Type", http.DetectContentType(head))
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := w.Write(head); err != nil {
		return
	}
	_, _ = io.Copy(w, rc)
}
