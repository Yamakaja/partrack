package timing

import (
	"net/http"
)

// timingWriter wraps http.ResponseWriter to inject the Server-Timing header
// immediately before the status code is committed to the wire.
type timingWriter struct {
	http.ResponseWriter
	collector  *Collector
	totalSpan  *Span
	headerSent bool
}

func (tw *timingWriter) WriteHeader(code int) {
	if !tw.headerSent {
		tw.headerSent = true
		tw.totalSpan.Stop()
		if h := tw.collector.Header(); h != "" {
			tw.ResponseWriter.Header().Set("Server-Timing", h)
		}
	}
	tw.ResponseWriter.WriteHeader(code)
}

func (tw *timingWriter) Write(b []byte) (int, error) {
	if !tw.headerSent {
		tw.WriteHeader(http.StatusOK)
	}
	return tw.ResponseWriter.Write(b)
}

// Unwrap exposes the underlying ResponseWriter for http.ResponseController and
// chi middleware compatibility (e.g. chimw.Logger's WrapResponseWriter).
func (tw *timingWriter) Unwrap() http.ResponseWriter {
	return tw.ResponseWriter
}

// Flush delegates to the underlying http.Flusher if available.
func (tw *timingWriter) Flush() {
	if f, ok := tw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Middleware is a Chi-compatible middleware that adds Server-Timing headers to
// all non-SSE responses. It automatically measures:
//   - "total" — full handler time up to WriteHeader
//   - "db" — cumulative PostgreSQL query time (via DBTracer on the pgx pool)
//
// SSE connections (Accept: text/event-stream) are passed through without response
// wrapping; the Collector is still available in the context for custom spans.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := &Collector{}
		ctx := withCollector(r.Context(), c)
		r = r.WithContext(ctx)

		// SSE: don't buffer; Server-Timing can't be set on a streaming response.
		if r.Header.Get("Accept") == "text/event-stream" {
			next.ServeHTTP(w, r)
			return
		}

		total := c.Start("total", "")
		tw := &timingWriter{
			ResponseWriter: w,
			collector:      c,
			totalSpan:      total,
		}
		next.ServeHTTP(tw, r)

		// Handler returned without ever calling WriteHeader (e.g. empty 200 response).
		if !tw.headerSent {
			tw.WriteHeader(http.StatusOK)
		}
	})
}
