package middleware

import (
	"net/http"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

// JSONErrors serves mux and replaces its built-in plain-text 404 and 405
// responses with JSON envelopes (http.not_found, http.method_not_allowed),
// keeping the Allow header on 405.
//
// It acts as the catch-all instead of a "/" pattern on the mux: with Go 1.22+
// routing a "/" pattern matches every method, so POST /healthz would hit it
// and return 404 instead of 405.
func JSONErrors(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern != "" {
			// ServeHTTP (not h) so the mux sets r.Pattern and path values.
			mux.ServeHTTP(w, r)
			return
		}
		// No route matched: run the built-in handler on a throwaway writer
		// only to learn its status and Allow header.
		probe := &probeWriter{header: http.Header{}}
		h.ServeHTTP(probe, r)
		if probe.code == http.StatusMethodNotAllowed {
			if allow := probe.header.Get("Allow"); allow != "" {
				w.Header().Set("Allow", allow)
			}
			response.Error(w, r, http.StatusMethodNotAllowed, errcodes.HTTPMethodNotAllowed,
				"method not allowed", nil)
			return
		}
		response.Error(w, r, http.StatusNotFound, errcodes.HTTPNotFound, "route not found", nil)
	})
}

// probeWriter discards the body and records the status code.
type probeWriter struct {
	header http.Header
	code   int
}

func (p *probeWriter) Header() http.Header { return p.header }

func (p *probeWriter) Write(b []byte) (int, error) {
	if p.code == 0 {
		p.code = http.StatusOK
	}
	return len(b), nil
}

func (p *probeWriter) WriteHeader(code int) {
	if p.code == 0 {
		p.code = code
	}
}
