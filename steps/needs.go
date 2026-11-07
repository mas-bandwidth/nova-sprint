package steps

import (
	"slices"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// needs is the sentinels and needs step (LAYER-3 "Sentinels and needs"; the
// model's ResolveFold; NeverOverlapStarted, ReadyNeverFlipsWithLoad). A
// failed need takes the closure of its dependants in one group (LESSONS
// 2.4). Then, in id order: a waiting sentinel whose needs landed is Reached
// with a landed-sentinel judgement; a waiting producer whose needs landed,
// whose stream is not held and whose paths overlap no producer of its
// stream that is ready or started (those resolved this tick counted) is
// Resolved. Room on a row is never a condition (LESSONS 3.2).
func (t *tick) needs() {
	t.drops()
	for _, p := range t.v.Producers("waiting") {
		c := t.v.Cards[p]
		if !t.free(p) || !t.needsLanded(c) {
			continue
		}
		if c.Kind == "sentinel" {
			if g := t.group("reached", p, t.producerMove(p, card.Reached{})); g != nil {
				g.judge = t.riding("landed-sentinel", p, "sentinel "+p+" reached: every card it waited for has landed")
			}
			continue
		}
		if t.v.Streams[c.Stream].Held || len(t.overlap(c)) > 0 {
			continue
		}
		if t.group("resolve", p, t.producerMove(p, card.Resolve{})) != nil {
			t.resolved[p] = struct{}{}
		}
	}
}

func (t *tick) needsLanded(c store.Card) bool {
	for _, n := range c.Needs {
		if x, ok := t.v.Cards[n]; !ok || x.State != "landed" {
			return false
		}
	}
	return true
}

// overlap is the producers of c's stream, ready, started or resolved this
// tick, sharing a path with c (LANDING step 2).
func (t *tick) overlap(c store.Card) []string {
	out := t.v.Overlaps(c)
	for q := range t.resolved {
		if x := t.v.Cards[q]; x.Stream == c.Stream && shares(x.Paths, c.Paths) {
			out = append(out, q)
		}
	}
	slices.Sort(out)
	return out
}

func shares(a, b []string) bool {
	for _, p := range a {
		if slices.Contains(b, p) {
			return true
		}
	}
	return false
}

// drops fails every non-final dependant of a failed need, the whole closure
// as one group: each dependant's live consumers withdrawn, then NeedFailed
// (in this op for a dependant whose need fails in the same group).
func (t *tick) drops() {
	for _, n := range t.v.Producers("fail") {
		var moves []move
		about := ""
		seen := set{n: {}}
		queue := []string{n}
		for len(queue) > 0 {
			need := queue[0]
			queue = queue[1:]
			for _, d := range t.v.Dependants(need) {
				c := t.v.Cards[d]
				if _, s := seen[d]; s || c.State == "fail" || c.State == "landed" || !t.free(d) {
					continue
				}
				seen[d] = struct{}{}
				for id := range t.v.consOf[d] {
					if isLive(t.v.Cards[id]) {
						moves = append(moves, t.withdraw(id, "need "+need+" failed"))
					}
				}
				moves = append(moves, t.producerMove(d, card.NeedFailed{Need: need, InThisOp: need != n}))
				queue = append(queue, d)
				if about == "" {
					about = d
				}
			}
		}
		if len(moves) > 0 {
			t.group("drop", about, moves...)
		}
	}
}
