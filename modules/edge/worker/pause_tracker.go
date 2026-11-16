package worker

import (
	"sync"
	"time"
)

// PauseTracker tracks cumulative line-wait and pause milliseconds per consumer (R39b).
type PauseTracker struct {
	mu           sync.Mutex
	pausedMillis map[string]int64
	pauseStart   map[string]time.Time
	lineStart    map[string]time.Time
}

// NewPauseTracker constructs an initialized PauseTracker.
func NewPauseTracker() *PauseTracker {
	return &PauseTracker{
		pausedMillis: make(map[string]int64),
		pauseStart:   make(map[string]time.Time),
		lineStart:    make(map[string]time.Time),
	}
}

// StartLineWait records when a consumer started waiting for a hold in nova-reserve line.
func (pt *PauseTracker) StartLineWait(consumer string, now time.Time) {
	if pt == nil || consumer == "" {
		return
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.lineStart[consumer] = now
}

// EndLineWait records that a hold was acquired or line wait ended.
func (pt *PauseTracker) EndLineWait(consumer string, now time.Time) int64 {
	if pt == nil || consumer == "" {
		return 0
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()
	t0, ok := pt.lineStart[consumer]
	if !ok {
		return 0
	}
	delete(pt.lineStart, consumer)
	delta := now.Sub(t0).Milliseconds()
	if delta > 0 {
		pt.pausedMillis[consumer] += delta
		return delta
	}
	return 0
}

// UpdatePausedConsumers synchronizes currently paused consumers from nova-reserve status.
// Returns true if pause state changed.
func (pt *PauseTracker) UpdatePausedConsumers(paused []string, now time.Time) bool {
	if pt == nil {
		return false
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()
	changed := false
	current := make(map[string]bool, len(paused))
	for _, c := range paused {
		current[c] = true
		if _, ok := pt.pauseStart[c]; !ok {
			pt.pauseStart[c] = now
			changed = true
		}
	}
	for c, t0 := range pt.pauseStart {
		if !current[c] {
			delta := now.Sub(t0).Milliseconds()
			if delta > 0 {
				pt.pausedMillis[c] += delta
			}
			delete(pt.pauseStart, c)
			changed = true
		}
	}
	return changed
}

// Snapshot returns a point-in-time map of total paused/wait ms for all known consumers.
func (pt *PauseTracker) Snapshot(now time.Time) map[string]int64 {
	if pt == nil {
		return nil
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()
	res := make(map[string]int64, len(pt.pausedMillis)+len(pt.pauseStart)+len(pt.lineStart))
	for k, v := range pt.pausedMillis {
		res[k] = v
	}
	for k, t0 := range pt.pauseStart {
		res[k] += now.Sub(t0).Milliseconds()
	}
	for k, t0 := range pt.lineStart {
		res[k] += now.Sub(t0).Milliseconds()
	}
	return res
}

// TotalMillis returns the total elapsed wait+pause ms for consumer at time now.
func (pt *PauseTracker) TotalMillis(consumer string, now time.Time) int64 {
	if pt == nil || consumer == "" {
		return 0
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()
	total := pt.pausedMillis[consumer]
	if t0, ok := pt.pauseStart[consumer]; ok {
		total += now.Sub(t0).Milliseconds()
	}
	if t0, ok := pt.lineStart[consumer]; ok {
		total += now.Sub(t0).Milliseconds()
	}
	return total
}
