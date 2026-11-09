package steps

import (
	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// needs is the sentinels and needs step (LAYER-3 "Sentinels and needs"; the
// model's resolve set; ReadyNeverFlipsWithLoad). A failed need takes the
// closure of its dependants in one group (LESSONS 2.4). Then, in id order: a
// waiting sentinel whose needs landed is Reached with a landed-sentinel
// judgement; a waiting producer whose needs landed, whose stream is not held
// and who is not held itself (card.HeldReason: a hold or a missing route) is
// Resolved. Only a NEEDS orders starts: two cards that name the same file
// both start, and their landing (the group rung, then a land judgement)
// stitches them. Room on a row is never a condition (LESSONS 3.2). It reads the view's state every tick, never
// the land step's events, so a need landed by a store op or a restore
// resolves too (section 64b; the model's ResolveFromState).
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
		if t.v.Streams[c.Stream].Held || card.HeldReason(c.Reason) { // a held card waits: the deal never takes it
			continue
		}
		t.resolved[p] = struct{}{} // whether or not the budget admits it
		t.group("resolve", p, t.producerMove(p, card.Resolve{}))
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
