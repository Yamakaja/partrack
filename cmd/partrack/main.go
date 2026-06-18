package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Yamakaja/partrack/internal/blob"
	"github.com/Yamakaja/partrack/internal/config"
	"github.com/Yamakaja/partrack/internal/db"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/distributor/digikey"
	"github.com/Yamakaja/partrack/internal/distributor/lcsc"
	"github.com/Yamakaja/partrack/internal/distributor/mouser"
	"github.com/Yamakaja/partrack/internal/distributor/testclient"
	"github.com/Yamakaja/partrack/internal/server"
	"github.com/Yamakaja/partrack/internal/worker"
)

func main() {
	level := slog.LevelInfo
	if os.Getenv("PARTRACK_LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	cfg := config.Load()
	pool := db.Connect(cfg)
	defer pool.Close()

	db.Migrate(pool)
	db.RunStartupBackfills(context.Background(), pool)
	maybeBootstrap(pool)

	reg := distributor.NewRegistry()
	reg.Register("test", testclient.New())
	dk := digikey.New()
	if os.Getenv("PARTRACK_DIGIKEY_DEBUG") != "" {
		dk.SetDebug(true)
	}
	reg.Register("digikey", dk)
	reg.Register("mouser", mouser.New())
	reg.Register("lcsc", lcsc.New())

	q := worker.New(4, pool, reg)

	store, err := blob.NewFSStore(cfg.Storage.DataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "storage: %v\n", err)
		os.Exit(1)
	}

	srv := server.New(cfg, pool, q, reg, store)

	// Serve in the background so main can wait for a shutdown signal.
	serveErr := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stdout, "partrack listening on %s\n", cfg.Server.ListenAddr)
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	// Block until the listener fails or a termination signal arrives.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serveErr:
		if err != nil {
			fmt.Fprintf(os.Stderr, "server: %v\n", err)
			os.Exit(1)
		}
		return
	case <-ctx.Done():
		stop() // restore default signal handling so a second signal force-quits
	}

	// Graceful shutdown: stop accepting connections and drain in-flight requests,
	// then drain the worker queue. The DB pool (deferred) closes last so jobs and
	// requests still in flight can complete.
	slog.Info("shutdown signal received, draining")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http server shutdown", "err", err)
	}
	if err := q.Shutdown(shutdownCtx); err != nil {
		slog.Error("worker queue shutdown", "err", err)
	}
	slog.Info("shutdown complete")
}
