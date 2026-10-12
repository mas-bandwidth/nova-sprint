package hostload

import (
	"sync"
)

// RingSize is how many one-second samples the ring holds: the ten seconds the
// fleet table's load cell shows the highest of.
const RingSize = 10

// Ring is the last RingSize samples of a busy percent, each capped at
// MaxPercent. The zero Ring is empty.
type Ring struct {
	v [RingSize]float64
	n uint64 // samples ever added
}

// Count is how many samples were ever added.
func (r *Ring) Count() uint64 { return r.n }

// MaxSince is the highest of the samples added after the first count of them, at
// most the RingSize held; false when none was added since.
func (r *Ring) MaxSince(count uint64) (float64, bool) {
	if r.n <= count {
		return 0, false
	}
	k := min(r.n-count, RingSize)
	m := 0.0
	for i := r.n - k; i < r.n; i++ {
		m = max(m, r.v[i%RingSize])
	}
	return m, true
}

// Sampler takes the machine's busy percent once a second into a Ring. Its readings
// are the Source's: ProcStat counters (the percent between two readings), else
// CPUSecond (a meter that takes its own second). Safe for use from the goroutine that
// runs it and the one that reads it.
type Sampler struct {
	mu   sync.Mutex
	ring Ring
}

// Peak is the highest sample added after the first since of them (at most the last
// RingSize), and how many samples there have been in all, which the caller passes as
// since next time once its beat is written; false when none is new.
func (s *Sampler) Peak(since uint64) (pct float64, total uint64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pct, ok = s.ring.MaxSince(since)
	return pct, s.ring.Count(), ok
}
