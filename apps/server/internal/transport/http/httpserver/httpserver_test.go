package httpserver_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpserver"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

// TestEnvelope_ParallelHandlersNoRace checks concurrent requests through the
// full chain each see their own meta.request_id (no shared global, F4).
func TestEnvelope_ParallelHandlersNoRace(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x", func(w http.ResponseWriter, r *http.Request) {
		response.OK(w, r, http.StatusOK, map[string]string{"k": "v"})
	})
	h := httpserver.NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), mux)

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			want := fmt.Sprintf("req-%d", i)
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.Header.Set(middleware.HeaderRequestID, want)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			var body struct {
				Meta struct {
					RequestID string `json:"request_id"`
				} `json:"meta"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Errorf("req %d: decode: %v", i, err)
				return
			}
			if body.Meta.RequestID != want {
				t.Errorf("req %d: meta.request_id = %q, want %q", i, body.Meta.RequestID, want)
			}
		})
	}
	wg.Wait()
}
