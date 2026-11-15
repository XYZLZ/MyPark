package simulator

import (
	"sync"
	"time"
)

// LogEntry is one device event the simulator sent. It never carries a ticket
// code or a card UID.
type LogEntry struct {
	Seq    int64     `json:"seq"`
	At     time.Time `json:"at"`
	Device string    `json:"device"`
	Type   string    `json:"type"`
	Result string    `json:"result"`
	Reason string    `json:"reason,omitempty"`
	CarID  string    `json:"car_id,omitempty"`
}

// EventLog is a ring buffer of the latest entries (design §13.3).
type EventLog struct {
	mu      sync.Mutex
	entries []LogEntry // oldest first, at most capacity
	cap     int
	seq     int64
}

// NewEventLog returns a log that keeps the latest capacity entries.
func NewEventLog(capacity int) *EventLog {
	return &EventLog{cap: max(capacity, 1)}
}

// Append stamps e with the next sequence number (from 1) and stores it,
// dropping the oldest entry when the log is full.
func (l *EventLog) Append(e LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	e.Seq = l.seq
	if len(l.entries) == l.cap {
		copy(l.entries, l.entries[1:])
		l.entries = l.entries[:l.cap-1]
	}
	l.entries = append(l.entries, e)
}

// After returns a copy of the entries with a sequence number above seq, oldest
// first.
func (l *EventLog) After(seq int64) []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []LogEntry{}
	for _, e := range l.entries {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out
}
