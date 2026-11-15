package simulator

import (
	"net/http"
	"net/http/httptest"
)

// HandlerTransport returns a RoundTripper that serves each request in-process
// with h, for the CI scenario tests (M2C-14). The handler sees RequestURI set
// as on a real server, so signatures over it verify.
func HandlerTransport(h http.Handler) http.RoundTripper { return handlerTransport{h} }

type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	in := httptest.NewRequestWithContext(r.Context(), r.Method, r.URL.RequestURI(), r.Body)
	in.ContentLength = r.ContentLength
	in.Header = r.Header.Clone()
	in.Host = r.URL.Host
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, in)
	res := rec.Result()
	res.Request = r
	return res, nil
}
