// Package server builds the HTTP server and wires up all routes.
package server

import (
	"context"
	"io/fs"
	"net/http"
	"time"

	"github.com/Yamakaja/partrack/internal/config"
	"github.com/Yamakaja/partrack/internal/handler"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	webfiles "github.com/Yamakaja/partrack/web"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New constructs an http.Server with all routes registered.
func New(cfg *config.Config, db *pgxpool.Pool, q *worker.Queue) *http.Server {
	cache := middleware.NewAuthCache()
	cache.StartFlusher(context.Background(), db, 5*time.Minute)

	h := handler.New(db, cfg, q, cache)
	mw := middleware.New(db, cache)
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)

	// API routes
	registerRoutes(r, h, mw)

	// Static file server with SPA fallback
	r.Mount("/", spaHandler())

	return &http.Server{
		Addr:    cfg.Server.ListenAddr,
		Handler: r,
	}
}

// spaHandler serves the embedded frontend from web/dist.
// Unknown paths fall back to index.html to support client-side routing.
func spaHandler() http.Handler {
	sub, err := fs.Sub(webfiles.Static, "dist")
	if err != nil {
		// dist/ placeholder is committed; this should never fail.
		panic("server: failed to create dist sub-filesystem: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to open the requested path in the embedded FS.
		f, err := sub.Open(r.URL.Path)
		if err != nil {
			// Path not found — serve index.html for client-side routing.
			r2 := *r
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, &r2)
			return
		}
		f.Close()
		fileServer.ServeHTTP(w, r)
	})
}
