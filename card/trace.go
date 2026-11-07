package card

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Trace validation (tla/README.md, "Trace validation"): a walk of the card
// machine on the store records, after every accepted op, the move and the
// store's cards, rows and stream projected onto the variables of
// tla/CardMachine.tla, and tla/CardMachineTrace.tla checks the recording is
// a behaviour of the model. TraceState is the refinement mapping.
//
// Every variable of CardMachine.tla is accounted for:
//
//   - ps, cs, rs, held: ModelState, below.
//   - last (the step monitor): not projected; the model computes it.
//   - env (the environment budget, a model bound): not projected; the trace
//     module raises it on every environment move and never bounds it.
//
// What the code has that the model does not (the abstractions):
//
//   - A consumer's id: the model names consumers by slot, <<p, i>> for p's
//     i-th cut. A slot is read off the id and columns (traceSlots): p's
//     consumers ordered by attempt, the work consumer before the reads of
//     its attempt, reads by review and then by their index.
//   - Heads: commit names in the code, 0..MaxHead in the model, 0 being the
//     base and "no head". TraceState maps them through a table the walk
//     gives (heads[0] is the base); a head not in it is an error.
//   - finding: text in the code, "-" or "fix" in the model (whether a read
//     asked for a fix): any non-empty finding is "fix".
//   - Score and priority are carried as they are (any number, any level);
//     the model's bounds on them (0..1; normal or blocker) are on its
//     environment's choices, not on its moves.
//   - flaps: carried as the count; the model stops Gone at 1, a bound the
//     trace module lifts (CardMachineTrace.tla, GoneAt).
//   - Left out because no rule of the model reads them (as the model says):
//     branch, base_sha, reason, brief, prose, paths, done_when, cost, priced,
//     pr, tier (every card and row of a walk is pro), gen, the row's tiers,
//     kind, session and labels, the stream's name, repo and position.
//   - Producers live in one stream (the walk's); TraceState projects that
//     stream's hold onto held.

// ModelState is the card machine's state in CardMachine.tla's terms.
type ModelState struct {
	Ps   map[string]ModelProd
	Cs   map[Slot]ModelCons
	Rs   map[string]ModelRow
	Held bool
}

// Slot is a consumer's name in the model: its producer and cut number.
type Slot struct {
	P string
	I int
}

// ModelProd is CardMachine.tla's producer record (a sentinel's too).
type ModelProd struct {
	St, Author, Prio, Finding string
	Att, Head, Bounces, Score int
	Review, Ncut              int
	Needs, After              []string
	Red                       bool
}

// ModelCons is CardMachine.tla's consumer record.
type ModelCons struct {
	St, Kind, Row, Res, Author, Finding string
	Att, Head, Review, Prior            int
}

// ModelRow is CardMachine.tla's row record.
type ModelRow struct {
	St           string
	Flaps, Width int
	Held         bool
}

// TraceState is the refinement mapping: the snapshot's cards, rows and
// stream projected onto CardMachine.tla's ps, cs, rs and held. heads is the
// head table: heads[0] is the base (head 0), heads[i] is head i.
// widths is each row's width, read from the roster (round 22: the store
// keeps none).
func TraceState(s store.Snapshot, stream string, heads []string, widths map[string]int) (ModelState, error) {
	m := ModelState{Ps: map[string]ModelProd{}, Cs: map[Slot]ModelCons{}, Rs: map[string]ModelRow{}, Held: s.Streams[stream].Held}
	head := func(h string) (int, error) {
		if h == "" {
			return 0, nil
		}
		if i := slices.Index(heads, h); i >= 0 {
			return i, nil
		}
		return 0, fmt.Errorf("card: trace: head %q is not in the head table", h)
	}
	slots := traceSlots(s)
	for _, c := range s.Cards {
		var err error
		if consumer(c.Kind) {
			m.Cs[slots[c.ID]], err = traceCons(c, head)
		} else {
			m.Ps[c.ID], err = traceProd(c, len(traceConsumers(s, c.ID)), head)
		}
		if err != nil {
			return ModelState{}, err
		}
	}
	for n, r := range s.Rows {
		m.Rs[n] = ModelRow{St: r.State, Flaps: r.Flaps, Width: widths[n], Held: r.Held}
	}
	return m, nil
}

func traceProd(c store.Card, ncut int, head func(string) (int, error)) (ModelProd, error) {
	h, err := head(c.Head)
	return ModelProd{St: c.State, Att: c.Attempt, Head: h, Author: cmp.Or(c.Author, "none"), Bounces: c.Bounces, Score: c.Score,
		Prio: c.Priority, Needs: slices.Clone(c.Needs), After: slices.Clone(c.After), Review: c.Review, Ncut: ncut,
		Finding: fixOrNone(c.Finding), Red: c.Red}, err
}

func traceCons(c store.Card, head func(string) (int, error)) (ModelCons, error) {
	h, err := head(c.Head)
	if err != nil {
		return ModelCons{}, err
	}
	prior, err := head(c.Prior)
	return ModelCons{St: c.State, Kind: c.Kind, Row: c.Holder, Att: c.Attempt, Head: h, Res: cmp.Or(c.Result, "-"),
		Author: cmp.Or(c.Author, "none"), Review: c.Review, Prior: prior, Finding: fixOrNone(c.Finding)}, err
}

func fixOrNone(f string) string {
	if f == "" {
		return "-"
	}
	return "fix"
}

// traceConsumers is p's consumers in cut order: by attempt, the work
// consumer of an attempt before its reads, reads by review and then by the
// index in their id (<p>.r<review>.<n>).
func traceConsumers(s store.Snapshot, p string) []store.Card {
	var cs []store.Card
	for _, c := range s.Cards {
		if c.Of == p {
			cs = append(cs, c)
		}
	}
	readN := func(c store.Card) int {
		var n int
		_, _ = fmt.Sscanf(c.ID[strings.LastIndex(c.ID, ".")+1:], "%d", &n) // a work id has no index: 0
		return n
	}
	isRead := func(c store.Card) int {
		if c.Kind == "read" {
			return 1
		}
		return 0
	}
	slices.SortFunc(cs, func(a, b store.Card) int {
		return cmp.Or(cmp.Compare(a.Attempt, b.Attempt), cmp.Compare(isRead(a), isRead(b)),
			cmp.Compare(a.Review, b.Review), cmp.Compare(readN(a), readN(b)))
	})
	return cs
}

// traceSlots is every consumer's slot.
func traceSlots(s store.Snapshot) map[string]Slot {
	out := map[string]Slot{}
	for _, p := range s.Cards {
		if consumer(p.Kind) {
			continue
		}
		for i, c := range traceConsumers(s, p.ID) {
			out[c.ID] = Slot{P: p.ID, I: i + 1}
		}
	}
	return out
}

// TraceSlot is consumer id's slot in s, and whether it has one.
func TraceSlot(s store.Snapshot, id string) (Slot, bool) {
	sl, ok := traceSlots(s)[id]
	return sl, ok
}
