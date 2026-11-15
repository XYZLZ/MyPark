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

func TestEventLog_LastSeqAndAfterOnAFullRing(t *testing.T) {
	l := NewEventLog(3)
	if l.LastSeq() != 0 {
		t.Errorf("LastSeq of an empty log = %d", l.LastSeq())
	}
	for range 5 {
		l.Append(LogEntry{})
	}
	if l.LastSeq() != 5 {
		t.Errorf("LastSeq = %d, want 5", l.LastSeq())
	}
	for _, c := range []struct {
		after int64
		want  []int64
	}{{0, []int64{3, 4, 5}}, {2, []int64{3, 4, 5}}, {3, []int64{4, 5}}, {4, []int64{5}}, {5, nil}, {9, nil}} {
		var got []int64
		for _, e := range l.After(c.after) {
			got = append(got, e.Seq)
		}
		if len(got) != len(c.want) {
			t.Errorf("After(%d) = %v, want %v", c.after, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("After(%d) = %v, want %v", c.after, got, c.want)
			}
		}
	}
}
