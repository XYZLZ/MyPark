package simulator

import (
	"bytes"
	"net/http"
	"strconv"
	"testing"
)

func TestHandlerTransport_KeepsContentLength(t *testing.T) {
	var got int64
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.ContentLength
		w.WriteHeader(http.StatusNoContent)
	})
	body := []byte(`{"a":1}`)
	req, err := http.NewRequest(http.MethodPost, "http://api.test/x", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Body = nopCloser{bytes.NewReader(body)}
	req.ContentLength = int64(len(body))
	if _, err := HandlerTransport(h).RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if got != int64(len(body)) {
		t.Errorf("handler saw ContentLength %s, want %d", strconv.FormatInt(got, 10), len(body))
	}
}

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }
