package steps

import (
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/card"
)

// reports is the reports step (LAYER-3 "Reports"): inbox items of kind
// report, in arrival order, each one group (a work report with its
// Finished, Broken or Faulted; a read report alone) or one answer. LAYER-6
// applies a row's report as the row's own op, so this inbox path is for a
// layer 6 that routes them here; the fold shows either way. There is no
// take item: a dealt consumer is in hand at the deal (defect 52).
func (t *tick) reports() {
	for _, it := range t.in.Inbox {
		if it.Kind == "report" {
			t.reportItem(it)
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

// ack answers an item from the record and consumes it; nothing is applied
// twice, so layer 4 may redeliver. The answer rides as a refusal's does.
func (t *tick) ack(it InboxItem, format string, args ...any) {
	t.groups = append(t.groups, group{kind: "none", none: true, key: GroupKey{Kind: "none", Item: it.ID},
		answer: &RowAnswer{Result: it.ID, Row: it.Row, Text: fmt.Sprintf(format, args...)}})
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
		t.refuse(it, "consumer %s is held by %s, not %s", c.ID, c.Holder, it.Row)
	case it.Session != "" && row.Session != "" && row.Session != it.Session:
		t.refuse(it, "report from session %s; row %s is now %s; nothing applied", it.Session, it.Row, row.Session)
	case c.State == "fail":
		t.refuse(it, "consumer %s was withdrawn: %s; branch %s is kept", c.ID, c.Reason, c.Branch)
	case c.State == "done" && c.Result == it.Result && (it.Head == "" || c.Head == it.Head):
		t.ack(it, "applied at seq %d", t.v.touched[c.ID])
	case c.State == "done":
		t.refuse(it, "consumer %s reported %s at head %s already; a report is applied once", c.ID, c.Result, c.Head)
	case c.Kind == "work" && c.Attempt != p.Attempt:
		t.refuse(it, "consumer %s is of attempt %d; producer %s is at attempt %d", c.ID, c.Attempt, p.ID, p.Attempt)
	case c.Kind == "read" && c.Head != p.Head:
		t.refuse(it, "read %s was cut at %s; producer %s is at %s", c.ID, c.Head, p.ID, p.Head)
	default:
		t.reportGroup(it, c.ID)
	}
}

// reportGroup is card.ReportPlan on the view: the consumer's Report and, for
// a work consumer, the producer's move in one plan.
func (t *tick) reportGroup(it InboxItem, c string) {
	m := move{label: "Report " + c, plan: func() (card.Plan, *card.Refusal) {
		cc := t.cardOf(c)
		p := t.cardOf(cc.Of)
		r := card.Report{Row: it.Row, Result: it.Result, Head: it.Head, Reason: it.Reason, Cost: it.Cost, Priced: it.Priced}
		return card.ReportPlan(cc, p, t.consOf(p.ID), t.rows(), t.streamOf(p.Stream), t.needsOf(p.ID), r)
	}}
	g, r := t.newGroup("report", c, m)
	if r != nil {
		t.refuse(it, "%s", r.Reason)
		return
	}
	g.key.Item = it.ID
	t.add(g)
}
