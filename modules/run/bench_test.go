package run

// Benchmarks, unit tier (store.Fake, a fake policy, no clock): what run
// does every tick besides layer 3's plan and layer 5's answer, on a settled
// sprint with nothing to do, over fleet size and card count. Run them on a
// bench:
//
//	go test -run '^$' -bench . -benchmem ./modules/run/

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/dash"
	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// idleAsking answers every tick with the catalog, limits and timeouts and
// nothing per subject: the engine's ranking deals, and the fake's own cost
// is no part of the measure.
type idleAsking struct{}

func (idleAsking) AnswerContext(_ context.Context, _ policy.Versions, snap store.Snapshot, _ roster.Roster, _ policy.Questions) (policy.Answers, []policy.Failure) {
	return policy.Answers{Version: snap.Policy, Catalog: benchCatalog(),
		Limits:   policy.Limits{Bounces: 3, Faults: 3, SameFinding: 3, ReportsPerTick: 100, FleetWidthCap: 4, FlapLimit: 3, EscalateAfter: 2, FrontierAfter: 3},
		Timeouts: policy.Timeouts{RowSilentGone: 600, RowSilentWithdraw: 1800, LandingStall: 60, Judgement: 3600, RouteRestFor: 1800, Tick: 1, ReadyNag: testReadyNag, ReadyTake: 180, BaseRegate: []int{120}}}, nil
}

// benchRun is a settled, started sprint on a Fake: rows rows of width 4
// serving every tier, all on the bus; cards producers in streams of 100 (at
// most 20), the first four per row working (an acknowledged work consumer
// each), the rest ready. It returns the ticker after the ticks that settle
// it, and the tick's observed inputs.
func benchRun(b *testing.B, rows, cards int) (*Ticker, Observed) {
	b.Helper()
	snap := store.Snapshot{Epoch: 1, Policy: 1, Streams: map[string]store.Stream{}, Cards: map[string]store.Card{}, Rows: map[string]store.Row{},
		Judgements: map[string]store.Judgement{}, Cursors: map[string]int64{}, Identity: store.Identity{Name: "Bench", Email: "bench@example.invalid"}}
	ro := roster.Roster{Workers: map[string]roster.Worker{}, Kin: map[string]roster.Kin{}}
	o := Observed{Now: t0, Roster: ro, Heads: map[string]string{}}
	streams := max(1, min(20, cards/100))
	for i := range streams {
		name := fmt.Sprintf("s%02d", i)
		snap.Streams[name] = store.Stream{Name: name, Repo: "https://example.invalid/r.git", Base: "main", Pos: i + 1, Mode: "auto", Forge: "github"}
		o.Heads[name] = fmt.Sprintf("%040x", i+1)
	}
	for i := range rows {
		name := fmt.Sprintf("r%05d", i)
		snap.Rows[name] = store.Row{Name: name, State: "up", Session: "hook-" + name, Role: "worker"}
		ro.Workers[name] = roster.Worker{Name: name, Width: 4, Tiers: store.Tiers}
		o.Present = append(o.Present, name)
	}
	for i := range cards {
		id := fmt.Sprintf("p%05d", i)
		p := store.Card{ID: id, Kind: "producer", Stream: fmt.Sprintf("s%02d", i%streams), State: "ready", Tier: "pro", Brief: "do " + id, Paths: []string{fmt.Sprintf("a/%05d", i)}, Score: i}
		if i < rows*4 {
			row := fmt.Sprintf("r%05d", i/4)
			p.State, p.Attempt, p.Holder, p.Branch = "working", 1, row, "sprint/"+p.Stream+"/"+id
			w := store.Card{ID: id + ".w1", Kind: "work", Of: id, Stream: p.Stream, State: "working", Tier: "pro", Holder: row, Attempt: 1, Branch: p.Branch, Model: "test/pro"}
			snap.Cards[w.ID] = w
			snap.Cursors[steps.AckCursor(1, w.ID)] = 1
		}
		snap.Cards[id] = p
	}
	f, err := store.NewFakeFrom(snap, store.PolicyVersion{Version: 1, Kind: "script", Name: "test.star", Body: "# test", Active: true})
	if err != nil {
		b.Fatal(err)
	}
	tk, err := NewTicker(context.Background(), f, &healthy{}, idleAsking{}, t0)
	if err != nil {
		b.Fatal(err)
	}
	for i := range 3 { // the first ticks open what the world owes (judgements); then it is idle
		o.Now = t0.Add(time.Duration(i+1) * time.Second)
		if _, err := tk.Tick(context.Background(), o); err != nil {
			b.Fatal(err)
		}
		tk.Push(context.Background(), &fakeBus{}, o.Now)
	}
	o.Now = o.Now.Add(time.Second)
	if _, err := tk.Tick(context.Background(), o); err != nil {
		b.Fatal(err)
	}
	if c := tk.Counts(); c.Applied+c.Refused > 0 {
		b.Fatalf("the world is not idle: %+v", c)
	}
	return tk, o
}

func benchSizes(b *testing.B, f func(b *testing.B, rows, cards int)) {
	for _, rows := range []int{10, 100, 1000, 10000} {
		for _, cards := range []int{100, 1000, 10000} {
			b.Run(fmt.Sprintf("rows=%d/cards=%d", rows, cards), func(b *testing.B) { f(b, rows, cards) })
		}
	}
}

// BenchmarkTickerIdle is one idle Ticker.Tick: the fold of no event, the
// presence, the plan of nothing, no op.
func BenchmarkTickerIdle(b *testing.B) {
	benchSizes(b, func(b *testing.B, rows, cards int) {
		tk, o := benchRun(b, rows, cards)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := tk.Tick(context.Background(), o); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkTickerBoard is the rest of an idle tick in the run loop
// (loop.tick): the push of nothing, the snapshot copy and the board's
// publish.
func BenchmarkTickerBoard(b *testing.B) {
	benchSizes(b, func(b *testing.B, rows, cards int) {
		tk, o := benchRun(b, rows, cards)
		board := dash.NewFromFeed(dash.Feed{Snap: tk.Snapshot(), At: o.Now}, func() time.Time { return o.Now })
		bus := &fakeBus{}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			tk.Push(context.Background(), bus, o.Now)
			snap := tk.Snapshot()
			if err := board.Publish(dash.Feed{Snap: snap, Roster: o.Roster.Advertised(snap.Rows, ""), Landings: tk.Landings(), At: o.Now}); err != nil {
				b.Fatal(err)
			}
		}
		if len(bus.sent) > 0 {
			b.Fatalf("an idle tick pushed %d envelopes", len(bus.sent))
		}
	})
}

// canned is a store whose Events serves one event, renumbered after the
// cursor, each time give is set: the fold's cost without the Fake's.
type canned struct {
	store.Store
	ev   store.Event
	give bool
}

func (c *canned) Events(_ context.Context, after int64, _ int) ([]store.Event, error) {
	if !c.give {
		return nil, nil
	}
	c.give = false
	e := c.ev
	e.Seq = after + 1
	return []store.Event{e}, nil
}

// BenchmarkTickerFold is the fold of one event, a report moving a work
// consumer and its producer, into the ticker (the view, the snapshot copy,
// the pusher): one unit of own work.
func BenchmarkTickerFold(b *testing.B) {
	benchSizes(b, func(b *testing.B, rows, cards int) {
		tk, o := benchRun(b, rows, cards)
		r, err := tk.store.Apply(context.Background(), store.Op{ID: "bench-report", Epoch: 1, Actor: "r00000", Kind: "report", At: o.Now,
			Writes: []store.Write{store.SetCardState{Card: "p00000.w1", State: "done"}, store.SetCardState{Card: "p00000", State: "review"}}})
		if err != nil || !r.Accepted {
			b.Fatal(r, err)
		}
		es, err := tk.store.Events(context.Background(), tk.snap.Seq, 0)
		if err != nil || len(es) != 1 {
			b.Fatal(len(es), err)
		}
		c := &canned{Store: tk.store, ev: es[0]}
		tk.store = c
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			c.give = true
			if err := tk.Fold(context.Background()); err != nil {
				b.Fatal(err)
			}
		}
	})
}
