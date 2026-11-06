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
// after the first); a refusal names every plan and the condition. A hand-built
// plan, with no move to run again, is kept as written. Two writes to one
// field are still refused. Several reads of one head are AskReads, not two
// AskReads merged.
func Merge(plans ...Plan) (Plan, *Refusal) {
	if !needsReplay(plans) {
		return mergeConcat(plans)
	}
	return mergeSeq(plans)
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
	for _, p := range plans {
		m.Expect = append(m.Expect, p.Expect...)
		m.Writes = append(m.Writes, p.Writes...)
	}
	if r := dupWrites(m.Writes); r != nil {
		return Plan{}, r
	}
	return checked(m)
}

func mergeSeq(plans []Plan) (Plan, *Refusal) {
	cur := cloneRecords(plans[0].seen)
	foldWrites(cur, plans[0].Writes)
	out := Plan{
		Expect: append([]store.Expect{}, plans[0].Expect...),
		Writes: append([]store.Write{}, plans[0].Writes...),
	}
	labels := []string{planLabel(plans[0], 1)}
	reRuns := 0
	for i := 1; i < len(plans); i++ {
		unionMissing(&cur, plans[i].seen)
		next, ran, r := rerunPlan(plans[i], cur)
		reRuns += ran
		if r != nil {
			names := append(append([]string{}, labels...), planLabel(plans[i], i+1))
			return Plan{replays: reRuns}, no("merge of %s refused: %s", andList(names), r.Reason)
		}
		out.Expect = append(out.Expect, next.Expect...)
		out.Writes = append(out.Writes, next.Writes...)
		foldWrites(cur, next.Writes)
		unionMissing(&cur, next.seen)
		labels = append(labels, planLabel(plans[i], i+1))
	}
	if r := dupWrites(out.Writes); r != nil {
		return Plan{replays: reRuns}, r
	}
	done, r := checked(out)
	if r != nil {
		return Plan{replays: reRuns}, no("merge of %s refused: %s", andList(labels), r.Reason)
	}
	done.replays = reRuns
	done.seen = cur
	done.label = strings.Join(labels, "+")
	return done, nil
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
	return finish(m.plan, name(e)+" "+p.ID, seen, func(cur records) (Plan, *Refusal) {
		return Move(cardOr(cur, p), overlayCons(cons, cur), overlayRows(rows, cur), streamOr(cur, stream), cardsOr(cur, needs), e)
	})
}

func sealConsumer(m *mover, prod Card, row Row, e Event) (Plan, *Refusal) {
	seen := records{cards: map[string]Card{}, rows: map[string]Row{}}
	addCard(seen, m.p)
	addCard(seen, prod)
	if row.Name != "" {
		seen.rows[row.Name] = cloneRow(row)
	}
	c, rw := m.p, row
	return finish(m.plan, name(e)+" "+c.ID, seen, func(cur records) (Plan, *Refusal) {
		got := rw
		if row.Name != "" {
			if folded, ok := cur.rows[row.Name]; ok {
				got = folded
			}
		}
		return MoveConsumer(cardOr(cur, c), cardOr(cur, prod), got, e)
	})
}

func sealRow(p Plan, r Row, e Event) (Plan, *Refusal) {
	seen := records{rows: map[string]Row{r.Name: cloneRow(r)}}
	return finish(p, name(e)+" "+r.Name, seen, func(cur records) (Plan, *Refusal) {
		got := r
		if folded, ok := cur.rows[r.Name]; ok {
			got = folded
		}
		return MoveRow(got, e)
	})
}

func sealStream(p Plan, s Stream, e Event, ready []Card, again rerun) (Plan, *Refusal) {
	seen := records{cards: map[string]Card{}, streams: map[string]Stream{}}
	if s.Name != "" {
		seen.streams[s.Name] = s
	}
	for _, c := range ready {
		addCard(seen, c)
	}
	return finish(p, name(e)+" "+s.Name, seen, again)
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

func overlayCons(base []Card, cur records) []Card {
	out := make([]Card, len(base))
	for i, c := range base {
		out[i] = cardOr(cur, c)
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
