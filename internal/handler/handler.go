// Package handler contains HTTP handler implementations for all partrack API
// endpoints. All handlers are methods on the Handler struct which holds
// shared dependencies.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Yamakaja/partrack/internal/config"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	DB          *pgxpool.Pool
	Queries     *dbgen.Queries
	Config      *config.Config
	Worker      *worker.Queue
	Cache       *middleware.AuthCache
	oidcEntries map[string]*oidcEntry // populated by initOIDC() in auth.go
}

// New creates a Handler and initialises OIDC providers.
func New(db *pgxpool.Pool, cfg *config.Config, q *worker.Queue, cache *middleware.AuthCache) *Handler {
	h := &Handler{
		DB:      db,
		Queries: dbgen.New(db),
		Config:  cfg,
		Worker:  q,
		Cache:   cache,
	}
	h.initOIDC()
	return h
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
		"error": message,
		"code":  code,
	})
}

// writeJSON encodes v as JSON and writes it with status 200.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
