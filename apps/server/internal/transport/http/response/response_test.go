package response_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

// newReq returns a request whose context carries request ID "r1".
func newReq(method string) *http.Request {
	r := httptest.NewRequest(method, "/x", nil)
	return r.WithContext(reqctx.WithRequestID(r.Context(), "r1"))
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestOK_WrapsDataAndMeta(t *testing.T) {
	rec := httptest.NewRecorder()
	response.OK(rec, newReq(http.MethodPost), http.StatusCreated, map[string]int{"a": 1})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	body := decode(t, rec)
	if _, ok := body["error"]; ok {
		t.Errorf("success body has an error key: %v", body)
	}
	data, _ := body["data"].(map[string]any)
	if len(data) != 1 || data["a"] != float64(1) {
		t.Errorf("data = %v, want {a:1}", body["data"])
	}
	meta, _ := body["meta"].(map[string]any)
	if len(meta) != 1 || meta["request_id"] != "r1" {
		t.Errorf("meta = %v, want only request_id r1", body["meta"])
	}
}

func TestList_EmptyIsArrayWithPage(t *testing.T) {
	rec := httptest.NewRecorder()
	response.List[int](rec, newReq(http.MethodGet), nil, response.Page{Limit: 5})

	body := decode(t, rec)
	if data, ok := body["data"].([]any); !ok || len(data) != 0 {
		t.Errorf("data = %#v, want empty array", body["data"])
	}
	meta, _ := body["meta"].(map[string]any)
	page, ok := meta["page"].(map[string]any)
	if !ok || page["limit"] != float64(5) {
		t.Fatalf("meta.page = %v, want limit 5", meta["page"])
	}
	if v, present := page["next_cursor"]; !present || v != nil {
		t.Errorf("next_cursor = %v (present=%v), want explicit null", v, present)
	}
}

func TestList_NextCursor(t *testing.T) {
	rec := httptest.NewRecorder()
	next := "abc"
	response.List(rec, newReq(http.MethodGet), []string{"x"}, response.Page{Limit: 1, NextCursor: &next})
	meta, _ := decode(t, rec)["meta"].(map[string]any)
	page, _ := meta["page"].(map[string]any)
	if page["next_cursor"] != "abc" {
		t.Errorf("next_cursor = %v, want abc", page["next_cursor"])
	}
}

func TestError_RequestIDInMeta(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Error(rec, newReq(http.MethodGet), http.StatusServiceUnavailable,
		errcodes.HealthNotReady, "db down", nil)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	body := decode(t, rec)
	if _, ok := body["data"]; ok {
		t.Errorf("error body has a data key: %v", body)
	}
	e, _ := body["error"].(map[string]any)
	if _, ok := e["request_id"]; ok {
		t.Errorf("error.request_id must be absent: %v", e)
	}
	if e["code"] != "health.not_ready" || e["message"] != "db down" {
		t.Errorf("error = %v", e)
	}
	if d, ok := e["details"].(map[string]any); !ok || len(d) != 0 {
		t.Errorf("details = %v, want {}", e["details"])
	}
	meta, _ := body["meta"].(map[string]any)
	if meta["request_id"] != "r1" {
		t.Errorf("meta.request_id = %v, want r1", meta["request_id"])
	}
}

func TestError_MissingRequestIDIsEmptyString(t *testing.T) {
	rec := httptest.NewRecorder()
	response.Error(rec, httptest.NewRequest(http.MethodGet, "/x", nil), http.StatusNotFound,
		errcodes.HTTPNotFound, "nf", map[string]any{"k": "v"})
	body := decode(t, rec)
	meta, _ := body["meta"].(map[string]any)
	if v, ok := meta["request_id"]; !ok || v != "" {
		t.Errorf("meta.request_id = %v, want empty string present", v)
	}
	e, _ := body["error"].(map[string]any)
	if d, _ := e["details"].(map[string]any); d["k"] != "v" {
		t.Errorf("details = %v, want k=v", e["details"])
	}
}

func TestNoContent_EmptyBody(t *testing.T) {
	rec := httptest.NewRecorder()
	response.NoContent(rec)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "" {
		t.Errorf("Content-Type = %q, want none", ct)
	}
}

func TestValidationError_Envelope(t *testing.T) {
	rec := httptest.NewRecorder()
	response.ValidationError(rec, newReq(http.MethodPost), &validation.Error{
		Fields: []validation.FieldError{{Field: "plate", Rule: "required"}, {Field: "seats", Rule: "min", Param: "1"}},
	})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	body := decode(t, rec)
	e, _ := body["error"].(map[string]any)
	if e["code"] != "validation.failed" {
		t.Errorf("code = %v, want validation.failed", e["code"])
	}
	details, _ := e["details"].(map[string]any)
	fields, ok := details["fields"].([]any)
	if !ok || len(fields) != 2 {
		t.Fatalf("details.fields = %v, want 2 entries", details["fields"])
	}
	first, _ := fields[0].(map[string]any)
	if first["field"] != "plate" || first["rule"] != "required" || first["param"] != "" {
		t.Errorf("fields[0] = %v, want field=plate rule=required param=\"\"", first)
	}
	second, _ := fields[1].(map[string]any)
	if second["param"] != "1" {
		t.Errorf("fields[1].param = %v, want 1", second["param"])
	}
	if meta, _ := body["meta"].(map[string]any); meta["request_id"] != "r1" {
		t.Errorf("meta = %v", body["meta"])
	}
}

func TestValidationError_NilErrorWritesEmptyFields(t *testing.T) {
	rec := httptest.NewRecorder()
	response.ValidationError(rec, newReq(http.MethodPost), nil)
	e, _ := decode(t, rec)["error"].(map[string]any)
	details, _ := e["details"].(map[string]any)
	if fields, ok := details["fields"].([]any); !ok || len(fields) != 0 {
		t.Fatalf("details.fields = %#v, want empty array", details["fields"])
	}
}
