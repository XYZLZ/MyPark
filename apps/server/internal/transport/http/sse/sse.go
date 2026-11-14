// Package sse writes text/event-stream responses (design §11.3). Every SSE
// write in the server goes through it.
package sse

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrCannotFlush means the response writer, through any Unwrap chain, cannot
// flush, so events would sit in a buffer instead of reaching the client.
var ErrCannotFlush = errors.New("sse: response writer cannot flush")

// Writer writes server-sent events. Each write sets a fresh write deadline,
// so a stream outlives the server's WriteTimeout while it keeps writing, and
// flushes, so each event reaches the client at once. It is not safe for
// concurrent use.
type Writer struct {
	w       http.ResponseWriter
	rc      *http.ResponseController
	timeout time.Duration
	buf     bytes.Buffer
}

// New sets the event-stream headers on w. It does not write the status; the
// first event does. writeTimeout bounds each write.
func New(w http.ResponseWriter, writeTimeout time.Duration) (*Writer, error) {
	if !canFlush(w) {
		return nil, ErrCannotFlush
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	return &Writer{w: w, rc: http.NewResponseController(w), timeout: writeTimeout}, nil
}

// canFlush walks the Unwrap chain the way http.ResponseController does,
// without flushing (which would commit the status).
func canFlush(w http.ResponseWriter) bool {
	for {
		switch t := w.(type) {
		case interface{ FlushError() error }, http.Flusher:
			return true
		case interface{ Unwrap() http.ResponseWriter }:
			w = t.Unwrap()
		default:
			return false
		}
	}
}

// Retry tells the client how long to wait before reconnecting.
func (w *Writer) Retry(ms int) error {
	w.buf.Reset()
	w.buf.WriteString("retry: " + strconv.Itoa(ms) + "\n\n")
	return w.flush()
}

// Event writes one event. The id line is omitted when id is empty. data is
// written as is; a line break in it starts another data line.
func (w *Writer) Event(name, id string, data []byte) error {
	if strings.ContainsAny(name, "\r\n") || strings.ContainsAny(id, "\r\n") {
		return fmt.Errorf("sse: line break in event name or id")
	}
	w.buf.Reset()
	w.buf.WriteString("event: " + name + "\n")
	if id != "" {
		w.buf.WriteString("id: " + id + "\n")
	}
	for line := range bytes.Lines(data) {
		w.buf.WriteString("data: ")
		w.buf.Write(bytes.TrimRight(line, "\r\n"))
		w.buf.WriteByte('\n')
	}
	if len(data) == 0 {
		w.buf.WriteString("data: \n")
	}
	w.buf.WriteByte('\n')
	return w.flush()
}

// Comment writes a comment line, which clients ignore.
func (w *Writer) Comment(text string) error {
	if strings.ContainsAny(text, "\r\n") {
		return fmt.Errorf("sse: line break in comment")
	}
	w.buf.Reset()
	w.buf.WriteString(": " + text + "\n\n")
	return w.flush()
}

// flush sets the write deadline (wall clock: it is a network deadline), then
// writes and flushes the buffered frame. A writer without deadline support,
// like httptest.ResponseRecorder, is fine.
func (w *Writer) flush() error {
	err := w.rc.SetWriteDeadline(time.Now().Add(w.timeout))
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("sse: set write deadline: %w", err)
	}
	if _, err := w.w.Write(w.buf.Bytes()); err != nil {
		return fmt.Errorf("sse: write: %w", err)
	}
	if err := w.rc.Flush(); err != nil {
		return fmt.Errorf("sse: flush: %w", err)
	}
	return nil
}
