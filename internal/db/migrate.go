package db

import (
	"errors"
	"fmt"
	"os"
	"strings"

	dbfiles "github.com/Yamakaja/partrack/db"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrate runs all pending up migrations against the database.
// Calls os.Exit(1) on failure.
func Migrate(pool *pgxpool.Pool) {
	src, err := iofs.New(dbfiles.Migrations, "migrations")
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate: failed to load migration sources: %v\n", err)
		os.Exit(1)
	}

	connStr := pool.Config().ConnString()
	m, err := migrate.NewWithSourceInstance("iofs", src, "pgx5://"+stripScheme(connStr))
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate: failed to create migrator: %v\n", err)
		os.Exit(1)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		fmt.Fprintf(os.Stderr, "migrate: failed to run migrations: %v\n", err)
		os.Exit(1)
	}
}

// stripScheme removes a leading "postgres://" or "postgresql://" so it can be
// re-prefixed with "pgx5://" as required by the golang-migrate pgx/v5 driver.
func stripScheme(connStr string) string {
	for _, prefix := range []string{"postgresql://", "postgres://"} {
		if strings.HasPrefix(connStr, prefix) {
			return connStr[len(prefix):]
		}
	}
	return connStr
}
