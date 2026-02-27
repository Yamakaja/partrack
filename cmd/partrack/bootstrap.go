package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/pgtype"
)

// maybeBootstrap creates an initial admin user and prints a password-reset link
// to stdout if the database contains no users. It is a no-op otherwise.
func maybeBootstrap(pool *pgxpool.Pool) {
	ctx := context.Background()
	q := dbgen.New(pool)

	count, err := q.CountAllUsers(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: count users: %v\n", err)
		return
	}
	if count > 0 {
		return
	}

	// Create the bootstrap admin user.
	isAdmin := true
	user, err := q.AdminCreateUser(ctx, dbgen.AdminCreateUserParams{
		DisplayName: "Admin",
		Email:       "admin@localhost",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: create user: %v\n", err)
		return
	}
	if _, err := q.AdminUpdateUser(ctx, dbgen.AdminUpdateUserParams{
		ID:      user.ID,
		IsAdmin: &isAdmin,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: set is_admin: %v\n", err)
		return
	}

	// Generate a one-time password-reset token.
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: rand: %v\n", err)
		return
	}
	rawToken := base64.RawURLEncoding.EncodeToString(rawBytes)
	sum := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(sum[:])

	expiresAt := time.Now().Add(24 * time.Hour)
	if _, err := q.CreatePasswordResetToken(ctx, dbgen.CreatePasswordResetTokenParams{
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: create reset token: %v\n", err)
		return
	}

	resetPath := "/reset-password?token=" + rawToken
	border := strings.Repeat("─", 60)
	fmt.Fprintf(os.Stdout, "\n  %s\n", border)
	fmt.Fprintf(os.Stdout, "  Bootstrap admin created (no existing users found)\n")
	fmt.Fprintf(os.Stdout, "  Email:   admin@localhost\n")
	fmt.Fprintf(os.Stdout, "  Link:    %s\n", resetPath)
	fmt.Fprintf(os.Stdout, "  Expires: %s\n", expiresAt.Format(time.RFC1123))
	fmt.Fprintf(os.Stdout, "  %s\n\n", border)
}
