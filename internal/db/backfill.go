package db

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RunStartupBackfills executes one-time data backfills that require Go logic and
// therefore can't be plain SQL migrations. Each backfill is guarded by a marker
// row in app_backfills (migration 000057) so it runs exactly once per database.
//
// Failures are logged but non-fatal: the marker is only written after the
// backfill succeeds, so a failed run is simply retried on the next startup.
// Called from main after Migrate.
func RunStartupBackfills(ctx context.Context, pool *pgxpool.Pool) {
	if err := backfillCanonicalPackage(ctx, pool); err != nil {
		slog.Error("backfill failed", "backfill", backfillCanonicalPackageName, "err", err)
	}
	if err := backfillCanonicalPackageRenormalize(ctx, pool); err != nil {
		slog.Error("backfill failed", "backfill", backfillCanonicalPackageRenormalizeName, "err", err)
	}
}

// backfillCanonicalPackageName is the app_backfills marker for the
// canonical_package backfill. Versioned so a future re-run (e.g. after the
// normaliser improves) can use a fresh name.
const backfillCanonicalPackageName = "canonical_package_from_package_v1"

// backfillCanonicalPackage populates parts.canonical_package for rows that have a
// raw package but no canonical one — legacy parts created before enrichment began
// setting canonical_package. The canonical value is derived with the same
// genspec.NormalizePackage used on create/enrich, so the catalog matches what new
// imports produce. Parametric (generic) BOM matching keys on canonical_package, so
// without this those parts can never satisfy a generic line.
func backfillCanonicalPackage(ctx context.Context, pool *pgxpool.Pool) error {
	var done bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM app_backfills WHERE name = $1)`,
		backfillCanonicalPackageName,
	).Scan(&done); err != nil {
		return fmt.Errorf("check marker: %w", err)
	}
	if done {
		return nil
	}

	rows, err := pool.Query(ctx,
		`SELECT id::text, package FROM parts
		 WHERE canonical_package IS NULL AND package IS NOT NULL AND package <> ''`)
	if err != nil {
		return fmt.Errorf("query parts: %w", err)
	}
	type target struct {
		id, pkg string
	}
	targets, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (target, error) {
		var t target
		err := r.Scan(&t.id, &t.pkg)
		return t, err
	})
	if err != nil {
		return fmt.Errorf("collect parts: %w", err)
	}

	batch := &pgx.Batch{}
	for _, t := range targets {
		canon := genspec.NormalizePackage(t.pkg)
		if canon == "" {
			continue // package the normaliser can't classify — leave NULL
		}
		batch.Queue(`UPDATE parts SET canonical_package = $1 WHERE id = $2::uuid`, canon, t.id)
	}

	updated := batch.Len()
	if updated > 0 {
		if err := pool.SendBatch(ctx, batch).Close(); err != nil {
			return fmt.Errorf("apply updates: %w", err)
		}
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO app_backfills (name) VALUES ($1) ON CONFLICT DO NOTHING`,
		backfillCanonicalPackageName,
	); err != nil {
		return fmt.Errorf("write marker: %w", err)
	}

	slog.Info("backfill complete",
		"backfill", backfillCanonicalPackageName,
		"scanned", len(targets), "updated", updated)
	return nil
}

// backfillCanonicalPackageRenormalizeName is the app_backfills marker for the
// re-normalisation pass. Bump the version suffix whenever genspec.NormalizePackage
// gains a package form that should re-flow onto already-stored rows.
const backfillCanonicalPackageRenormalizeName = "canonical_package_renormalize_v2"

// backfillCanonicalPackageRenormalize re-derives parts.canonical_package from the
// raw package for every row whose stored canonical disagrees with what the current
// genspec.NormalizePackage produces. It self-heals rows whose canonical was set
// before the normaliser learned a package form — e.g. the distributor
// "<imperial> (<metric> Metric)" shape, stored verbatim as "1008 (2520 Metric)"
// instead of reducing to "1008", which kept inductor lines from matching. Rows the
// normaliser still can't classify (canon == "") are left untouched.
func backfillCanonicalPackageRenormalize(ctx context.Context, pool *pgxpool.Pool) error {
	var done bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM app_backfills WHERE name = $1)`,
		backfillCanonicalPackageRenormalizeName,
	).Scan(&done); err != nil {
		return fmt.Errorf("check marker: %w", err)
	}
	if done {
		return nil
	}

	rows, err := pool.Query(ctx,
		`SELECT id::text, package, canonical_package FROM parts
		 WHERE package IS NOT NULL AND package <> ''`)
	if err != nil {
		return fmt.Errorf("query parts: %w", err)
	}
	type target struct {
		id, pkg string
		canon   *string
	}
	targets, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (target, error) {
		var t target
		err := r.Scan(&t.id, &t.pkg, &t.canon)
		return t, err
	})
	if err != nil {
		return fmt.Errorf("collect parts: %w", err)
	}

	batch := &pgx.Batch{}
	for _, t := range targets {
		want := genspec.NormalizePackage(t.pkg)
		if want == "" {
			continue // normaliser can't classify it — don't clobber the stored value
		}
		cur := ""
		if t.canon != nil {
			cur = *t.canon
		}
		if want == cur {
			continue
		}
		batch.Queue(`UPDATE parts SET canonical_package = $1 WHERE id = $2::uuid`, want, t.id)
	}

	updated := batch.Len()
	if updated > 0 {
		if err := pool.SendBatch(ctx, batch).Close(); err != nil {
			return fmt.Errorf("apply updates: %w", err)
		}
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO app_backfills (name) VALUES ($1) ON CONFLICT DO NOTHING`,
		backfillCanonicalPackageRenormalizeName,
	); err != nil {
		return fmt.Errorf("write marker: %w", err)
	}

	slog.Info("backfill complete",
		"backfill", backfillCanonicalPackageRenormalizeName,
		"scanned", len(targets), "updated", updated)
	return nil
}
