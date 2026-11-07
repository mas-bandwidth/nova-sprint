package dash

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// NewFromSnapshot creates the run-host dashboard without reading the store.
// now is injected so the serving process and tests share one clock.
func NewFromSnapshot(s store.Snapshot, now func() time.Time) *Cache {
	if now == nil {
		now = time.Now
	}
	at := now()
	c := &Cache{state: copySnapshot(s), at: at, now: now, gate: make(chan struct{}, 1)}
	c.view = mapSnapshot(c.state, c.roster, at)
	c.samples = []sample{{at, c.view.Landed}}
	return c
}

// PublishSnapshot accepts only a completed run fold. Idle successful folds
// refresh the clock; an older snapshot or an absent timestamp is refused.
func (c *Cache) PublishSnapshot(s store.Snapshot, at time.Time) error {
	return c.publishSnapshot(s, nil, at)
}

// PublishSnapshotWithRoster publishes one fold and the roster observed with it.
func (c *Cache) PublishSnapshotWithRoster(s store.Snapshot, r roster.Roster, at time.Time) error {
	return c.publishSnapshot(s, &r, at)
}

func (c *Cache) publishSnapshot(s store.Snapshot, r *roster.Roster, at time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if at.IsZero() || s.Seq < c.state.Seq || at.Before(c.at) {
		return fmt.Errorf("dashboard: stale run snapshot")
	}
	c.state = copySnapshot(s)
	if r != nil {
		c.roster = copyRoster(*r)
	}
	c.at = at
	c.err = nil
	c.alarm = false
	c.view = mapSnapshot(c.state, c.roster, at)
	c.addSample(at, c.view.Landed)
	return nil
}

func copyRoster(r roster.Roster) roster.Roster {
	r.Workers = maps.Clone(r.Workers)
	for name, worker := range r.Workers {
		worker.Tiers = slices.Clone(worker.Tiers)
		r.Workers[name] = worker
	}
	r.Kin = maps.Clone(r.Kin)
	for name, kin := range r.Kin {
		kin.Tiers = slices.Clone(kin.Tiers)
		r.Kin[name] = kin
	}
	return r
}
func copySnapshot(s store.Snapshot) store.Snapshot {
	s.Streams = maps.Clone(s.Streams)
	s.Cards = maps.Clone(s.Cards)
	for k, c := range s.Cards {
		c.Needs = slices.Clone(c.Needs)
		c.After = slices.Clone(c.After)
		c.Paths = slices.Clone(c.Paths)
		c.DoneWhen = slices.Clone(c.DoneWhen)
		s.Cards[k] = c
	}
	s.Rows = maps.Clone(s.Rows)
	s.Judgements = maps.Clone(s.Judgements)
	s.Cursors = maps.Clone(s.Cursors)
	return s
}

// CheckFreshness raises the stale alarm without reading the store or waiting
// for a dashboard viewer. The caller supplies its clock's current time.
func (c *Cache) CheckFreshness(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkAlarm(at)
}
