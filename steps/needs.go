package steps

import (
	"slices"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// needs is the sentinels and needs step (LAYER-3 "Sentinels and needs"; the
// model's ResolveFold; NeverOverlapStarted, ReadyNeverFlipsWithLoad). A
// failed need takes the closure of its dependants in one group (LESSONS
// 2.4). Then, over the waiting producers whose chance to resolve moved since
// the last tick (View.resolvePending: guarantee 11, never a scan of every
// waiting producer), in id order: a sentinel whose needs landed is Reached
// with a landed-sentinel judgement; a producer whose needs landed, whose
// stream is not held and whose paths overlap no producer of its stream that
// is ready or started (those resolved this tick counted) is Resolved. Room
// on a row is never a condition (LESSONS 3.2). A candidate the budget cut
// stays pending; one that cannot resolve leaves the set until an event
// pends it again.
func (t *tick) needs() {
	t.drops()
	keep := set{}
	for _, p := range sorted(t.v.resolvePending) {
		c, ok := t.v.Cards[p]
		if !ok || c.State != "waiting" || !t.free(p) || !t.needsLanded(c) {
			continue
		}
		if c.Kind == "sentinel" {
			if t.overBudget() {
				keep[p] = struct{}{}
			} else if g := t.group("reached", p, t.producerMove(p, card.Reached{})); g != nil {
				g.judge = t.riding("landed-sentinel", p, "sentinel "+p+" reached: every card it waited for has landed")
			}
			continue
		}
		if t.v.Streams[c.Stream].Held || len(t.overlap(c)) > 0 {
			continue
		}
		if t.overBudget() {
			keep[p] = struct{}{}
			continue
		}
		t.resolved[p] = struct{}{} // the overlap tally counts it whether or not its op is accepted
		for _, path := range c.Paths {
			add(t.resolvedP, c.Stream+"\x00"+path, p)
		}
		t.group("resolve", p, t.producerMove(p, card.Resolve{}))
	}
	t.v.resolvePending = keep
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
	for _, path := range c.Paths {
		for q := range t.resolvedP[c.Stream+"\x00"+path] {
			if !slices.Contains(out, q) {
				out = append(out, q)
			}
		}
	}
	slices.Sort(out)
	return out
}

// drops fails every non-final dependant of a need that failed since the last
// tick (View.dropPending), the whole closure as one group: each dependant's
// live consumers withdrawn, then NeedFailed (in this op for a dependant whose
// need fails in the same group). A drop the budget cut stays pending.
func (t *tick) drops() {
	keep := set{}
	for _, n := range sorted(t.v.dropPending) {
		if t.overBudget() {
			keep[n] = struct{}{}
			continue
		}
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
	t.v.dropPending = keep
}
