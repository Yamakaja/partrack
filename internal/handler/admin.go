package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"
)

// adminUserResponse is the JSON shape returned by all admin user endpoints.
type adminUserResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	IsActive    bool   `json:"is_active"`
	IsAdmin     bool   `json:"is_admin"`
	HasPassword bool   `json:"has_password"`
	CreatedAt   string `json:"created_at"`
}

func toAdminUserResponse(id pgtype.UUID, displayName, email string, isActive, isAdmin bool, hasPassword interface{}, createdAt pgtype.Timestamptz) adminUserResponse {
	hp, _ := hasPassword.(bool)
	return adminUserResponse{
		ID:          id.String(),
		DisplayName: displayName,
		Email:       email,
		IsActive:    isActive,
		IsAdmin:     isAdmin,
		HasPassword: hp,
		CreatedAt:   createdAt.Time.UTC().Format(time.RFC3339),
	}
}

// ListUsers handles GET /api/v1/admin/users
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	limit := int32(50)
	offset := int32(0)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = int32(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = int32(n)
		}
	}

	total, err := h.Queries.CountAllUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	rows, err := h.Queries.ListAllUsers(r.Context(), dbgen.ListAllUsersParams{Limit: limit, Offset: offset})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	items := make([]adminUserResponse, len(rows))
	for i, row := range rows {
		items[i] = toAdminUserResponse(row.ID, row.DisplayName, row.Email, row.IsActive, row.IsAdmin, row.HasPassword, row.CreatedAt)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

// CreateUser handles POST /api/v1/admin/users
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if req.DisplayName == "" || req.Email == "" {
		writeError(w, http.StatusBadRequest, "display_name and email are required", "BAD_REQUEST")
		return
	}

	user, err := h.Queries.AdminCreateUser(r.Context(), dbgen.AdminCreateUserParams{
		DisplayName: req.DisplayName,
		Email:       req.Email,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "email already in use", "CONFLICT")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	hasPassword := false
	if req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		if err := h.Queries.AdminSetPassword(r.Context(), dbgen.AdminSetPasswordParams{
			UserID:       user.ID,
			PasswordHash: string(hash),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		hasPassword = true
	}

	resp := adminUserResponse{
		ID:          user.ID.String(),
		DisplayName: user.DisplayName,
		Email:       user.Email,
		IsActive:    user.IsActive,
		IsAdmin:     user.IsAdmin,
		HasPassword: hasPassword,
		CreatedAt:   user.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
	writeJSON(w, http.StatusCreated, resp)
}

// GetUser handles GET /api/v1/admin/users/{userId}
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	var userID pgtype.UUID
	if err := userID.Scan(chi.URLParam(r, "userId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id", "BAD_REQUEST")
		return
	}
	row, err := h.Queries.GetUserForAdmin(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found", "NOT_FOUND")
		return
	}
	writeJSON(w, http.StatusOK, toAdminUserResponse(row.ID, row.DisplayName, row.Email, row.IsActive, row.IsAdmin, row.HasPassword, row.CreatedAt))
}

// UpdateUser handles PATCH /api/v1/admin/users/{userId}
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	var userID pgtype.UUID
	if err := userID.Scan(chi.URLParam(r, "userId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id", "BAD_REQUEST")
		return
	}

	var req struct {
		DisplayName *string `json:"display_name"`
		Email       *string `json:"email"`
		IsActive    *bool   `json:"is_active"`
		IsAdmin     *bool   `json:"is_admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	row, err := h.Queries.AdminUpdateUser(r.Context(), dbgen.AdminUpdateUserParams{
		ID:          userID,
		DisplayName: req.DisplayName,
		Email:       req.Email,
		IsActive:    req.IsActive,
		IsAdmin:     req.IsAdmin,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "email already in use", "CONFLICT")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// If deactivating, immediately clear all sessions and API tokens.
	if req.IsActive != nil && !*req.IsActive {
		_ = h.Queries.DeleteSessionsByUserID(r.Context(), userID)
		_ = h.Queries.DeleteTokensByUserID(r.Context(), userID)
	}

	// Re-fetch has_password from the admin view.
	detail, err := h.Queries.GetUserForAdmin(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusOK, adminUserResponse{
			ID: row.ID.String(), DisplayName: row.DisplayName, Email: row.Email,
			IsActive: row.IsActive, IsAdmin: row.IsAdmin,
			CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339),
		})
		return
	}
	writeJSON(w, http.StatusOK, toAdminUserResponse(detail.ID, detail.DisplayName, detail.Email, detail.IsActive, detail.IsAdmin, detail.HasPassword, detail.CreatedAt))
}

// SetPassword handles POST /api/v1/admin/users/{userId}/set-password
func (h *Handler) SetPassword(w http.ResponseWriter, r *http.Request) {
	var userID pgtype.UUID
	if err := userID.Scan(chi.URLParam(r, "userId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id", "BAD_REQUEST")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required", "BAD_REQUEST")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if err := h.Queries.AdminSetPassword(r.Context(), dbgen.AdminSetPasswordParams{
		UserID:       userID,
		PasswordHash: string(hash),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GeneratePasswordResetLink handles POST /api/v1/admin/users/{userId}/password-reset-link
func (h *Handler) GeneratePasswordResetLink(w http.ResponseWriter, r *http.Request) {
	var userID pgtype.UUID
	if err := userID.Scan(chi.URLParam(r, "userId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id", "BAD_REQUEST")
		return
	}

	// Generate a cryptographically random 32-byte token.
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	rawToken := base64.RawURLEncoding.EncodeToString(rawBytes)
	tokenHash := hashToken(rawToken)

	expiresAt := time.Now().Add(24 * time.Hour)
	tok, err := h.Queries.CreatePasswordResetToken(r.Context(), dbgen.CreatePasswordResetTokenParams{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path":       "/reset-password?token=" + rawToken,
		"expires_at": tok.ExpiresAt.Time.UTC().Format(time.RFC3339),
	})
}
