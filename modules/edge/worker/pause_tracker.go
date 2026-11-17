package worker

import "sync"

// PauseTracker tracks cumulative queue-wait and CI-pause milliseconds per
// consumer so time spent waiting for room or paused behind CI is excluded from
// step timeouts and DeadlineAt (R39b).
type PauseTracker struct {
	mu           sync.Mutex
	pausedMillis map[string]int64
}

// NewPauseTracker constructs an initialized PauseTracker.
func NewPauseTracker() *PauseTracker {
	return &PauseTracker{
		pausedMillis: make(map[string]int64),
	}
}

// AddMillis records an incremental wait/pause span for consumer.
func (pt *PauseTracker) AddMillis(consumer string, deltaMs int64) {
	if pt == nil || consumer == "" || deltaMs <= 0 {
		return
	}
	pt.mu.Lock()
	pt.pausedMillis[consumer] += deltaMs
	pt.mu.Unlock()
}

// SetFloorMillis ensures consumer's cumulative paused milliseconds is at least
// floorMs (e.g. from live nova-reserve subscribe snapshots).
func (pt *PauseTracker) SetFloorMillis(consumer string, floorMs int64) {
	if pt == nil || consumer == "" || floorMs <= 0 {
		return
	}
	pt.mu.Lock()
	if floorMs > pt.pausedMillis[consumer] {
		pt.pausedMillis[consumer] = floorMs
	}
	pt.mu.Unlock()
}

// TotalMillis returns the cumulative wait+pause milliseconds recorded for consumer.
func (pt *PauseTracker) TotalMillis(consumer string) int64 {
	if pt == nil || consumer == "" {
		return 0
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.pausedMillis[consumer]
}
