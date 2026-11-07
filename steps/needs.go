package steps

import (
	"slices"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// needs is the sentinels and needs step (LAYER-3 "Sentinels and needs"; the
// model's ResolveFold; NeverOverlapStarted, ReadyNeverFlipsWithLoad). A
// failed need takes the closure of its dependants in one group. Then, in id order: a waiting sentinel whose needs landed is Reached
// with a landed-sentinel judgement; a waiting producer whose needs landed,
// whose stream is not held and who names no file a producer of its stream
// that is ready or started names (those resolved this tick counted) is
// Resolved; one held behind such a producer opens an overlap judgement
// saying "held behind <card> on <path>". Room on a row is never
// a condition. It reads the view's state every tick, never
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
		if t.v.Streams[c.Stream].Held {
			continue
		}
		if why := t.heldBehind(c); why != "" {
			t.judge("overlap", p, why)
			continue
		}
		t.resolved[p] = struct{}{} // the overlap tally counts it whether or not the budget admits it
		for _, path := range c.Paths {
			if Concrete(path) {
				add(t.resolvedP, c.Stream+"\x00"+path, p)
			}
		}
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

// heldBehind is why c is held behind another producer of its stream, ready,
// started or resolved this tick, that names a file c names (LANDING step
// 2): "held behind <card> on <path>" for the least such card by id and the
// first of c's files it names; "" when none is.
func (t *tick) heldBehind(c store.Card) string {
	by, on := leastHolder(c, func(path string) []set {
		return []set{t.v.paths[c.Stream][path], t.resolvedP[c.Stream+"\x00"+path]}
	})
	if by == "" {
		return ""
	}
	return "held behind " + by + " on " + on
}

// leastHolder is the least producer by id, other than c, in the sets
// holding one of c's files (Concrete), and the first of c's files it holds;
// "" when none is. The needs step and Waits share it, so the board, check
// and the overlap judgement name the same holder.
func leastHolder(c store.Card, holding func(path string) []set) (by, on string) {
	for _, path := range c.Paths {
		if !Concrete(path) {
			continue
		}
		for _, holders := range holding(path) {
			for q := range holders {
				if q != c.ID && (by == "" || q < by) {
					by, on = q, path
				}
			}
		}
	}
	return by, on
}

// shares says whether two path lists name a common file (Concrete).
func shares(a, b []string) bool {
	for _, p := range a {
		if Concrete(p) && slices.Contains(b, p) {
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
