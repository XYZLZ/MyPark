package gate

import "slices"

// Status is the state of a stay (spec §5.4).
type Status string

// The five stay states.
const (
	StatusEntryPending Status = "entry_pending"
	StatusActive       Status = "active"
	StatusPaid         Status = "paid"
	StatusExited       Status = "exited"
	StatusVoided       Status = "voided"
)

// transitions is the stay state machine. An active stay never exits
// directly: the exit pass hops through paid (design §10.5).
var transitions = map[Status][]Status{
	StatusEntryPending: {StatusActive, StatusVoided},
	StatusVoided:       {StatusActive}, // late entry pass (M2C-31)
	StatusActive:       {StatusPaid},
	StatusPaid:         {StatusActive, StatusExited}, // overstay or lost ticket; exit
}

// CanTransition reports whether a stay may move from one status to another.
func CanTransition(from, to Status) bool {
	return slices.Contains(transitions[from], to)
}
