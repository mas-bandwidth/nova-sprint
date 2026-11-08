package card

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// records are the cards, rows and streams a move read. Merge folds the
// writes of the plans ahead onto a copy and runs the later move again, so a
// merged plan is checked the way each move would be checked on its own.
type records struct {
	cards   map[string]Card
	rows    map[string]Row
	streams map[string]Stream
}

// rerun is that move. Nil on a hand-built plan: Merge keeps its writes.
type rerun func(records) (Plan, *Refusal)

// Merge applies plans as one op, in order. Nested merges flatten to the
// moves first. Every move the machine built, the first included, is planned
// again against the records read before any write of this op, with each
// earlier move's writes folded in. The last write of a field is the one
// emitted. The merged plan's seen is that pre-write read, so a later merge
// does not see this op's own writes. A hand-built plan is folded onto those
// records, or refused when it writes a card no plan read. A refusal names
// the plans and the condition. Two AskReads of one head still collide.
func Merge(plans ...Plan) (Plan, *Refusal) {
	flat := flatten(nil, plans)
	if !needsReplay(flat) {
		return mergeConcat(flat)
	}
	return compose(flat, records{})
}

// flatten is the ordered moves of plans. A merge stores its moves in parts.
func flatten(dst, plans []Plan) []Plan {
	for _, p := range plans {
		if len(p.parts) > 0 {
			dst = flatten(dst, p.parts)
			continue
		}
		dst = append(dst, p)
	}
	return dst
}

func needsReplay(plans []Plan) bool {
	for _, p := range plans {
		if p.again != nil {
			return true
		}
	}
	return false
}

func mergeConcat(plans []Plan) (Plan, *Refusal) {
	var m Plan
	labels := planLabels(plans)
	for _, p := range plans {
		m.Expect = append(m.Expect, p.Expect...)
		m.Writes = append(m.Writes, p.Writes...)
	}
	if r := dupWrites(m.Writes); r != nil {
		return Plan{}, no("merge of %s refused: %s", andList(labels), r.Reason)
	}
	return checked(m)
}

// compose plans each move against the fold. base is what an outer merge
// already wrote; the seen stored on the result is the pre-write read.
func compose(plans []Plan, base records) (Plan, *Refusal) {
	labels := planLabels(plans)
	// Two plans that each create the same card refuse before the re-run. A
	// re-run would allocate the next id and hide it.
	if r := dupNewCards(plans); r != nil {
		return namedMerge(labels, 0, r)
	}
	read := readsOf(base, plans)
	cur := cloneRecords(base)
	out, runs, r := foldPlans(plans, labels, read, cur)
	if r != nil {
		return Plan{replays: runs}, r
	}
	if r := dupWrites(out.Writes); r != nil {
		return namedMerge(labels, runs, r)
	}
	if r := preemptGuard(plans); r != nil {
		return namedMerge(labels, runs, r)
	}
	done, r := checked(out)
	if r != nil {
		return namedMerge(labels, runs, r)
	}
	return sealCompose(done, plans, labels, read, runs), nil
}

func namedMerge(labels []string, runs int, r *Refusal) (Plan, *Refusal) {
	return Plan{replays: runs}, no("merge of %s refused: %s", andList(labels), r.Reason)
}

// foldPlans re-runs every machine move against the fold so far. A hand-built
// move is kept. Expects stay the ones read off the snapshot.
func foldPlans(plans []Plan, labels []string, read, cur records) (Plan, int, *Refusal) {
	var out Plan
	written := map[[3]string]bool{}
	created := map[string]bool{}
	runs := 0
	for i, p := range plans {
		unionMissing(&cur, p.seen)
		if r := leafGate(p, read, cur, created); r != nil {
			return refusedLeaf(labels, i, runs, r)
		}
		next, ran, r := rerunPlan(p, cur)
		runs += ran
		if r != nil {
			return refusedLeaf(labels, i, runs, r)
		}
		out.Expect = append(out.Expect, keepExpects(p.Expect, written, created)...)
		out.Expect = append(out.Expect, addedExpects(next.Expect, p.Expect, written, created)...)
		// A move into working is the store's width check. Covering it with a
		// later state write would hide a take the row has no room for, so
		// that pair is refused. Every other field, state included, keeps its
		// last write: Merge(Resolve, HoldStream) parks.
		if r := coveredWorking(out.Writes, next.Writes); r != nil {
			return refusedLeaf(labels, i, runs, r)
		}
		out.Writes = absorb(out.Writes, next.Writes)
		noteFields(written, next.Writes)
		noteCreated(created, next.Writes)
		foldOnto(&cur, read, next.Writes)
	}
	out.Writes = quietWrites(out.Writes, read)
	return out, runs, nil
}

// quietWrites drops a write that puts a field back to the value the pre-op
// snapshot already holds. The store refuses a write that changes nothing,
// and it checks each write against the record the writes ahead of it left,
// which starts at that snapshot. Resolve then Hold parks p2 back to waiting;
// emitting that state write is "card p2 is already waiting".
func quietWrites(ws []store.Write, read records) []store.Write {
	cur := cloneRecords(read)
	var out []store.Write
	for _, w := range ws {
		if idleWrite(w, cur) {
			continue
		}
		out = append(out, w)
		foldWrite(cur, w)
	}
	return out
}

func idleWrite(w store.Write, cur records) bool {
	switch x := w.(type) {
	case store.SetStreamHeld:
		st, ok := cur.streams[x.Stream]
		return ok && st.Held == x.Held
	case store.SetRowState:
		r, ok := cur.rows[x.Row]
		return ok && r.State == x.State
	case store.SetRowFlaps:
		r, ok := cur.rows[x.Row]
		return ok && r.Flaps == x.Flaps
	case store.SetRowHeld:
		r, ok := cur.rows[x.Row]
		return ok && r.Held == x.Held
	default:
		return idleCard(w, cur)
	}
}

func idleCard(w store.Write, cur records) bool {
	id := writeCardID(w)
	c, ok := cur.cards[id]
	if id == "" || !ok {
		return false
	}
	switch x := w.(type) {
	case store.SetCardReview:
		return false
	case store.SetCardState:
		return c.State == x.State
	case store.SetCardReason:
		return c.Reason == x.Reason
	case store.SetCardHead:
		return c.Head == x.Head
	case store.SetCardFinding:
		return c.Finding == x.Finding
	case store.SetCardAuthor:
		return c.Author == x.Author
	case store.SetCardBranch:
		return c.Branch == x.Branch
	case store.SetCardBaseSHA:
		return c.BaseSHA == x.SHA
	case store.SetCardPriorHead:
		return c.PriorHead == x.Head
	case store.SetCardResult:
		return c.Result == x.Result
	case store.SetCardProse:
		return c.Prose == x.Prose
	case store.SetCardRed:
		return c.Red == x.Red
	case store.SetCardPriority:
		return normPriority(c.Priority) == normPriority(x.Priority)
	case store.SetCardScore:
		return c.Score == x.Score
	case store.SetCardAttempt:
		return c.Attempt == x.Attempt
	case store.SetCardBounces:
		return c.Bounces == x.Bounces
	case store.SetCardBounceBase:
		return c.BounceBase == x.Base
	case store.SetCardCost:
		return c.Cost == x.Cost && c.Priced == x.Priced
	case store.SetCardUsage:
		return c.Usage == x.Usage
	default:
		return idleLists(w, c)
	}
}

func idleLists(w store.Write, c Card) bool {
	switch x := w.(type) {
	case store.SetCardNeeds:
		return sameSet(c.Needs, x.Needs)
	case store.SetCardAfter:
		return sameSet(c.After, x.After)
	case store.SetCardPaths:
		return slices.Equal(c.Paths, x.Paths)
	case store.SetCardDoneWhen:
		return slices.Equal(c.DoneWhen, x.DoneWhen)
	default:
		return false
	}
}

func normPriority(p string) string {
	if p == "" {
		return "normal"
	}
	return p
}

func sameSet(a, b []string) bool {
	aa, bb := slices.Clone(a), slices.Clone(b)
	slices.Sort(aa)
	slices.Sort(bb)
	return slices.Equal(slices.Compact(aa), slices.Compact(bb))
}

func refusedLeaf(labels []string, i, runs int, r *Refusal) (Plan, int, *Refusal) {
	return Plan{}, runs, no("merge of %s refused: %s", andList(labels[:i+1]), r.Reason)
}

func leafGate(p Plan, read, cur records, created map[string]bool) *Refusal {
	if r := unreadWrite(p, read, created); r != nil {
		return r
	}
	// InThisOp alone is refused. A one-plan merge has no earlier write, so
	// the need is still not fail and this is the refusal. Merged after the
	// drop, the fold already shows the need failed.
	return inThisOpOpen(p.ev, cur)
}

// readsOf is every record the moves read, before this op writes.
func readsOf(base records, plans []Plan) records {
	s := cloneRecords(base)
	for _, p := range plans {
		unionMissing(&s, p.seen)
	}
	return s
}

func sealCompose(done Plan, plans []Plan, labels []string, seen records, runs int) Plan {
	done.replays = runs
	done.seen = seen
	done.label = strings.Join(labels, "+")
	done.parts = append([]Plan(nil), plans...)
	saved := append([]Plan(nil), plans...)
	done.again = func(next records) (Plan, *Refusal) {
		return compose(saved, next)
	}
	return done
}

// absorb keeps the later write of a field, state included. The store refuses
// two writes of one field, so the path cannot be replayed inside the op.
// coveredWorking refuses the one path that must not be collapsed.
func absorb(dst, add []store.Write) []store.Write {
	cover := map[[3]string]bool{}
	for _, w := range add {
		for _, f := range writeFields(w) {
			cover[f] = true
		}
	}
	var kept []store.Write
	for _, w := range dst {
		if !covered(w, cover) {
			kept = append(kept, w)
		}
	}
	return append(kept, add...)
}

// coveredWorking refuses a later state write that would erase a move into
// working. That write is what the store uses to count the row's width.
func coveredWorking(dst, add []store.Write) *Refusal {
	later := map[string]bool{}
	for _, w := range add {
		if s, ok := w.(store.SetCardState); ok {
			later[s.Card] = true
		}
	}
	for _, w := range dst {
		s, ok := w.(store.SetCardState)
		if ok && s.State == Working && later[s.Card] {
			return secondWrite([3]string{"card", s.Card, "state"})
		}
	}
	return nil
}

func covered(w store.Write, cover map[[3]string]bool) bool {
	fs := writeFields(w)
	if len(fs) == 0 {
		return false
	}
	for _, f := range fs {
		if !cover[f] {
			return false
		}
	}
	return true
}

func noteCreated(dst map[string]bool, ws []store.Write) {
	for _, w := range ws {
		if p, ok := w.(store.PutCard); ok {
			dst[p.ID] = true
		}
	}
}

// unreadWrite refuses a hand-built write to a card no plan of the op read
// and no earlier write created. A machine move reads what it writes.
func unreadWrite(p Plan, read records, created map[string]bool) *Refusal {
	if p.again != nil {
		return nil
	}
	for _, w := range p.Writes {
		id := writeCardID(w)
		if id == "" || created[id] {
			continue
		}
		if c, ok := read.cards[id]; ok && c.Kind != "" {
			continue
		}
		return no("writes card %s, which no plan read", id)
	}
	return nil
}

// inThisOpOpen refuses NeedFailed, Waive or Relink with InThisOp unless an
// earlier write of this op has already failed that need.
func inThisOpOpen(e Event, cur records) *Refusal {
	need, ok := inThisOpNeed(e)
	if !ok {
		return nil
	}
	if c, has := cur.cards[need]; has && c.State == Fail {
		return nil
	}
	return no("need %s was not failed by an earlier write of this op; merge it with the drop", need)
}

func inThisOpNeed(e Event) (string, bool) {
	switch v := e.(type) {
	case NeedFailed:
		return v.Need, v.InThisOp
	case Waive:
		return v.Need, v.InThisOp
	case Relink:
		return v.Need, v.InThisOp
	default:
		return "", false
	}
}

func writeCardID(w store.Write) string {
	if p, ok := w.(store.PutCard); ok {
		return p.ID
	}
	if a, ok := w.(store.SetCardAfter); ok {
		return a.Card
	}
	id, _, ok := oneField(w)
	if !ok {
		return ""
	}
	return id
}

// foldOnto applies writes to cur. A card only a plan read, not yet in the
// fold, is copied from that read first so the write edits the real record.
func foldOnto(cur *records, read records, ws []store.Write) {
	for _, w := range ws {
		if id := writeCardID(w); id != "" {
			fillCard(cur, read, id)
		}
		foldWrite(*cur, w)
	}
}

func fillCard(dst *records, read records, id string) {
	if dst.cards == nil {
		dst.cards = map[string]Card{}
	}
	if c, ok := dst.cards[id]; ok && c.Kind != "" {
		return
	}
	if c, ok := read.cards[id]; ok && c.Kind != "" {
		dst.cards[id] = cloneCard(c)
	}
}

func planLabels(plans []Plan) []string {
	out := make([]string, len(plans))
	for i, p := range plans {
		out[i] = planLabel(p, i+1)
	}
	return out
}

// dupNewCards refuses two creates of one card. The re-run of a second
// AskRead would cut the next index, and the collision on the first index
// would never be checked.
func dupNewCards(plans []Plan) *Refusal {
	var ws []store.Write
	for _, p := range plans {
		for _, w := range p.Writes {
			if _, ok := w.(store.PutCard); ok {
				ws = append(ws, w)
			}
		}
	}
	return dupWrites(ws)
}

// keepExpects drops a condition on a field an earlier plan of this op writes,
// and any condition on a card this op creates: the store checks every
// condition before any write, so a gen fence on a card PutCard has not
// inserted yet is "card does not exist".
func keepExpects(es []store.Expect, written map[[3]string]bool, created map[string]bool) []store.Expect {
	var out []store.Expect
	for _, e := range es {
		if id := expectCard(e); id != "" && created[id] {
			continue
		}
		if f, ok := expectField(e); ok && written[f] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// addedExpects keeps a fence the re-run added and the original plan did not
// have. Relink's acyclic walk reads cards the earlier writes of this op made
// reachable; those gens have to be fenced or a concurrent relink closes a cycle.
func addedExpects(next, orig []store.Expect, written map[[3]string]bool, created map[string]bool) []store.Expect {
	have := map[string]bool{}
	for _, e := range orig {
		have[fmt.Sprintf("%#v", e)] = true
	}
	var out []store.Expect
	for _, e := range next {
		if have[fmt.Sprintf("%#v", e)] {
			continue
		}
		if id := expectCard(e); id != "" && created[id] {
			continue
		}
		if f, ok := expectField(e); ok && written[f] {
			continue
		}
		out = append(out, e)
	}
	return out
}

func expectCard(e store.Expect) string {
	switch x := e.(type) {
	case store.CardGen:
		return x.Card
	case store.CardState:
		return x.Card
	case store.CardAbsent:
		return x.Card
	case store.NeedsLanded:
		return x.Card
	case store.CardRed:
		return x.Card
	default:
		return ""
	}
}

func noteFields(dst map[[3]string]bool, ws []store.Write) {
	for _, w := range ws {
		for _, f := range writeFields(w) {
			dst[f] = true
		}
	}
}

func expectField(e store.Expect) ([3]string, bool) {
	switch x := e.(type) {
	case store.CardState:
		return [3]string{"card", x.Card, "state"}, true
	case store.CardHolder:
		return [3]string{"card", x.Card, "holder"}, true
	case store.CardRed:
		return [3]string{"card", x.Card, "red"}, true
	case store.RowIs:
		return [3]string{"row", x.Row, "state"}, true
	case store.RowHeld:
		return [3]string{"row", x.Row, "held"}, true
	case store.StreamHeld:
		return [3]string{"stream", x.Stream, "held"}, true
	default:
		return [3]string{}, false
	}
}

// preemptGuard is the model's preempt, read off the flattened moves. Only a
// Withdraw with Preempt set is checked; a plain withdraw is an ordinary
// release, whatever its reason. The starter's priority includes a
// SetPriority earlier in this op. A row with no open width is layer 3's.
func preemptGuard(plans []Plan) *Refusal {
	prio := map[string]string{}
	for i, p := range plans {
		if st, ok := p.ev.(Start); ok {
			if b, ok := starter(p, prio); ok && b.Priority == "blocker" {
				if r := preemptAgainst(plans, i, b, st.Row); r != nil {
					return r
				}
			}
		}
		notePriority(prio, p.Writes)
	}
	return nil
}

func starter(p Plan, prio map[string]string) (Card, bool) {
	b, ok := p.seen.cards[lastID(p.label)]
	if !ok {
		return Card{}, false
	}
	if v, has := prio[b.ID]; has {
		b.Priority = v
	}
	return b, true
}

func preemptAgainst(plans []Plan, i int, b Card, row string) *Refusal {
	for j, q := range plans {
		if i == j {
			continue
		}
		wd, ok := q.ev.(Withdraw)
		if !ok {
			continue
		}
		c, ok := q.seen.cards[lastID(q.label)]
		if !ok || !consumer(c.Kind) || c.Of == b.ID {
			continue
		}
		if !wd.Preempt {
			continue
		}
		if r := preemptPair(b, c, row); r != nil {
			return r
		}
	}
	return nil
}

func notePriority(dst map[string]string, ws []store.Write) {
	for _, w := range ws {
		if s, ok := w.(store.SetCardPriority); ok {
			dst[s.Card] = normPriority(s.Priority)
		}
	}
}

func preemptPair(b, c Card, row string) *Refusal {
	if c.State != Working {
		return no("consumer %s is %s; a preempt withdraws a working consumer, and the step chooses the youngest on the row", c.ID, c.State)
	}
	if c.Holder != row {
		return no("consumer %s is on %s; blocker %s is dealt to %s", c.ID, c.Holder, b.ID, row)
	}
	return nil
}

func lastID(label string) string {
	f := strings.Fields(label)
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

func rerunPlan(p Plan, cur records) (Plan, int, *Refusal) {
	if p.again == nil {
		return p, 0, nil
	}
	out, r := p.again(cloneRecords(cur))
	return out, 1, r
}

func planLabel(p Plan, n int) string {
	if p.label != "" {
		return p.label
	}
	return fmt.Sprintf("plan %d", n)
}

func andList(ss []string) string {
	if len(ss) < 2 {
		return strings.Join(ss, "")
	}
	return strings.Join(ss[:len(ss)-1], ", ") + " and " + ss[len(ss)-1]
}

func finish(p Plan, label string, seen records, again rerun) (Plan, *Refusal) {
	out, r := checked(p)
	if r != nil {
		return Plan{}, r
	}
	out.label = label
	out.seen = seen
	out.again = again
	return out, nil
}

func takeSeen(p Card, cons []Card, rows map[string]Row, s Stream, needs map[string]Card) records {
	seen := records{cards: map[string]Card{}, rows: map[string]Row{}, streams: map[string]Stream{}}
	addCard(seen, p)
	for _, c := range cons {
		addCard(seen, c)
	}
	for _, c := range needs {
		addCard(seen, c)
	}
	for n, r := range rows {
		seen.rows[n] = cloneRow(r)
	}
	if s.Name != "" {
		seen.streams[s.Name] = s
	}
	return seen
}

func addCard(s records, c Card) {
	if c.ID == "" {
		return
	}
	if s.cards == nil {
		s.cards = map[string]Card{}
	}
	s.cards[c.ID] = cloneCard(c)
}

func sealMove(m *mover, e Event) (Plan, *Refusal) {
	p, cons, rows, stream, needs := m.p, m.cons, m.rows, m.s, m.needs
	seen := takeSeen(p, cons, rows, stream, needs)
	out, r := finish(m.plan, name(e)+" "+p.ID, seen, func(cur records) (Plan, *Refusal) {
		return Move(cardOr(cur, p), overlayCons(cons, cur, p.ID), overlayRows(rows, cur), streamOr(cur, stream), cardsOr(cur, needs), e)
	})
	return stamped(out, e, r)
}

func sealConsumer(m *mover, prod Card, row Row, e Event) (Plan, *Refusal) {
	seen := records{cards: map[string]Card{}, rows: map[string]Row{}, streams: map[string]Stream{}}
	addCard(seen, m.p)
	addCard(seen, prod)
	if row.Name != "" {
		seen.rows[row.Name] = cloneRow(row)
	}
	if v, ok := e.(Withdraw); ok && v.Stream.Name != "" {
		seen.streams[v.Stream.Name] = v.Stream
	}
	c, rw := m.p, row
	out, r := finish(m.plan, name(e)+" "+c.ID, seen, func(cur records) (Plan, *Refusal) {
		got := rw
		if row.Name != "" {
			if folded, ok := cur.rows[row.Name]; ok {
				got = folded
			}
		}
		return MoveConsumer(cardOr(cur, c), cardOr(cur, prod), got, foldedWithdraw(e, cur, prod.Stream))
	})
	return stamped(out, e, r)
}

// foldedWithdraw reads a release's stream from the folded state. The event
// carried the stream as it was when the withdraw was planned.
func foldedWithdraw(e Event, cur records, stream string) Event {
	v, ok := e.(Withdraw)
	if !ok {
		return e
	}
	if st, ok := cur.streams[stream]; ok && st.Name != "" {
		v.Stream = st
	}
	return v
}

func sealRow(p Plan, r Row, e Event) (Plan, *Refusal) {
	seen := records{rows: map[string]Row{r.Name: cloneRow(r)}}
	out, ref := finish(p, name(e)+" "+r.Name, seen, func(cur records) (Plan, *Refusal) {
		got := r
		if folded, ok := cur.rows[r.Name]; ok {
			got = folded
		}
		return MoveRow(got, e)
	})
	return stamped(out, e, ref)
}

func sealStream(p Plan, s Stream, e Event, ready []Card, again rerun) (Plan, *Refusal) {
	seen := records{cards: map[string]Card{}, streams: map[string]Stream{}}
	if s.Name != "" {
		seen.streams[s.Name] = s
	}
	for _, c := range ready {
		addCard(seen, c)
	}
	out, r := finish(p, name(e)+" "+s.Name, seen, again)
	return stamped(out, e, r)
}

func stamped(p Plan, e Event, r *Refusal) (Plan, *Refusal) {
	if r != nil {
		return Plan{}, r
	}
	p.ev = e
	return p, nil
}

func cardOr(cur records, was Card) Card {
	if c, ok := cur.cards[was.ID]; ok && c.ID != "" {
		return c
	}
	return was
}

func streamOr(cur records, was Stream) Stream {
	if s, ok := cur.streams[was.Name]; ok && s.Name != "" {
		return s
	}
	return was
}

func cardsOr(cur records, fallback map[string]Card) map[string]Card {
	if len(cur.cards) == 0 {
		return fallback
	}
	return cur.cards
}

// overlayCons is base with what the plans ahead wrote, plus any consumer of
// producer those plans cut. A read cut earlier in the op is live for the
// next move.
func overlayCons(base []Card, cur records, producer string) []Card {
	out := make([]Card, 0, len(base))
	seen := map[string]bool{}
	for _, c := range base {
		got := cardOr(cur, c)
		out = append(out, got)
		seen[got.ID] = true
	}
	var extra []string
	for id, c := range cur.cards {
		if !seen[id] && c.Of == producer && consumer(c.Kind) {
			extra = append(extra, id)
		}
	}
	slices.Sort(extra)
	for _, id := range extra {
		out = append(out, cur.cards[id])
	}
	return out
}

func overlayRows(base map[string]Row, cur records) map[string]Row {
	out := map[string]Row{}
	for n, r := range base {
		if got, ok := cur.rows[n]; ok {
			out[n] = got
		} else {
			out[n] = r
		}
	}
	for n, r := range cur.rows {
		if _, ok := out[n]; !ok {
			out[n] = r
		}
	}
	return out
}

func readyOn(s records, stream string) []Card {
	var ids []string
	for id, c := range s.cards {
		if c.Kind == "producer" && c.Stream == stream && c.State == Ready {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	out := make([]Card, len(ids))
	for i, id := range ids {
		out[i] = s.cards[id]
	}
	return out
}

func cloneRecords(s records) records {
	out := records{
		cards:   map[string]Card{},
		rows:    map[string]Row{},
		streams: map[string]Stream{},
	}
	for id, c := range s.cards {
		out.cards[id] = cloneCard(c)
	}
	for n, r := range s.rows {
		out.rows[n] = cloneRow(r)
	}
	for n, st := range s.streams {
		out.streams[n] = st
	}
	return out
}

func unionMissing(dst *records, src records) {
	if dst.cards == nil {
		dst.cards = map[string]Card{}
	}
	for id, c := range src.cards {
		if _, ok := dst.cards[id]; !ok {
			dst.cards[id] = cloneCard(c)
		}
	}
	if dst.rows == nil {
		dst.rows = map[string]Row{}
	}
	for n, r := range src.rows {
		if _, ok := dst.rows[n]; !ok {
			dst.rows[n] = cloneRow(r)
		}
	}
	if dst.streams == nil {
		dst.streams = map[string]Stream{}
	}
	for n, st := range src.streams {
		if _, ok := dst.streams[n]; !ok {
			dst.streams[n] = st
		}
	}
}

func cloneCard(c Card) Card {
	c.Needs = slices.Clone(c.Needs)
	c.After = slices.Clone(c.After)
	c.Paths = slices.Clone(c.Paths)
	c.DoneWhen = slices.Clone(c.DoneWhen)
	return c
}

func cloneRow(r Row) Row {
	r.Tiers = slices.Clone(r.Tiers)
	return r
}

func foldWrite(s records, w store.Write) {
	switch x := w.(type) {
	case store.PutCard:
		addCard(s, cardFromPut(x))
	case store.SetStreamHeld:
		editStream(s, x.Stream, x.Held)
	case store.SetRowState:
		editRow(s, x.Row, func(r *Row) { r.State = x.State })
	case store.SetRowFlaps:
		editRow(s, x.Row, func(r *Row) { r.Flaps = x.Flaps })
	case store.SetRowHeld:
		editRow(s, x.Row, func(r *Row) { r.Held, r.Reason = x.Held, x.Reason })
	default:
		foldCard(s, w)
	}
}

func cardFromPut(p store.PutCard) Card {
	return Card{ID: p.ID, Kind: p.Kind, Of: p.Of, Stream: p.Stream, State: Waiting, Holder: p.Holder, Tier: p.Tier,
		Attempt: p.Attempt, Review: p.Review, Finding: p.Finding, Score: p.Score, Priority: normPriority(p.Priority),
		Needs: slices.Clone(p.Needs), After: slices.Clone(p.After), Brief: p.Brief, Prose: p.Prose,
		Paths: slices.Clone(p.Paths), DoneWhen: slices.Clone(p.DoneWhen)}
}

func editStream(s records, name string, held bool) {
	if s.streams == nil {
		s.streams = map[string]Stream{}
	}
	st := s.streams[name]
	st.Name = name
	st.Held = held
	s.streams[name] = st
}

func editRow(s records, name string, fn func(*Row)) {
	if s.rows == nil {
		s.rows = map[string]Row{}
	}
	r := s.rows[name]
	r.Name = name
	fn(&r)
	s.rows[name] = r
}

func editCard(s records, id string, fn func(*Card)) {
	if id == "" {
		return
	}
	if s.cards == nil {
		s.cards = map[string]Card{}
	}
	c := s.cards[id]
	c.ID = id
	fn(&c)
	s.cards[id] = c
}

func foldCard(s records, w store.Write) {
	switch x := w.(type) {
	case store.SetCardState:
		editCard(s, x.Card, func(c *Card) { c.State = x.State })
	case store.SetCardReason:
		editCard(s, x.Card, func(c *Card) { c.Reason = x.Reason })
	case store.SetCardHead:
		editCard(s, x.Card, func(c *Card) { c.Head = x.Head })
	case store.SetCardFinding:
		editCard(s, x.Card, func(c *Card) { c.Finding = x.Finding })
	case store.SetCardBounces:
		editCard(s, x.Card, func(c *Card) { c.Bounces = x.Bounces })
	case store.SetCardBounceBase:
		editCard(s, x.Card, func(c *Card) { c.BounceBase = x.Base })
	case store.SetCardReview:
		editCard(s, x.Card, func(c *Card) { c.Review++ })
	case store.SetCardAuthor:
		editCard(s, x.Card, func(c *Card) { c.Author = x.Author })
	case store.SetCardAttempt:
		editCard(s, x.Card, func(c *Card) { c.Attempt = x.Attempt })
	case store.SetCardNeeds:
		editCard(s, x.Card, func(c *Card) { c.Needs = slices.Clone(x.Needs) })
	case store.SetCardAfter:
		editCard(s, x.Card, func(c *Card) { c.After = slices.Clone(x.After) })
	case store.SetCardRed:
		editCard(s, x.Card, func(c *Card) { c.Red = x.Red })
	default:
		foldCardRest(s, w)
	}
}

func foldCardRest(s records, w store.Write) {
	switch x := w.(type) {
	case store.SetCardProse:
		editCard(s, x.Card, func(c *Card) { c.Prose = x.Prose })
	case store.SetCardPaths:
		editCard(s, x.Card, func(c *Card) { c.Paths = slices.Clone(x.Paths) })
	case store.SetCardDoneWhen:
		editCard(s, x.Card, func(c *Card) { c.DoneWhen = slices.Clone(x.DoneWhen) })
	case store.SetCardPriority:
		editCard(s, x.Card, func(c *Card) { c.Priority = x.Priority })
	case store.SetCardScore:
		editCard(s, x.Card, func(c *Card) { c.Score = x.Score })
	case store.SetCardBranch:
		editCard(s, x.Card, func(c *Card) { c.Branch = x.Branch })
	case store.SetCardBaseSHA:
		editCard(s, x.Card, func(c *Card) { c.BaseSHA = x.SHA })
	case store.SetCardPriorHead:
		editCard(s, x.Card, func(c *Card) { c.PriorHead = x.Head })
	case store.SetCardResult:
		editCard(s, x.Card, func(c *Card) { c.Result = x.Result })
	case store.SetCardCost:
		editCard(s, x.Card, func(c *Card) { c.Cost, c.Priced = x.Cost, x.Priced })
	case store.SetCardUsage:
		editCard(s, x.Card, func(c *Card) { c.Usage = x.Usage })
	}
}
