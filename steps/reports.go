package steps

import (
	"fmt"
	"regexp"

	"github.com/mas-bandwidth/nova-sprint/card"
)

// inbox is the inbox in arrival order, one pass over every kind, so the
// budget consumes it as a prefix (CarryFIFO): a row's report or take (LAYER-3
// "Reports"; LAYER-6 applies a row's report as the row's own op, so this path
// is for a layer 6 that routes them here, and is not in the model), an edge
// result (a landing's outcome, a stop's ack: land.go).
func (t *tick) inbox() {
	taken := map[string]int{}
	for _, it := range t.in.Inbox {
		switch it.Kind {
		case "report":
			t.reportItem(it)
		case "take":
			t.takeItem(it, taken)
		case "land", "stop":
			t.edgeItem(it)
		default:
			t.refuse(it, "item %s is of kind %q; the inbox takes report, take, land and stop", it.ID, it.Kind)
		}
	}
}

// refuse answers an item with a refusal and consumes it; nothing moves. The
// answer rides with the item's group and goes out only when the budget
// admits it (Stella, 03f44371); a carried item is answered when it is
// consumed.
func (t *tick) refuse(it InboxItem, format string, args ...any) {
	t.groups = append(t.groups, group{kind: "none", none: true, key: GroupKey{Kind: "none", Item: it.ID},
		answer: &RowAnswer{Result: it.ID, Row: it.Row, Text: fmt.Sprintf(format, args...), Refused: true}})
}

// ack answers a repeated item from the record and consumes it; nothing is
// applied twice, so layer 4 may redeliver. The answer rides as a refusal's.
func (t *tick) ack(it InboxItem, format string, args ...any) {
	t.groups = append(t.groups, group{kind: "none", none: true, key: GroupKey{Kind: "none", Item: it.ID},
		answer: &RowAnswer{Result: it.ID, Row: it.Row, Text: fmt.Sprintf(format, args...)}})
}

// appliedBy is the seq a card's record was written by: the fold's witness,
// else the seq of the snapshot the view was built from (the write is at or
// before it; a cold view did not see it).
func (t *tick) appliedBy(id string) int64 {
	if seq := t.v.touched[id]; seq > 0 {
		return seq
	}
	return t.v.Built
}

// reportItem plans one report: the refusals of the design, in its order,
// then card.ReportPlan as one group.
func (t *tick) reportItem(it InboxItem) {
	c, ok := t.v.Cards[it.Consumer]
	if !ok || !isConsumer(c) {
		t.refuse(it, "card %s does not exist: run `nova-sprint next --as %s`", it.Consumer, it.Row)
		return
	}
	if !t.unclaimed(c.ID) {
		return // a group of this tick moves it: the item waits for the folded result (Stella, 03f44371)
	}
	row, p := t.v.Rows[c.Holder], t.v.Cards[c.Of]
	switch {
	case c.Holder != it.Row:
		t.refuse(it, "consumer %s is held by %s, not %s: run `nova-sprint next --as %s`", c.ID, c.Holder, it.Row, it.Row)
	case it.Session != "" && row.Session != "" && row.Session != it.Session:
		t.refuse(it, "report from session %s; row %s is now %s; nothing applied: run `nova-sprint next --as %s`", it.Session, it.Row, row.Session, it.Row)
	case c.State == "fail":
		t.refuse(it, "consumer %s was withdrawn: %s; branch %s is kept: run `nova-sprint next --as %s`", c.ID, c.Reason, c.Branch, it.Row)
	case c.State == "done" && c.Result == it.Result && (it.Head == "" || c.Head == it.Head):
		t.ack(it, "applied by seq %d", t.appliedBy(c.ID))
	case c.State == "done":
		t.refuse(it, "consumer %s reported %s at head %s already; a report is applied once: run `nova-sprint next --as %s`", c.ID, c.Result, c.Head, it.Row)
	case c.Kind == "work" && c.Attempt != p.Attempt:
		t.refuse(it, "consumer %s is of attempt %d; producer %s is at attempt %d: run `nova-sprint next --as %s`", c.ID, c.Attempt, p.ID, p.Attempt, it.Row)
	case c.Kind == "read" && c.Head != p.Head:
		t.refuse(it, "read %s was cut at %s; producer %s is at %s: run `nova-sprint show %s`", c.ID, c.Head, p.ID, p.Head, p.ID)
	case c.State == "ready":
		t.refuse(it, "take it first: `nova-sprint take %s@%d`", c.ID, c.Gen)
	case c.Kind == "read" && it.Result == "fix" && !hasFileLine(it.Reason):
		t.refuse(it, "a fix read names a file and line: `nova-sprint read %s@%d fix --finding <path>:<line> ...`", c.ID, c.Gen)
	default:
		t.reportGroup(it, c.ID)
	}
}

// hasFileLine says whether a finding names a file and a line (path:line).
func hasFileLine(finding string) bool { return fileLine.MatchString(finding) }

var fileLine = regexp.MustCompile(`\S+\.\w+:\d+`)

// reportGroup is card.ReportPlan on the view: the consumer's Report and, for
// a work consumer, the producer's move in one plan.
func (t *tick) reportGroup(it InboxItem, c string) {
	r := card.Report{Row: it.Row, Result: it.Result, Head: it.Head, Reason: it.Reason, Cost: it.Cost, Priced: it.Priced}
	g, ref := t.newGroup("report", c, t.reportMove(c, r))
	if ref != nil {
		t.refuse(it, "%s", ref.Reason)
		return
	}
	g.key.Item = it.ID
	t.add(g)
}

// reportMove is card.ReportPlan of consumer c: the report with the
// producer's Finished, Broken or Faulted in one plan (a read's moves only
// the read).
func (t *tick) reportMove(c string, r card.Report) move {
	return move{label: "Report " + c, plan: func() (card.Plan, *card.Refusal) {
		cc := t.cardOf(c)
		p := t.cardOf(cc.Of)
		return card.ReportPlan(cc, p, t.consOf(p.ID), t.rows(), t.streamOf(p.Stream), t.needsOf(p.ID), r)
	}}
}

// takeItem plans one take: at most width minus working per row by an
// in-tick tally, so the store's width write never refuses a tick op. The
// refusal names a consumer the row works, never the one being taken.
func (t *tick) takeItem(it InboxItem, taken map[string]int) {
	c, ok := t.v.Cards[it.Consumer]
	switch {
	case !ok || !isConsumer(c):
		t.refuse(it, "card %s does not exist: run `nova-sprint next --as %s`", it.Consumer, it.Row)
		return
	case !t.unclaimed(c.ID):
		return // as a report's: the item waits for the folded result
	case c.Holder != it.Row:
		t.refuse(it, "consumer %s is held by %s, not %s: run `nova-sprint next --as %s`", c.ID, c.Holder, it.Row, it.Row)
		return
	case c.State == "working":
		t.ack(it, "applied by seq %d", t.appliedBy(c.ID))
		return
	case c.State != "ready":
		t.refuse(it, "consumer %s is %s; only a ready consumer is taken: run `nova-sprint next --as %s`", c.ID, c.State, it.Row)
		return
	}
	row, width := c.Holder, t.width(c.Holder)
	if t.v.Working(row)+taken[row] >= width {
		t.refuse(it, "row %s is at width %d: finish %s first", row, width, t.workingOn(row))
		return
	}
	g, r := t.newGroup("take", c.ID, t.consumerMove(c.ID, card.Take{Row: it.Row}))
	if r != nil {
		t.refuse(it, "%s", r.Reason)
		return
	}
	taken[row]++
	g.key.Item = it.ID
	t.add(g)
}

// workingOn is the first working consumer on row by id, or "a working
// consumer" when the tally alone fills the width.
func (t *tick) workingOn(row string) string {
	for _, id := range t.v.Live(row) {
		if t.v.Cards[id].State == "working" {
			return id
		}
	}
	return "a working consumer"
}
