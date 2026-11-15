package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/middleware"
)

// assertEnvelopeShape checks the spec 6.0 envelope of any response: 204 has
// no body, everything else is exactly one of data or error plus
// meta.request_id equal to X-Request-Id, lists carry meta.page, and the
// security headers are present.
func assertEnvelopeShape(t *testing.T, label string, rec *httptest.ResponseRecorder) {
	t.Helper()
	for k, v := range middleware.SecurityHeaders {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s: header %s = %q, want %q", label, k, got, v)
		}
	}
	id := rec.Header().Get(middleware.HeaderRequestID)
	if id == "" {
		t.Errorf("%s: no X-Request-Id", label)
	}
	if rec.Code == http.StatusNoContent {
		if rec.Body.Len() != 0 {
			t.Errorf("%s: 204 with body %q", label, rec.Body)
		}
		return
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("%s: Content-Type = %q", label, ct)
	}
	body := decodeBody(t, rec)
	_, hasData := body["data"]
	errObj, hasErr := body["error"].(map[string]any)
	if hasData == hasErr {
		t.Errorf("%s: want exactly one of data/error, got %s", label, rec.Body)
		return
	}
	meta, _ := body["meta"].(map[string]any)
	if meta["request_id"] != id {
		t.Errorf("%s: meta.request_id = %v, want %q", label, meta["request_id"], id)
	}
	if hasErr {
		if _, ok := errObj["details"].(map[string]any); !ok {
			t.Errorf("%s: error.details is not an object: %s", label, rec.Body)
		}
		if _, ok := errObj["code"].(string); !ok {
			t.Errorf("%s: error.code missing: %s", label, rec.Body)
		}
	}
	if _, isList := body["data"].([]any); isList {
		page, _ := meta["page"].(map[string]any)
		if _, ok := page["limit"]; !ok {
			t.Errorf("%s: list without meta.page.limit: %s", label, rec.Body)
		}
		if _, ok := page["next_cursor"]; !ok {
			t.Errorf("%s: list without meta.page.next_cursor: %s", label, rec.Body)
		}
	} else if meta["page"] != nil {
		t.Errorf("%s: non-list with meta.page: %s", label, rec.Body)
	}
}

// assertStreamOrEnvelope checks the stream route: an unknown lot is a 404
// envelope, a known lot an event stream (not enveloped, design §11.3) that
// starts with retry: 3000.
func assertStreamOrEnvelope(t *testing.T, e *apiEnv) {
	t.Helper()
	path := "/v1/lots/" + uuid.NewString() + "/availability/stream"
	rec := e.serve(streamReq(path), "")
	if rec.Code != http.StatusNotFound || errCode(decodeBody(t, rec)) != string(errcodes.LotNotFound) {
		t.Errorf("unknown lot: got %d %s, want 404 lot.not_found", rec.Code, rec.Body)
	}
	assertEnvelopeShape(t, "unknown lot", rec)

	var pub uuid.UUID
	lotID := testsupport.SeedPilotLot(t, e.db)
	if err := e.db.App.QueryRow(t.Context(), "select public_id from lots where id = $1", lotID).Scan(&pub); err != nil {
		t.Fatal(err)
	}
	rec = e.serve(streamReq("/v1/lots/"+pub.String()+"/availability/stream"), "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("known lot: got %d %q, want 200 text/event-stream", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.HasPrefix(rec.Body.String(), "retry: 3000\n\n") {
		t.Errorf("known lot: body starts %q, want retry: 3000", rec.Body.String())
	}
}

// Spec 6.0: every registered route answers with the envelope, for an
// anonymous caller and for the authorized owner.
func TestEveryRouteReturnsEnvelope(t *testing.T) {
	e := newAPIEnv(t)
	for _, rt := range e.routes {
		entry, ok := matrix[rt.Pattern]
		if !ok {
			t.Errorf("route %s has no matrix entry", rt.Pattern)
			continue
		}
		if rt.Pattern == streamPattern {
			t.Run(rt.Pattern, func(t *testing.T) { assertStreamOrEnvelope(t, e) })
			continue
		}
		t.Run(rt.Pattern, func(t *testing.T) {
			f := e.newFixture()
			assertEnvelopeShape(t, "anonymous", e.serve(entry.build(f), ""))
			assertEnvelopeShape(t, "owner", e.serve(entry.build(f), f.owner.token))
			// The admin routes only answer the envelope for an admin.
			if rt.Policy.Roles != nil && rt.Policy.Roles[0] == "admin" {
				assertEnvelopeShape(t, "admin", e.serve(entry.build(f), f.admin.token))
			}
		})
	}
	// Errors the router itself raises are enveloped too.
	assertEnvelopeShape(t, "404", e.serve(httptest.NewRequest(http.MethodGet, "/nope", nil), ""))
	assertEnvelopeShape(t, "405", e.serve(httptest.NewRequest(http.MethodPut, "/v1/vehicles", nil), ""))
}
