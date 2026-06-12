package handler_test

import (
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Yamakaja/partrack/internal/blob"
	"github.com/Yamakaja/partrack/internal/config"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/distributor/testclient"
	"github.com/Yamakaja/partrack/internal/server"
	"github.com/Yamakaja/partrack/internal/testutil"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testEnv holds the shared test server, database pool, and worker queue.
type testEnvType struct {
	Server *httptest.Server
	DB     *pgxpool.Pool
	Queue  *worker.Queue
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

	srv := server.New(cfg, testDB, q, reg, store)
	ts := httptest.NewServer(srv.Handler)

	testEnv = &testEnvType{Server: ts, DB: testDB, Queue: q}

	code := m.Run()
	ts.Close()
	os.RemoveAll(storeDir)
	os.Exit(code)
}
