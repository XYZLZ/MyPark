package middleware

import (
	"cmp"
	"log/slog"
	"net/http"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
)

// routeUnmatched is logged as the route when no mux pattern matched.
const routeUnmatched = "unmatched"

// AccessLog logs one line per request with request_id, method, route,
// status and duration_ms. route is the matched mux pattern (r.Pattern), so
// it never contains raw path values. It must wrap the mux directly (no
// request cloning in between) because the mux sets r.Pattern on the request
// it receives.
func AccessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)

			route := cmp.Or(r.Pattern, routeUnmatched)
			log.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("request_id", reqctx.RequestID(r.Context())),
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.Int("status", sw.status()),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			)
		})
	}
}

// statusWriter records the response status and whether headers were sent.
type statusWriter struct {
	http.ResponseWriter
	code        int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.code = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) status() int {
	if !w.wroteHeader {
		return http.StatusOK
	}
	return w.code
}
