package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

// Recover turns a handler panic into a logged error and, when the response
// has not started yet, a 500 internal.unexpected envelope. It is the
// outermost middleware, so it reads the request ID back from the response
// header that RequestID sets. http.ErrAbortHandler is re-panicked so net/http
// can abort the connection as intended.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusWriter{ResponseWriter: w}
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler {
					panic(v)
				}
				id := w.Header().Get(HeaderRequestID)
				log.ErrorContext(r.Context(), "http: panic recovered",
					"request_id", id,
					"method", r.Method,
					"path", r.URL.Path,
					"panic", v,
					"stack", string(debug.Stack()),
				)
				if sw.wroteHeader {
					return
				}
				r = r.WithContext(reqctx.WithRequestID(r.Context(), id))
				response.Error(w, r, http.StatusInternalServerError, errcodes.InternalUnexpected,
					"internal server error", nil)
			}()
			next.ServeHTTP(sw, r)
		})
	}
}
