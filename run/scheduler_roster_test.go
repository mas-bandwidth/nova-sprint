package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

type countingRoster struct { calls int; err error }
func (r *countingRoster) ReadRoster(context.Context) (roster.Roster, error) {
	r.calls++
	return roster.Roster{Workers: map[string]roster.Worker{"alice": {Name: "alice", Width: 2, Tiers: []string{"pro"}}}}, r.err
}
type rosterPublisher struct { roster roster.Roster; calls int }
func (p *rosterPublisher) Offer(store.Snapshot, time.Time) {}
func (p *rosterPublisher) OfferRosterPresence(_ store.Snapshot, _ time.Time, r roster.Roster, _ []string) { p.roster = r; p.calls++ }

func TestSchedulerReadsRosterOnceAndSharesView(t *testing.T) {
	ctx := context.Background()
	steps := &tickSteps{}
	e, err := NewEngine(ctx, store.NewFake(), &tickGuard{}, steps, &tickEdges{})
	if err != nil { t.Fatal(err) }
	r, pub := &countingRoster{}, &rosterPublisher{}
	s, err := NewScheduler(e, &fakeClock{at: time.Unix(1000, 0)}, &fakePresence{}, pub, &fakeLogger{}, fakeResults{}, fakeDeadline{})
	if err != nil { t.Fatal(err) }
	s.roster = r
	if err = s.Step(ctx, nil); err != nil { t.Fatal(err) }
	if r.calls != 1 || pub.calls != 1 || steps.inputs[0].Roster.Workers["alice"].Width != 2 || pub.roster.Workers["alice"].Width != 2 { t.Fatalf("reads=%d publishes=%d inputs=%+v", r.calls, pub.calls, steps.inputs) }
	r.err = errors.New("offline")
	if err = s.Step(ctx, nil); err == nil || len(steps.inputs) != 1 { t.Fatalf("err=%v inputs=%d", err, len(steps.inputs)) }
}
