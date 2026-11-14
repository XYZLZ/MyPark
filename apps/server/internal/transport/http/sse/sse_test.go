package sse_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/sse"
)

func TestSSEWriter_Format(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := sse.New(rec, 10*time.Second)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.Retry(3000); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if !rec.Flushed {
		t.Error("Retry did not flush")
	}
	if err := w.Event("availability", "1042", []byte(`{"a":1}`)); err != nil {
		t.Fatalf("Event: %v", err)
	}
	if err := w.Event("spaces", "", []byte(`{"full":false}`)); err != nil {
		t.Fatalf("Event without id: %v", err)
	}
	if err := w.Comment("ping"); err != nil {
		t.Fatalf("Comment: %v", err)
	}

	want := "retry: 3000\n\n" +
		"event: availability\nid: 1042\ndata: {\"a\":1}\n\n" +
		"event: spaces\ndata: {\"full\":false}\n\n" +
		": ping\n\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body =\n%q\nwant\n%q", got, want)
	}
	for k, v := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-store",
		"X-Accel-Buffering": "no",
	} {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}

	if err := w.Event("bad\nname", "", []byte(`{}`)); err == nil {
		t.Error("Event accepted a newline in the event name")
	}
	if err := w.Event("availability", "1\r2", []byte(`{}`)); err == nil {
		t.Error("Event accepted a carriage return in the id")
	}
}

// hidden hides every optional interface of the recorder, Flush included.
type hidden struct{ http.ResponseWriter }

// unwrapping hides them too but exposes the recorder through Unwrap, like
// the middleware wrappers do.
type unwrapping struct{ http.ResponseWriter }

func (u unwrapping) Unwrap() http.ResponseWriter { return u.ResponseWriter }

func TestSSEWriter_NeedsFlush(t *testing.T) {
	if _, err := sse.New(hidden{httptest.NewRecorder()}, time.Second); err == nil {
		t.Error("New accepted a writer that cannot flush")
	}
	rec := httptest.NewRecorder()
	w, err := sse.New(unwrapping{rec}, time.Second)
	if err != nil {
		t.Fatalf("New through Unwrap: %v", err)
	}
	if err := w.Retry(3000); err != nil {
		t.Fatalf("Retry through Unwrap: %v", err)
	}
	if !rec.Flushed {
		t.Error("Flush did not reach the recorder through Unwrap")
	}
}
