package dash

import (
	"fmt"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Feed is what run publishes each tick: the snapshot of its fold and the
// roster read with it, one pair, never one without the other (Stella's
// handoff finding 6: a fresh snapshot is never shown beside an old roster).
// RosterErr is the roster read's failure, if it failed: the board then shows
// every row at width 0 and raises the stale alarm, and still shows the
// store's own lines (STOPPED among them) as fresh.
type Feed struct {
	Snap      store.Snapshot
	Roster    roster.Roster
	RosterErr error
	At        time.Time
	// Landings is the epoch's landing times (the v1 import's excluded), every
	// one, that run keeps from the store's events. nil means run could not
	// read them (the ETA then reads "-"); non-nil, even empty, is the store's
	// answer.
	Landings []time.Time
}

// NewFromFeed is the run-host dashboard: it reads no store; run publishes.
// now is run's clock.
func NewFromFeed(f Feed, now func() time.Time) *Cache {
	c := &Cache{now: now, gate: make(chan struct{}, 1), initialized: true}
	c.publish(f)
	c.samples = []sample{{f.At, c.view.Landed}}
	return c
}

// Publish shows f. The snapshot is mapped before Publish returns, so the
// caller may change it after. A snapshot older than the one shown, or an
// absent time, is refused.
func (c *Cache) Publish(f Feed) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if f.At.IsZero() || f.Snap.Seq < c.state.Seq || f.At.Before(c.at) {
		return fmt.Errorf("dashboard: a run snapshot older than the one shown (seq %d at %s)", f.Snap.Seq, f.At.Format(time.RFC3339))
	}
	c.publish(f)
	c.addSample(f.At, c.view.Landed)
	return nil
}

func (c *Cache) publish(f Feed) {
	c.state = store.Snapshot{Seq: f.Snap.Seq, Epoch: f.Snap.Epoch}
	c.at, c.err, c.alarm = f.At, nil, false
	ro := f.Roster
	if f.RosterErr != nil {
		ro, c.alarm = roster.Roster{}, true
	}
	c.landings, c.landingsOK = nil, f.Landings != nil
	if c.landingsOK {
		c.landings = append([]time.Time{}, f.Landings...)
	}
	c.view = mapSnapshot(f.Snap, ro, f.At)
	if f.RosterErr != nil {
		c.view.Error = "roster: " + f.RosterErr.Error()
	}
}

// CheckFreshness raises the stale alarm without reading the store or waiting
// for a dashboard viewer. The caller supplies its clock's current time.
func (c *Cache) CheckFreshness(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkAlarm(at)
}
