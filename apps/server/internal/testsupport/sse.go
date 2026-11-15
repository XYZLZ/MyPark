package testsupport

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"time"
)

// SSEFrame is one parsed server-sent event block: a retry line, or a named
// event with an optional id and its data lines joined by "\n".
type SSEFrame struct {
	Retry, Event, ID, Data string
}

// ReadSSE parses r into frames on its own goroutine, so tests can wait on a
// frame with a timeout instead of blocking on the body. Comment lines are
// skipped. The channel closes at EOF or on a read error.
func ReadSSE(r io.Reader) <-chan SSEFrame {
	out := make(chan SSEFrame, 256)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		var f SSEFrame
		var data []string
		empty := true
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				if !empty {
					f.Data = strings.Join(data, "\n")
					out <- f
				}
				f, data, empty = SSEFrame{}, nil, true
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			name, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch name {
			case "retry":
				f.Retry = value
			case "event":
				f.Event = value
			case "id":
				f.ID = value
			case "data":
				data = append(data, value)
			}
			empty = false
		}
	}()
	return out
}

// NextSSE returns the next frame, failing the test when none arrives within
// timeout or the stream ends.
func NextSSE(t testing.TB, frames <-chan SSEFrame, timeout time.Duration) SSEFrame {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatal("event stream ended")
		}
		return f
	case <-time.After(timeout):
		t.Fatalf("no event within %v", timeout)
	}
	return SSEFrame{}
}
