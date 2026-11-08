package steps

// Benchmarks, unit tier (store.Fake, no clock): the tick's plan, the deal
// and the fold over fleet size and card count. Run them on a bench:
//
//	go test -run '^$' -bench . -benchmem ./modules/steps/

import (
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

var (
	benchRows  = []int{10, 100, 1000, 10000}
	benchCards = []int{100, 1000, 10000}
)

// benchSizes runs f for every rows x cards pair.
func benchSizes(b *testing.B, f func(b *testing.B, rows, cards int)) {
	for _, rows := range benchRows {
		for _, cards := range benchCards {
			b.Run(fmt.Sprintf("rows=%d/cards=%d", rows, cards), func(b *testing.B) { f(b, rows, cards) })
		}
	}
}

// benchWorld is rows rows of width 4 serving every tier and cards
// producers in streams of 100 (at most 20), each on a file of its own,
// resolved to ready. Rows named in away are not on the bus.
func benchWorld(b *testing.B, rows, cards int, away bool) *sim {
	b.Helper()
	s := newSim(b)
	s.away = set{}
	streams := max(1, min(20, cards/100))
	var ws []store.Write
	for i := range streams {
		ws = append(ws, store.PutStream{Name: fmt.Sprintf("s%02d", i), Repo: "git@example.com:r.git", Base: "main", Pos: i + 1})
	}
	for i := range rows {
		name := fmt.Sprintf("r%05d", i)
		s.roster.Workers[name] = roster.Worker{Name: name, Width: 4, Tiers: store.Tiers}
		ws = append(ws, store.PutRow{Name: name, Session: "hook-" + name, Role: "worker"}, store.SetRowState{Row: name, State: "up"})
		if away {
			s.away[name] = struct{}{}
		}
	}
	s.must("seed", ws...)
	ws = nil
	for i := range cards {
		id := fmt.Sprintf("p%05d", i)
		ws = append(ws, store.PutCard{ID: id, Kind: "producer", Stream: fmt.Sprintf("s%02d", i%streams), Tier: "pro", Brief: "do " + id, Paths: []string{fmt.Sprintf("a/%05d", i)}, Score: i})
		if len(ws) == 500 {
			s.must("seed", ws...)
			ws = nil
		}
	}
	if len(ws) > 0 {
		s.must("seed", ws...)
	}
	s.quiesce(b)
	return s
}

// quiesce ticks until a tick plans nothing, each row acknowledging what it
// was dealt after every tick, so no deal is nudged or taken back.
func (s *sim) quiesce(b *testing.B) {
	b.Helper()
	for range 2000 {
		p := s.tick()
		s.ackAll()
		if len(p.Ops) == 0 && len(p.Edges) == 0 && len(s.inbox) == 0 {
			s.fold()
			return
		}
	}
	b.Fatal("the world did not settle in 2000 ticks")
}

// ackAll acknowledges every unacknowledged working consumer in one op.
func (s *sim) ackAll() {
	snap := s.snap()
	var ws []store.Write
	for _, c := range s.unacked() {
		ws = append(ws, store.SetCursor{Name: AckCursor(snap.Epoch, c), Seq: max(snap.Seq, 1)})
	}
	if len(ws) > 0 {
		s.must("take", ws...)
	}
}

// recorder answers as benchAsk does and keeps the questions.
type recorder struct{ q Questions }

func (r *recorder) Ask(q Questions) Answers { r.q = q; return benchAsk{}.Ask(q) }

// everyAsk is the answers the built-in policy gives the tick's questions:
// every candidate named for each rows question (rows() lists them all, those
// with room first), each row's width as its work, the engine's order,
// reads, landing, limits and timeouts, a bench model per tier. They are
// built once, outside the measure.
func everyAsk(v *View, in Inputs) scripted {
	r := &recorder{}
	Tick(v, in, r)
	lim, tmo := Engine.Limits, Engine.Timeouts
	a := Answers{Version: 1, Models: benchModels(), Rows: map[string][]string{}, Width: map[string]WidthAnswer{}, Order: map[string][]any{},
		Reads: map[string]int{}, Landing: map[string]LandingAnswer{}, Limits: &lim, Timeouts: &tmo}
	for _, c := range r.q.Order {
		a.Order[c.ID] = []any{1, c.Score}
	}
	for _, c := range r.q.Reads {
		a.Reads[c.ID] = Engine.Reads
	}
	for _, l := range r.q.Landing {
		a.Landing[l.Name] = Engine.Landing
	}
	for _, q := range r.q.Rows {
		names := make([]string, len(q.Candidates))
		for i, c := range q.Candidates {
			names[i] = c.Name
		}
		a.Rows[q.Card.ID] = names
	}
	for _, w := range r.q.Width {
		a.Width[w.Name] = WidthAnswer{Work: w.Width}
	}
	return scripted{a}
}

// benchTick measures one tick's plan on s with each asker: the engine's
// defaults (no rows answer) and the built-in's answers (every candidate).
func benchTick(b *testing.B, s *sim) {
	in := s.inputs()
	for _, ask := range []struct {
		name string
		a    Asker
	}{{"engine", benchAsk{}}, {"every", everyAsk(s.v, in)}} {
		b.Run("ask="+ask.name, func(b *testing.B) {
			var ops int
			b.ReportAllocs()
			for b.Loop() {
				ops = len(Tick(s.v, in, ask.a).Ops)
			}
			b.ReportMetric(float64(ops), "ops/tick")
		})
	}
}

// BenchmarkTickIdle is one tick's plan on a settled world: every row full
// or every card dealt and acknowledged, so the tick has nothing to do.
func BenchmarkTickIdle(b *testing.B) {
	benchSizes(b, func(b *testing.B, rows, cards int) {
		benchTick(b, benchWorld(b, rows, cards, false))
	})
}

// BenchmarkTickDeal is one tick's plan with every card ready and every row
// open and empty: the deal at the budget's width.
func BenchmarkTickDeal(b *testing.B) {
	benchSizes(b, func(b *testing.B, rows, cards int) {
		s := benchWorld(b, rows, cards, true)
		s.away = set{}
		benchTick(b, s)
	})
}

// BenchmarkFold is the view's fold of one event, a deal of one consumer,
// on a settled world: the cost of one unit of own work.
func BenchmarkFold(b *testing.B) {
	benchSizes(b, func(b *testing.B, rows, cards int) {
		s := benchWorld(b, rows, cards, false)
		es, err := s.f.Events(b.Context(), 0, 0)
		if err != nil {
			b.Fatal(err)
		}
		var deal []store.Event
		for _, e := range es {
			if e.Accepted && e.After != nil && len(e.After.Cards) > 0 && e.Kind == "tick" {
				deal = []store.Event{e}
			}
		}
		if deal == nil {
			b.Fatal("no tick event to fold")
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			s.v.Fold(deal)
		}
	})
}
