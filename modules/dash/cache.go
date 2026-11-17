// Package dash serves the board: run's own copy on the tailnet, or a pulled
// copy of it on a public host.
package dash

import (
	"log"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/reserve"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Cache is the board run publishes each tick (NewFromFeed, Publish).
type Cache struct {
	mu            sync.RWMutex
	state         store.Snapshot
	at            time.Time
	err           error
	view          envelope
	now           func() time.Time
	alarm         bool
	landings      []time.Time // this epoch's landings (not the v1 import's), from the store's events: the ETA's rate, independent of uptime
	landingsOK    bool
	rosterNote    errNote       // the run feed's roster failure, said once per cause
	roster        roster.Roster // each row's kind and width (round 22); SetRoster
	hasFeed       bool
	lastSnap      store.Snapshot
	lastRoster    roster.Roster
	lastRosterErr error
	lastStats     *store.StatsMark
	reserve       reserve.FleetSnapshot
}

// SetRoster sets the roster the page reads each row's kind and width from
// (round 22: they are nova-config's, never the store's). Until it is set
// every row is in the fleet at width 0.
func (c *Cache) SetRoster(r roster.Roster) {
	c.mu.Lock()
	c.roster = r.Clone()
	c.mu.Unlock()
}

// SetReserve updates the live nova-reserve fleet snapshot and re-renders the
// cached Resources table in sub-second time without touching PostgreSQL (R48, R59).
func (c *Cache) SetReserve(res reserve.FleetSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reserve = res.Clone()
	if c.hasFeed {
		c.renderFeedLocked()
	}
}

func (c *Cache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}
func (c *Cache) checkAlarm(now time.Time) {
	if now.Sub(c.at) >= 32*time.Second && !c.alarm {
		log.Printf("ALARM stale: dashboard data is %s old", now.Sub(c.at))
		c.alarm = true
	}
}
