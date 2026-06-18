package db

import (
	"context"
	"os"
	"testing"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func strptr(s string) *string { return &s }

// canonOf returns parts.canonical_package for id (nil when NULL).
func canonOf(t *testing.T, pool *pgxpool.Pool, id pgtype.UUID) *string {
	t.Helper()
	var c *string
	if err := pool.QueryRow(context.Background(),
		`SELECT canonical_package FROM parts WHERE id = $1`, id).Scan(&c); err != nil {
		t.Fatalf("query canonical_package: %v", err)
	}
	return c
}

func TestBackfillCanonicalPackage(t *testing.T) {
	url := os.Getenv("PARTRACK_TEST_DB_URL")
	if url == "" {
		t.Skip("PARTRACK_TEST_DB_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	// Registered first so it runs last (cleanups are LIFO) — after the data cleanup
	// below, which needs the pool open.
	t.Cleanup(pool.Close)
	Migrate(pool)

	q := dbgen.New(pool)

	// app_backfills is not truncated between tests, so clear the marker to force a
	// run regardless of prior test runs in this DB.
	mustExec(t, pool, `DELETE FROM app_backfills WHERE name = $1`, backfillCanonicalPackageName)
	t.Cleanup(func() {
		mustExec(t, pool, `DELETE FROM parts WHERE mpn LIKE 'TEST-BACKFILL-%'`)
		mustExec(t, pool, `DELETE FROM app_backfills WHERE name = $1`, backfillCanonicalPackageName)
	})

	// Legacy state: a normalisable raw package, no canonical one.
	chip, err := q.CreatePart(ctx, dbgen.CreatePartParams{
		Scope:           dbgen.PartScopeGlobal,
		Mpn:             strptr("TEST-BACKFILL-CHIP"),
		Package:         strptr("0402 (1005 Metric)"),
		LifecycleStatus: dbgen.PartLifecycleStatusActive,
	})
	if err != nil {
		t.Fatalf("create chip part: %v", err)
	}
	// A package the normaliser can't classify (an array footprint has no single chip
	// size) — must be left NULL, not guessed.
	odd, err := q.CreatePart(ctx, dbgen.CreatePartParams{
		Scope:           dbgen.PartScopeGlobal,
		Mpn:             strptr("TEST-BACKFILL-ODD"),
		Package:         strptr("R_Array_Convex_4x0603"),
		LifecycleStatus: dbgen.PartLifecycleStatusActive,
	})
	if err != nil {
		t.Fatalf("create odd part: %v", err)
	}

	if err := backfillCanonicalPackage(ctx, pool); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	if c := canonOf(t, pool, chip.ID); c == nil || *c != "0402" {
		t.Errorf("chip canonical_package = %v, want \"0402\"", c)
	}
	if c := canonOf(t, pool, odd.ID); c != nil {
		t.Errorf("odd canonical_package = %q, want NULL", *c)
	}

	// The marker is now set, so a second run must be a no-op even for a freshly
	// inserted NULL part — proving the once-per-database guard.
	later, err := q.CreatePart(ctx, dbgen.CreatePartParams{
		Scope:           dbgen.PartScopeGlobal,
		Mpn:             strptr("TEST-BACKFILL-LATER"),
		Package:         strptr("0603 (1608 Metric)"),
		LifecycleStatus: dbgen.PartLifecycleStatusActive,
	})
	if err != nil {
		t.Fatalf("create later part: %v", err)
	}
	if err := backfillCanonicalPackage(ctx, pool); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if c := canonOf(t, pool, later.ID); c != nil {
		t.Errorf("later canonical_package = %q, want NULL (backfill must not re-run)", *c)
	}
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}
