package timing

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type queryStartKey struct{}

// DBTracer implements pgx.QueryTracer to measure cumulative PostgreSQL query time.
// It is stateless and safe to use as a single instance across all pool connections.
type DBTracer struct{}

// TraceQueryStart records the query start time in the context.
func (t *DBTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryStartKey{}, time.Now())
}

// TraceQueryEnd adds the elapsed query duration to the "db" span on the request Collector.
func (t *DBTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	start, ok := ctx.Value(queryStartKey{}).(time.Time)
	if !ok {
		return
	}
	if c := FromContext(ctx); c != nil {
		c.Add("db", "PostgreSQL", time.Since(start))
	}
}
