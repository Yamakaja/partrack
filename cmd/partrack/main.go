package main

import (
	"fmt"
	"os"

	"github.com/Yamakaja/partrack/internal/config"
	"github.com/Yamakaja/partrack/internal/db"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/distributor/digikey"
	"github.com/Yamakaja/partrack/internal/distributor/testclient"
	"github.com/Yamakaja/partrack/internal/server"
	"github.com/Yamakaja/partrack/internal/worker"
)

func main() {
	cfg := config.Load()
	pool := db.Connect(cfg)
	defer pool.Close()

	db.Migrate(pool)
	maybeBootstrap(pool)

	reg := distributor.NewRegistry()
	reg.Register("test", testclient.New())
	dk := digikey.New()
	if os.Getenv("PARTRACK_DIGIKEY_DEBUG") != "" {
		dk.SetDebug(true)
	}
	reg.Register("digikey", dk)

	q := worker.New(4, pool, reg)

	srv := server.New(cfg, pool, q, reg)

	fmt.Fprintf(os.Stdout, "partrack listening on %s\n", cfg.Server.ListenAddr)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "server: %v\n", err)
		os.Exit(1)
	}
}
