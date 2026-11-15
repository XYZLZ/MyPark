package availability

// WaitIdle waits until every query the hub started has finished. Tests call
// it only when no timer or caller can start another meanwhile.
func (h *Hub) WaitIdle() { h.wg.Wait() }

// MaxRefetches is maxRefetches, for the hub tests.
const MaxRefetches = maxRefetches
