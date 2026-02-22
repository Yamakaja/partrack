package db

import (
	"context"
	"fmt"
	"os"

	"github.com/Yamakaja/partrack/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect creates and verifies a pgxpool connection to PostgreSQL.
// Calls os.Exit(1) on failure.
func Connect(cfg *config.Config) *pgxpool.Pool {
	if cfg.Database.URL == "" {
		fmt.Fprintln(os.Stderr, "db: database URL is not configured (set database.url or PARTRACK_DB_URL)")
		os.Exit(1)
	}

	pool, err := pgxpool.New(context.Background(), cfg.Database.URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: failed to create pool: %v\n", err)
		os.Exit(1)
	}

	if err := pool.Ping(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "db: failed to connect to database: %v\n", err)
		os.Exit(1)
	}

	return pool
}
