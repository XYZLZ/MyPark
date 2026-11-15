package simulator

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEventLog_RingAndAfter(t *testing.T) {
	l := NewEventLog(3)
	for range 5 {
		l.Append(LogEntry{Type: "heartbeat"})
	}
	seqs := func(es []LogEntry) []int64 {
		var s []int64
		for _, e := range es {
			s = append(s, e.Seq)
		}
		return s
	}
	if got := seqs(l.After(0)); len(got) != 3 || got[0] != 3 || got[1] != 4 || got[2] != 5 {
		t.Errorf("After(0) = %v, want [3 4 5]", got)
	}
	if got := seqs(l.After(4)); len(got) != 1 || got[0] != 5 {
		t.Errorf("After(4) = %v, want [5]", got)
	}
	if got := l.After(5); len(got) != 0 {
		t.Errorf("After(5) = %v, want none", got)
	}
}

func TestEventLog_JSONShape(t *testing.T) {
	raw, err := json.Marshal(LogEntry{Seq: 1, At: time.Date(2026, 11, 24, 7, 0, 0, 0, time.UTC),
		Device: "podium-entry-1", Type: "podium.vehicle_detected", Result: "ack"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"seq":1`, `"at":`, `"device":"podium-entry-1"`, `"type":"podium.vehicle_detected"`, `"result":"ack"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("%s lacks %s", raw, key)
		}
	}
	if strings.Contains(string(raw), "reason") || strings.Contains(string(raw), "car_id") {
		t.Errorf("%s carries empty optional fields", raw)
	}
}
