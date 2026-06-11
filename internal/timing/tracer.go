package timing

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type queryStartKey struct{}

// queryInfo carries per-query start time and the sqlc query name from
// TraceQueryStart (which has the SQL) to TraceQueryEnd (which does not).
type queryInfo struct {
	start time.Time
	name  string
}

// DBTracer implements pgx.QueryTracer to measure cumulative PostgreSQL query time.
// It is stateless and safe to use as a single instance across all pool connections.
type DBTracer struct{}

// TraceQueryStart records the query start time and its sqlc name in the context.
func (t *DBTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryStartKey{}, queryInfo{start: time.Now(), name: queryName(data.SQL)})
}

// TraceQueryEnd adds the elapsed query duration to the "db" span on the request
// Collector and records it per-query for the global Registry.
func (t *DBTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	info, ok := ctx.Value(queryStartKey{}).(queryInfo)
	if !ok {
		return
	}
	dur := time.Since(info.start)
	if c := FromContext(ctx); c != nil {
		c.Add("db", "PostgreSQL", dur)
		c.AddQuery(info.name, dur)
	}
}

// queryName extracts the sqlc query name from a SQL string. sqlc emits every
// query with a leading `-- name: GetOrder :one` comment, so the first line is a
// stable, bounded identifier. Returns "" for SQL without that prefix (e.g. raw
// pool.Exec or transaction control).
func queryName(sql string) string {
	line := sql
	if nl := strings.IndexByte(sql, '\n'); nl >= 0 {
		line = sql[:nl]
	}
	line = strings.TrimSpace(line)
	const prefix = "-- name:"
	if !strings.HasPrefix(line, prefix) {
		return ""
	}
	rest := strings.TrimSpace(line[len(prefix):])
	if sp := strings.IndexByte(rest, ' '); sp >= 0 {
		rest = rest[:sp] // drop the " :one" / " :many" / " :exec" suffix
	}
	return rest
}
