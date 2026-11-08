package policy

// Benchmarks of the tick's answer over fleet size and card count: in this
// process on a fake clock (unit tier), and through the evaluator child as
// layer 6 runs it (functional tier: a process). Run them on a bench:
//
//	go test -run '^$' -bench . -benchmem ./modules/policy/

import (
	"context"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

var (
	benchRows  = []int{10, 100, 1000, 10000}
	benchCards = []int{100, 1000, 10000}
)

func benchSizes(b *testing.B, f func(b *testing.B, rows, cards int)) {
	for _, rows := range benchRows {
		for _, cards := range benchCards {
			b.Run(fmt.Sprintf("rows=%d/cards=%d", rows, cards), func(b *testing.B) { f(b, rows, cards) })
		}
	}
}

// benchTick is a settled sprint: rows rows of width 4 serving every tier,
// cards producers in streams of 100 (at most 20), the first four per row
// working (a work consumer each), the rest ready; the active version is the
// built-in; the questions are the tick's (every ready producer's order and
// work rows, every row's width).
func benchTick(rows, cards int) (Versions, store.Snapshot, roster.Roster, Questions) {
	snap := store.Snapshot{Seq: 1, Epoch: 1, Policy: 1, Streams: map[string]store.Stream{}, Cards: map[string]store.Card{}, Rows: map[string]store.Row{},
		Judgements: map[string]store.Judgement{}, Cursors: map[string]int64{}}
	ro := roster.Roster{Workers: map[string]roster.Worker{}, Kin: map[string]roster.Kin{}}
	streams := max(1, min(20, cards/100))
	for i := range streams {
		name := fmt.Sprintf("s%02d", i)
		snap.Streams[name] = store.Stream{Name: name, Repo: "git@example.com:r.git", Base: "main", Pos: i + 1, Mode: "auto"}
	}
	for i := range rows {
		name := fmt.Sprintf("r%05d", i)
		snap.Rows[name] = store.Row{Name: name, State: "up", Session: "hook-" + name, Role: "worker"}
		ro.Workers[name] = roster.Worker{Name: name, Width: 4, Tiers: store.Tiers, Harness: roster.WorkerHarness}
	}
	var q Questions
	for i := range cards {
		id := fmt.Sprintf("p%05d", i)
		p := store.Card{ID: id, Kind: "producer", Stream: fmt.Sprintf("s%02d", i%streams), State: "ready", Tier: "pro", Paths: []string{fmt.Sprintf("a/%05d", i)}, Score: i, Gen: 2}
		if i < rows*4 {
			p.State, p.Attempt = "working", 1
			w := store.Card{ID: id + ".w1", Kind: "work", Of: id, Stream: p.Stream, State: "working", Tier: "pro", Holder: fmt.Sprintf("r%05d", i/4), Attempt: 1, Gen: 1, Model: "test/pro"}
			snap.Cards[w.ID] = w
		} else {
			q.Order = append(q.Order, id)
			q.Rows = append(q.Rows, RowsQuestion{Card: id, Kind: KindWork})
		}
		snap.Cards[id] = p
	}
	vs := Versions{Active: &store.PolicyVersion{Version: 1, Body: DefaultBody(), Active: true}, Seq: snap.Seq}
	return vs, snap, ro, q
}

func requireAnswered(b *testing.B, e *Engine, vs Versions, snap store.Snapshot, ro roster.Roster, q Questions) {
	if a, fs := e.AnswerContext(context.Background(), vs, snap, ro, q); a.Version != 1 || len(fs) > 0 {
		b.Fatalf("answered by v%d, failures %v", a.Version, fs)
	}
}

// BenchmarkAnswerCold is a tick's answer on an engine that has compiled the
// version but never seen these inputs: every question evaluated.
func BenchmarkAnswerCold(b *testing.B) {
	benchSizes(b, func(b *testing.B, rows, cards int) {
		vs, snap, ro, q := benchTick(rows, cards)
		e := testEngine()
		e.budget = 1 << 62 // the bench measures the work, never the deadline
		requireAnswered(b, e, vs, snap, ro, Questions{})
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			b.StopTimer()
			for _, p := range e.progs {
				p.answers = nil // the last tick's answers dropped: none is a cache hit
			}
			b.StartTimer()
			requireAnswered(b, e, vs, snap, ro, q)
		}
	})
}

// BenchmarkAnswerIdle is the same tick asked again on the same inputs, as
// an idle tick asks it: every answer from the last tick's cache.
func BenchmarkAnswerIdle(b *testing.B) {
	benchSizes(b, func(b *testing.B, rows, cards int) {
		vs, snap, ro, q := benchTick(rows, cards)
		e := testEngine()
		e.budget = 1 << 62
		requireAnswered(b, e, vs, snap, ro, q)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			requireAnswered(b, e, vs, snap, ro, q)
		}
	})
}

// BenchmarkFunctionalAnswerIdle is BenchmarkAnswerIdle through the
// evaluator child, as layer 6 asks every tick: the snapshot, roster and
// questions sent and the answers returned over the pipe.
func BenchmarkFunctionalAnswerIdle(b *testing.B) {
	benchSizes(b, func(b *testing.B, rows, cards int) {
		vs, snap, ro, q := benchTick(rows, cards)
		e := NewEngine(RealClock{})
		requireAnswered(b, e, vs, snap, ro, q)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			requireAnswered(b, e, vs, snap, ro, q)
		}
	})
}
