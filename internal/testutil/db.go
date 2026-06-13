// Package testutil provides shared helpers for integration tests.
// Nothing in this package is part of the production binary.
package testutil

import (
	"context"
	"fmt"
	"os"
	"testing"

	internaldb "github.com/Yamakaja/partrack/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MustDB opens a pool to PARTRACK_TEST_DB_URL, runs all migrations, truncates
// all application tables, and returns the pool. The test (or whole process)
// is skipped/exited when the env var is absent.
func MustDB(t *testing.T) *pgxpool.Pool {
	url := os.Getenv("PARTRACK_TEST_DB_URL")
	if url == "" {
		if t != nil {
			t.Skip("PARTRACK_TEST_DB_URL not set")
		}
		fmt.Fprintln(os.Stderr, "PARTRACK_TEST_DB_URL not set")
		os.Exit(1)
	}

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		if t != nil {
			t.Fatalf("testutil: pgxpool.New: %v", err)
		}
		fmt.Fprintf(os.Stderr, "testutil: pgxpool.New: %v\n", err)
		os.Exit(1)
	}

	if err := pool.Ping(context.Background()); err != nil {
		if t != nil {
			t.Fatalf("testutil: pool.Ping: %v", err)
		}
		fmt.Fprintf(os.Stderr, "testutil: pool.Ping: %v\n", err)
		os.Exit(1)
	}

	// Run migrations using the same runner as production (idempotent).
	internaldb.Migrate(pool)

	// Start with a clean slate.
	Truncate(t, pool)

	if t != nil {
		t.Cleanup(pool.Close)
	}
	return pool
}

// Truncate deletes all per-test rows from every application table. It does NOT
// touch `distributors` (seeded by migration 000002) or the golang-migrate
// tracking table.
func Truncate(t *testing.T, pool *pgxpool.Pool) {
	const q = `TRUNCATE
		users, sessions, api_tokens, user_credentials, user_preferences,
		external_identities, teams, team_memberships, distributor_configs,
		parts, part_parameters, distributor_listings, distributor_parameter_mappings,
		storage_locations, inventory_entries, stock_adjustments,
		projects, bom_revisions, bom_line_items, bom_line_item_substitutes,
		shopping_carts, cart_line_items,
		orders, order_line_items,
		builds,
		general_checkouts, general_checkout_line_items
		RESTART IDENTITY CASCADE`
	if _, err := pool.Exec(context.Background(), q); err != nil {
		if t != nil {
			t.Helper()
			t.Fatalf("testutil: truncate: %v", err)
		}
		fmt.Fprintf(os.Stderr, "testutil: truncate: %v\n", err)
		os.Exit(1)
	}
}
