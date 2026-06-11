// Package server builds the HTTP server and wires up all routes.
package server

import (
	"context"
	"io/fs"
	"net/http"
	"time"

	"github.com/Yamakaja/partrack/internal/config"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/handler"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/timing"
	"github.com/Yamakaja/partrack/internal/worker"
	webfiles "github.com/Yamakaja/partrack/web"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New constructs an http.Server with all routes registered.
func New(cfg *config.Config, db *pgxpool.Pool, q *worker.Queue, reg distributor.Registry) *http.Server {
	cache := middleware.NewAuthCache()
	cache.StartFlusher(context.Background(), db, 5*time.Minute)

	h := handler.New(db, cfg, q, cache, reg)
	mw := middleware.New(db, cache, cfg.Server.TrustedProxies)
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(mw.RealIP) // trust X-Real-IP/X-Forwarded-For only from configured proxy CIDRs
	r.Use(maxBodyBytes)
	r.Use(mw.CSRF) // same-origin check for cookie-authenticated mutations
	r.Use(timing.Middleware)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)

	// API routes
	registerRoutes(r, h, mw)

	// Static file server with SPA fallback
	r.Mount("/", spaHandler())

	return &http.Server{
		Addr:    cfg.Server.ListenAddr,
		Handler: r,
		// ReadHeaderTimeout bounds the slow-header (slowloris) window; ReadTimeout
		// caps the whole request read (the body is already size-capped by
		// maxBodyBytes). IdleTimeout reaps idle keep-alive connections. WriteTimeout
		// is left unset on purpose: the SSE endpoints (/events, /scan/stream) hold
		// long-lived streaming responses that a write deadline would sever.
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// maxRequestBodyBytes caps request bodies to bound memory use and blunt
// body-flooding DoS. Generous enough for large BOM/order imports.
const maxRequestBodyBytes = 16 << 20 // 16 MiB

// maxBodyBytes wraps each request body in an http.MaxBytesReader so handlers that
// read it abort once the cap is exceeded.
func maxBodyBytes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
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
