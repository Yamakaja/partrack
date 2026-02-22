// Package middleware provides Chi-compatible HTTP middleware for partrack.
package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// contextKey is an unexported type for context keys in this package.
type contextKey int

const (
	contextKeyUser contextKey = iota
	contextKeyTeamMembership
	contextKeySessionID // raw session ID for logout/last-seen updates
)

// User is the authenticated user injected into the request context.
type User struct {
	ID          string
	DisplayName string
	Email       string
	IsActive    bool
	// sessionID is set when authenticated via a session cookie/token.
	// Empty when authenticated via API token.
	sessionID string
	// apiTokenID is set when authenticated via API token.
	apiTokenID string
}

// TeamMembership holds the resolved membership record injected by RequireTeamRole.
type TeamMembership struct {
	TeamID string
	UserID string
	Role   string // "viewer" | "member" | "purchaser" | "admin"
}

// Middleware holds shared DB access and the auth cache for all authentication middleware.
type Middleware struct {
	q     *dbgen.Queries
	cache *AuthCache
}

// New creates a Middleware backed by the given pool and cache.
func New(db *pgxpool.Pool, cache *AuthCache) *Middleware {
	return &Middleware{q: dbgen.New(db), cache: cache}
}

// UserFromContext retrieves the authenticated User from a request context.
// Returns nil if authentication middleware has not run or the user is not set.
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(contextKeyUser).(*User)
	return u
}

// MembershipFromContext retrieves the TeamMembership from the request context.
func MembershipFromContext(ctx context.Context) *TeamMembership {
	m, _ := ctx.Value(contextKeyTeamMembership).(*TeamMembership)
	return m
}

// Authenticate validates the session cookie or Authorization: Bearer token and
// injects the resolved User into the context. Returns 401 on failure.
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			writeUnauthorized(w, "authentication required")
			return
		}

		user, err := m.lookupToken(r.Context(), token)
		if err != nil || user == nil {
			writeUnauthorized(w, "invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), contextKeyUser, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractToken reads the raw token from the session cookie or Bearer header.
func extractToken(r *http.Request) string {
	if cookie, err := r.Cookie("session"); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

// hashToken returns the SHA-256 hex digest of a raw token.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// lookupToken validates a raw token and returns the associated User.
// It checks the in-process cache first, falling back to the database on a miss.
// last_seen_at / last_used_at writes are deferred to the cache flusher.
func (m *Middleware) lookupToken(ctx context.Context, raw string) (*User, error) {
	hash := hashToken(raw)

	// Cache hit — session token.
	if entry, ok := m.cache.LookupSession(hash); ok {
		m.cache.TouchSession(hash)
		return entry.user, nil
	}

	// Cache hit — API token.
	if entry, ok := m.cache.LookupAPIToken(hash); ok {
		m.cache.TouchAPIToken(hash)
		return entry.user, nil
	}

	// Cache miss — try session token via DB.
	row, err := m.q.GetUserBySessionToken(ctx, hash)
	if err == nil {
		user := &User{
			ID:          row.ID.String(),
			DisplayName: row.DisplayName,
			Email:       row.Email,
			IsActive:    row.IsActive,
			sessionID:   row.SessionID.String(),
		}
		m.cache.StoreSession(hash, row.SessionID.String(), row.ID.String(), user, row.ExpiresAt.Time)
		m.cache.TouchSession(hash)
		return user, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// Cache miss — try API token via DB.
	apiRow, err := m.q.GetUserByAPIToken(ctx, hash)
	if err == nil {
		user := &User{
			ID:          apiRow.ID.String(),
			DisplayName: apiRow.DisplayName,
			Email:       apiRow.Email,
			IsActive:    apiRow.IsActive,
			apiTokenID:  apiRow.TokenID.String(),
		}
		// GetUserByAPITokenRow has no ExpiresAt — the SQL filters expired tokens
		// server-side. Pass nil so the cache entry lives until explicit revocation.
		m.cache.StoreAPIToken(hash, apiRow.TokenID.String(), apiRow.ID.String(), user, nil)
		m.cache.TouchAPIToken(hash)
		return user, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // not found is not an error
	}
	return nil, err
}

// SessionID returns the session ID if the user was authenticated via a session.
// Empty string if authenticated via API token.
func (u *User) SessionID() string {
	return u.sessionID
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + msg + `","code":"UNAUTHORIZED"}`)) //nolint:errcheck
}
