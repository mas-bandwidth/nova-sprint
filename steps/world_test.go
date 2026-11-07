package steps

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/require"
)

// world is a store.Fake with the records a test needs and the view folded
// from its log. No clock: the fake clock starts at t0 (1000 s after the
// epoch, never 0) and each op is one second later.
type world struct {
	t      *testing.T
	f      *store.Fake
	v      *View
	roster FakeRoster
	cursor int64
	n      int
	now    time.Time
}

var t0 = time.Unix(1000, 0).UTC()

const base = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// sha is a distinct full commit sha for n.
func sha(n int) string { return fmt.Sprintf("%040x", n+1) }

func newWorld(t *testing.T) *world {
	w := &world{t: t, f: store.NewFake(), now: t0, roster: FakeRoster{}}
	w.v = Build(w.snap(), t0)
	return w
}

func (w *world) snap() store.Snapshot {
	s, err := w.f.Snapshot(context.Background())
	require.NoError(w.t, err)
	return s
}

// apply applies writes as one op and returns its receipt.
func (w *world) apply(kind string, expect []store.Expect, writes ...store.Write) store.Receipt {
	w.n++
	w.now = w.now.Add(time.Second)
	op := store.Op{ID: fmt.Sprintf("op%d", w.n), Epoch: w.snap().Epoch, Actor: "test", Kind: kind, At: w.now, Expect: expect, Writes: writes}
	r, err := w.f.Apply(context.Background(), op)
	require.NoError(w.t, err, "%s: %+v", kind, op)
	return r
}

// must applies writes and requires them accepted.
func (w *world) must(kind string, writes ...store.Write) {
	w.t.Helper()
	r := w.apply(kind, nil, writes...)
	require.True(w.t, r.Accepted, "%s refused: %s", kind, r.Reason)
}

// move applies a card plan and requires it accepted.
func (w *world) move(p card.Plan, r *card.Refusal) {
	w.t.Helper()
	require.Nil(w.t, r, "%v", r)
	rc := w.apply("move", p.Expect, p.Writes...)
	require.True(w.t, rc.Accepted, "refused: %s", rc.Reason)
}

// fold advances the view by the events since the last fold.
func (w *world) fold() {
	w.t.Helper()
	for {
		es, err := w.f.Events(context.Background(), w.cursor, 0)
		require.NoError(w.t, err)
		if len(es) == 0 {
			return
		}
		w.v.Fold(es)
		w.cursor = es[len(es)-1].Seq
	}
}

// stream, row and producer seed the usual records.
func (w *world) stream(name string) {
	w.must("stream", store.PutStream{Name: name, Repo: "git@example.com:r.git", Base: "main", Pos: len(w.v.Streams) + 1})
}

func (w *world) row(name string, width int, tiers ...string) {
	w.roster[name] = RosterRow{Kind: "worker", Width: width, Tiers: tiers}
	w.must("row", store.PutRow{Name: name, Kind: "worker", Width: width, Tiers: tiers, Model: "m-" + name, Session: "hook-" + name})
	w.must("seen", store.SetRowState{Row: name, State: "up"})
}

func (w *world) producer(id, stream, tier string, paths ...string) {
	w.must("add", store.PutCard{ID: id, Kind: "producer", Stream: stream, Tier: tier, Brief: "do " + id, Paths: paths})
}

func (w *world) card(id string) store.Card {
	c, ok := w.snap().Cards[id]
	require.True(w.t, ok, "card %s", id)
	return c
}

// cardOf, consumersOf, rowsOf, streamOf and needsOf read the store for a
// card move, as the steps read the view.
func (w *world) plan(p string, e card.Event) (card.Plan, *card.Refusal) {
	s := w.snap()
	var cons []card.Card
	needs := map[string]card.Card{}
	for _, c := range s.Cards {
		if c.Of == p {
			cons = append(cons, card.FromStore(c))
		}
		needs[c.ID] = card.FromStore(c)
	}
	rows := map[string]card.Row{}
	for _, r := range s.Rows {
		rows[r.Name] = card.RowFromStore(r)
	}
	pc := card.FromStore(s.Cards[p])
	return card.Move(pc, cons, rows, card.StreamFromStore(s.Streams[pc.Stream]), needs, e)
}

func (w *world) consumer(c string, e card.Event) (card.Plan, *card.Refusal) {
	s := w.snap()
	cc := card.FromStore(s.Cards[c])
	return card.MoveConsumer(cc, card.FromStore(s.Cards[cc.Of]), card.RowFromStore(s.Rows[cc.Holder]), e)
}

// report applies a row's report with its producer move (ReportPlan).
func (w *world) report(c, result, head, reason string) {
	w.t.Helper()
	w.move(w.reportPlan(c, result, head, reason))
}

// reportPlan is card.ReportPlan read from the store.
func (w *world) reportPlan(c, result, head, reason string) (card.Plan, *card.Refusal) {
	s := w.snap()
	cc := card.FromStore(s.Cards[c])
	p := card.FromStore(s.Cards[cc.Of])
	var cons []card.Card
	needs := map[string]card.Card{}
	for _, x := range s.Cards {
		if x.Of == p.ID {
			cons = append(cons, card.FromStore(x))
		}
		needs[x.ID] = card.FromStore(x)
	}
	rows := map[string]card.Row{}
	for _, r := range s.Rows {
		rows[r.Name] = card.RowFromStore(r)
	}
	return card.ReportPlan(cc, p, cons, rows, card.StreamFromStore(s.Streams[p.Stream]), needs, card.Report{Row: cc.Holder, Result: result, Head: head, Reason: reason})
}

// inputs is the tick's inputs at the world's clock: every row seen now, every
// stream at base.
func (w *world) inputs() Inputs {
	in := Inputs{Now: w.now.Add(time.Second), Heads: map[string]string{}, Seen: map[string]time.Time{}, Roster: w.roster}
	for s := range w.v.Streams {
		in.Heads[s] = base
	}
	for r := range w.v.Rows {
		in.Seen[r] = in.Now
	}
	return in
}
