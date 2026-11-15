package availability

import (
	"net/netip"
	"sync"
)

// StreamCaps bounds the open availability streams, per client IP and in
// total (design §11.5, M2C-53). Both counters sit under one mutex, so the
// two checks and the increments are one atomic step.
type StreamCaps struct {
	perIP, total int

	mu   sync.Mutex
	open int
	byIP map[netip.Addr]int
}

// NewStreamCaps returns caps of perIP streams per client IP and total
// streams overall.
func NewStreamCaps(perIP, total int) *StreamCaps {
	return &StreamCaps{perIP: perIP, total: total, byIP: make(map[netip.Addr]int)}
}

// Acquire takes a stream slot for ip. ok is false when either cap is
// reached. release frees the slot; calling it again does nothing. An IPv4
// address and its IPv4-mapped IPv6 form share one count.
func (c *StreamCaps) Acquire(ip netip.Addr) (release func(), ok bool) {
	ip = ip.Unmap()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.open >= c.total || c.byIP[ip] >= c.perIP {
		return func() {}, false
	}
	c.open++
	c.byIP[ip]++
	var once sync.Once
	return func() { once.Do(func() { c.release(ip) }) }, true
}

func (c *StreamCaps) release(ip netip.Addr) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.open--
	if c.byIP[ip]--; c.byIP[ip] <= 0 {
		delete(c.byIP, ip)
	}
}
