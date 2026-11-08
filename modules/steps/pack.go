package steps

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Actor is the op actor of every tick op.
const Actor = "sprint"

// pack orders the groups, keeps the budget, packs groups into ops of up to
// pack groups, names each op by a hash of its body and carries the inbox
// items the budget left, in arrival order (tick steps 3 to 6; BudgetKept,
// CarryFIFO).
func (t *tick) pack() Plan {
	ordered := t.order()
	admitted := t.admit(ordered)
	var plan Plan
	used := set{}
	for _, op := range t.ops(admitted) {
		id := t.opID(op)
		for _, g := range op {
			if g.key.Item != "" {
				used[g.key.Item] = struct{}{}
			}
			for _, e := range g.edges {
				e.After = id
				plan.Edges = append(plan.Edges, e)
			}
		}
		plan.Ops = append(plan.Ops, t.storeOp(id, op))
	}
	for _, g := range admitted {
		if g.none && g.key.Item != "" {
			used[g.key.Item] = struct{}{}
		}
		if g.answer != nil {
			plan.Answers = append(plan.Answers, *g.answer)
		}
	}
	for _, it := range t.in.Inbox {
		if _, u := used[it.ID]; !u {
			plan.Carry = append(plan.Carry, it)
		}
	}
	plan.Edges = append(plan.Edges, t.edges...)
	if jop, ok := t.judgementOp(); ok {
		plan.Ops = append(plan.Ops, jop)
	}
	return plan
}

// order is the groups with the blocker groups first (the preemptions, then
// the blockers' plain starts), the rest as the steps planned them: inbox
// items in arrival order, then needs, holds, limits, fix, the deal by rung.
func (t *tick) order() []group {
	out := make([]group, 0, len(t.groups))
	for _, g := range t.groups {
		if g.blocker {
			out = append(out, g)
		}
	}
	for _, g := range t.groups {
		if !g.blocker {
			out = append(out, g)
		}
	}
	return out
}

// admit keeps the budget (tick step 3; the model's Admit): a blocker group
// always; the first non-blocker group always, so a group larger than the
// budget is not starved; then each while the cards touched stay within the
// budget, stopping at the first that does not fit, so the inbox is consumed
// as a prefix.
func (t *tick) admit(gs []group) []group {
	var out []group
	touched, nonBlockers, stop := 0, 0, false
	for _, g := range gs {
		switch {
		case g.blocker:
			out = append(out, g)
			touched += g.size()
		case !stop && (nonBlockers == 0 || touched+g.size() <= t.lim.Budget):
			out = append(out, g)
			touched += g.size()
			nonBlockers++
		default:
			stop = true
		}
	}
	return out
}

// ops packs groups into ops of up to pack groups, in order (tick step 4);
// a group touching a card of a group of the last tick's refused ops is an
// op of its own (step 7: a refused op is split by group). An inbox item
// consumed with no move is in no op.
func (t *tick) ops(gs []group) [][]group {
	split := set{}
	for _, r := range t.in.Refused {
		if len(r.Groups) > 1 {
			for _, k := range r.Groups {
				for _, c := range k.Cards {
					split[c] = struct{}{}
				}
			}
		}
	}
	var out [][]group
	var cur []group
	for _, g := range gs {
		if g.none {
			continue
		}
		if slices.ContainsFunc(g.key.Cards, func(c string) bool { _, s := split[c]; return s }) {
			if len(cur) > 0 {
				out = append(out, cur)
				cur = nil
			}
			out = append(out, []group{g})
			continue
		}
		cur = append(cur, g)
		if len(cur) >= t.lim.Pack {
			out = append(out, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// storeOp is one op of the groups: the plans' conditions (each once) and
// writes in order, its groups' keys in the note.
func (t *tick) storeOp(id string, gs []group) store.Op {
	op := store.Op{ID: id, Epoch: t.v.Epoch, Actor: Actor, Kind: "tick", At: t.in.Now}
	keys := make([]GroupKey, 0, len(gs))
	for _, g := range gs {
		for _, e := range g.plan.Expect {
			if !slices.Contains(op.Expect, e) {
				op.Expect = append(op.Expect, e)
			}
		}
		op.Writes = append(op.Writes, g.plan.Writes...)
		op.Writes = append(op.Writes, g.judge...)
		keys = append(keys, g.key)
	}
	op.Note = noteOf(keys)
	return op
}

// opID names an op t<epoch>.<seq>.<hash of its body>: one view, one id,
// applied once (tick step 6).
func (t *tick) opID(gs []group) string {
	var es []store.Expect
	var ws []store.Write
	for _, g := range gs {
		es = append(es, g.plan.Expect...)
		ws = append(ws, g.plan.Writes...)
		ws = append(ws, g.judge...)
	}
	return fmt.Sprintf("t%d.%d.%s", t.v.Epoch, t.v.Seq, hashBody(es, ws))
}

// hashBody is the hash of an op body: its conditions and writes in order.
func hashBody(es []store.Expect, ws []store.Write) string {
	h := sha256.New()
	for _, e := range es {
		fmt.Fprintf(h, "%#v|", e)
	}
	for _, w := range ws {
		fmt.Fprintf(h, "%#v|", w)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func itoa(n int) string { return fmt.Sprint(n) }
