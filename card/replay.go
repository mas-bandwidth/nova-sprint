package card

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
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

// Merge applies plans as one op, in order. A plan the machine built is run
// again against the writes of the plans ahead of it (one extra move per plan
// after the first), and the result carries that same replay, so a merged
// plan composes with the next one. A refusal names every plan and the
// condition. The conditions are each plan's, from the snapshot it was read
// on, except a condition on a field an earlier plan of this op writes.
// A hand-built plan, with no move to run again, is kept as written. Two
// writes to one field are still refused. Several reads of one head are
// AskReads, not two AskReads merged.
func Merge(plans ...Plan) (Plan, *Refusal) {
	if !needsReplay(plans) {
		return mergeConcat(plans)
	}
	return compose(plans, records{}, false)
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

// compose is Merge of plans the machine built. rerunFirst is set when the
// caller already folded earlier plans into base: every plan, the first
// included, is read against base. Otherwise the first plan stands as built.
func compose(plans []Plan, base records, rerunFirst bool) (Plan, *Refusal) {
	labels := planLabels(plans)
	named := func(r *Refusal, runs int) (Plan, *Refusal) {
		return Plan{replays: runs}, no("merge of %s refused: %s", andList(labels), r.Reason)
	}
	// Two plans that each create the same card refuse before the re-run. A
	// re-run would allocate the next id and hide it, and several reads of
	// one head are AskReads. A later write the re-run drops (Hold after
	// Start no longer parks that producer) is checked on what is emitted.
	if r := dupNewCards(plans); r != nil {
		return named(r, 0)
	}
	cur := cloneRecords(base)
	if !rerunFirst {
		cur = cloneRecords(plans[0].seen)
	}
	out, runs, r := foldPlans(plans, labels, cur, rerunFirst)
	if r != nil {
		return Plan{replays: runs}, r
	}
	if r := dupWrites(out.Writes); r != nil {
		return named(r, runs)
	}
	if r := preemptGuard(plans); r != nil {
		return named(r, runs)
	}
	done, r := checked(out)
	if r != nil {
		return named(r, runs)
	}
	done.replays = runs
	done.seen = cur
	done.label = strings.Join(labels, "+")
	saved := append([]Plan(nil), plans...)
	done.again = func(next records) (Plan, *Refusal) {
		return compose(saved, next, true)
	}
	return done, nil
}

// foldPlans re-runs each plan after the first (and the first too, when
// rerunFirst) and keeps the conditions that still describe the snapshot.
func foldPlans(plans []Plan, labels []string, cur records, rerunFirst bool) (Plan, int, *Refusal) {
	var out Plan
	written := map[[3]string]bool{}
	runs := 0
	for i, p := range plans {
		unionMissing(&cur, p.seen)
		next, ran, r := takePlan(p, cur, i == 0 && !rerunFirst)
		runs += ran
		if r != nil {
			names := append(append([]string{}, labels[:i]...), labels[i])
			return Plan{}, runs, no("merge of %s refused: %s", andList(names), r.Reason)
		}
		out.Expect = append(out.Expect, keepExpects(p.Expect, written)...)
		out.Writes = append(out.Writes, next.Writes...)
		noteFields(written, next.Writes)
		foldWrites(cur, next.Writes)
		unionMissing(&cur, next.seen)
	}
	return out, runs, nil
}

func takePlan(p Plan, cur records, keep bool) (Plan, int, *Refusal) {
	if keep {
		return p, 0, nil
	}
	return rerunPlan(p, cur)
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

// keepExpects drops a condition on a field an earlier plan of this op writes.
// The store checks every condition before any write, so a fence read off the
// folded state would contradict the one read off the snapshot.
func keepExpects(es []store.Expect, written map[[3]string]bool) []store.Expect {
	var out []store.Expect
	for _, e := range es {
		if f, ok := expectField(e); ok && written[f] {
			continue
		}
		out = append(out, e)
	}
	return out
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

// preemptGuard is the model's preempt, read off the events: the starter's
// priority is blocker, and the withdrawn consumer is working on that row.
// A row with no open width is layer 3's, not this check.
func preemptGuard(plans []Plan) *Refusal {
	for i, p := range plans {
		st, ok := p.ev.(Start)
		if !ok {
			continue
		}
		b, ok := p.seen.cards[lastID(p.label)]
		if !ok || b.Priority != "blocker" {
			continue
		}
		for j, q := range plans {
			if i == j {
				continue
			}
			if _, ok := q.ev.(Withdraw); !ok {
				continue
			}
			c, ok := q.seen.cards[lastID(q.label)]
			if !ok || !consumer(c.Kind) || c.Of == b.ID {
				continue
			}
			if r := preemptPair(b, c, st.Row); r != nil {
				return r
			}
		}
	}
	return nil
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

func foldWrites(s records, ws []store.Write) {
	for _, w := range ws {
		foldWrite(s, w)
	}
}

func foldWrite(s records, w store.Write) {
	switch x := w.(type) {
	case store.PutCard:
		addCard(s, cardFromPut(x))
	case store.SetStreamHeld:
		editStream(s, x.Stream, x.Held)
	case store.SetRowState:
		editRow(s, x.Row, func(r *Row) { r.State = x.State })
	case store.SetRowWidth:
		editRow(s, x.Row, func(r *Row) { r.Width = x.Width })
	case store.SetRowFlaps:
		editRow(s, x.Row, func(r *Row) { r.Flaps = x.Flaps })
	case store.SetRowHeld:
		editRow(s, x.Row, func(r *Row) { r.Held, r.Reason = x.Held, x.Reason })
	default:
		foldCard(s, w)
	}
}

func cardFromPut(p store.PutCard) Card {
	return Card{ID: p.ID, Kind: p.Kind, Of: p.Of, Stream: p.Stream, Holder: p.Holder, Tier: p.Tier,
		Attempt: p.Attempt, Review: p.Review, Finding: p.Finding, Score: p.Score, Priority: p.Priority,
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
	}
}
