package handler_test

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Yamakaja/partrack/internal/blob"
	"github.com/Yamakaja/partrack/internal/config"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/distributor/testclient"
	"github.com/Yamakaja/partrack/internal/handler"
	"github.com/Yamakaja/partrack/internal/server"
	"github.com/Yamakaja/partrack/internal/testutil"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testEnv holds the shared test server, database pool, worker queue, and the
// Handler (so tests can inject dependencies like a fake LLM client).
type testEnvType struct {
	Server  *httptest.Server
	DB      *pgxpool.Pool
	Queue   *worker.Queue
	Handler *handler.Handler
}

// testDB and testEnv are shared across all handler_test tests in this package.
// A single DB pool and HTTP server are created once in TestMain; individual tests
// call t.Cleanup(func() { testutil.Truncate(t, testDB) }) to reset state.
var (
	testDB  *pgxpool.Pool
	testEnv *testEnvType
)

func TestMain(m *testing.M) {
	testDB = testutil.MustDB(nil)

	cfg := &config.Config{
		Auth: config.AuthConfig{SessionSecret: "test-secret-for-testing-only-32b!"},
	}
	reg := distributor.NewRegistry()
	reg.Register("test", testclient.New())
	q := worker.New(1, testDB, reg)

	storeDir, err := os.MkdirTemp("", "partrack-blobs-*")
	if err != nil {
		panic("blob store temp dir: " + err.Error())
	}
	store, err := blob.NewFSStore(storeDir)
	if err != nil {
		panic("blob store: " + err.Error())
	}

	h, router := server.Build(cfg, testDB, q, reg, store)
	ts := httptest.NewServer(router)

	testEnv = &testEnvType{Server: ts, DB: testDB, Queue: q, Handler: h}

	// Quiesce the shared worker before every truncate, so a test's cleanup never
	// races an in-flight job transaction (deadlock) or lets a late job write into
	// the next test (state bleed).
	testutil.BeforeTruncate = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = q.WaitIdle(ctx)
	}

	code := m.Run()
	ts.Close()
	os.RemoveAll(storeDir)
	os.Exit(code)
}
