package simulator

import "sync"

// Driver pools (M3B-59): drivers 1..16 serve reservation arrivals and QR
// cars; 17..20 serve only the no-show scenario, so a no-show block never
// takes an arrivals driver out of rotation.
var (
	PoolArrivals = [2]int{1, 16}
	PoolNoShow   = [2]int{17, 20}
)

// DriverPool hands out the drivers of an inclusive Range round-robin. The
// zero Next position starts at Range[0]. It is safe for concurrent use.
type DriverPool struct {
	Range [2]int

	mu   sync.Mutex
	next int // offset into Range of the next candidate
}

// Next returns the next driver the skip function does not refuse (blocked or
// busy drivers), advancing past it. It returns false when every driver of the
// pool is skipped. A nil skip refuses none.
func (p *DriverPool) Next(skip func(n int) bool) (int, bool) {
	size := p.Range[1] - p.Range[0] + 1
	if size <= 0 {
		return 0, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range size {
		off := (p.next + i) % size
		n := p.Range[0] + off
		if skip != nil && skip(n) {
			continue
		}
		p.next = (off + 1) % size
		return n, true
	}
	return 0, false
}
