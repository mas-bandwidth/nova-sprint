package run

import (
	"context"
	"time"

	"github.com/mas-bandwidth/nova-sprint/dash"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

type dashboardUpdate struct {
	state store.Snapshot
	at    time.Time
	roster roster.Roster
	hasRoster bool
}

// DashboardPublisher replaces a queued update with the newest; Offer never
// waits for rendering or HTTP, and no dashboard store reads are made.
type DashboardPublisher struct {
	cache   *dash.Cache
	clock   Clock
	updates chan dashboardUpdate
}

func NewDashboardPublisher(cache *dash.Cache, clock Clock) *DashboardPublisher {
	return &DashboardPublisher{cache: cache, clock: clock, updates: make(chan dashboardUpdate, 1)}
}
func (p *DashboardPublisher) Offer(s store.Snapshot, at time.Time) {
	p.offer(dashboardUpdate{state: s, at: at})
}
func (p *DashboardPublisher) OfferRoster(s store.Snapshot, at time.Time, r roster.Roster) {
	p.offer(dashboardUpdate{state: s, at: at, roster: copyRunRoster(r), hasRoster: true})
}
func (p *DashboardPublisher) offer(update dashboardUpdate) {
	select {
	case p.updates <- update:
		return
	default:
	}
	select {
	case <-p.updates:
	default:
	}
	select {
	case p.updates <- update:
	default:
	}
}
func (p *DashboardPublisher) Run(ctx context.Context) error {
	ticker := p.clock.Every(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-ticker.C():
			if !ok {
				return nil
			}
			p.cache.CheckFreshness(p.clock.Now())
		case update := <-p.updates:
			var err error
			if update.hasRoster {
				err = p.cache.PublishSnapshotWithRoster(update.state, update.roster, update.at)
			} else {
				err = p.cache.PublishSnapshot(update.state, update.at)
			}
			if err != nil {
				return err
			}
		}
	}
}
