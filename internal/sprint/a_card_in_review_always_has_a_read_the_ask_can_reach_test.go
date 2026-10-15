package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// TestACardInReviewAlwaysHasAReadTheAskCanReach pins the rule the sprint
// learned on 2026-10-12: a read that ends without a verdict, or is withdrawn by
// a hold or a return, was still asked, so `ask --another` must not say the card
// was never asked ("not asked yet") while `ask` says its reads are all out. The
// ask verb's two refusals and the no-stall rule's decisions decide "asked at
// this attempt" by one function (askedAtAttempt), so they never contradict each
// other (docs/SPEC-SPRINT.md section 6, the reads).
//
// The card is in review at attempt 2 with one read withdrawn by a hold and
// another handed back with no verdict: no read is out, so the check (the
// every-producer-has-its-consumer-out rule, ConsumersOut/readOutFor) reports the
// gap before the tick, and the next tick's ask (TickAsk) asks a fresh read of
// the attempt (docs/SPEC-SPRINT.md, the tick postconditions).
func TestACardInReviewAlwaysHasAReadTheAskCanReach(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	readersReadEveryTier(w)
	w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "m", Enabled: true}}

	// The card is in review at attempt 2, as putReview places it: the state the
	// bug found it in (the attempt-1 read withdrawn, the attempt-2 read returned).
	putReview(w, "s1-1", proBrief, 2, 1, "head-s1-1")
	pr := w.s.Work.Card("s1-1")
	require.Equal(t, Review, w.state("s1-1"), "the primary is in review")

	// One read of the attempt was withdrawn by a hold ...
	w.s.Readers.Put(&Card{ID: ReadCardID("s1-1", 2, "reader-a"), Rev: 1, Fields: map[string]string{
		"kind": "read", "primary": "s1-1", "reader": "reader-a", "attempt": "2",
		"head": "head-s1-1", "retired": stamp(t0), "retired_by": RetiredByHold}})
	// ... and another ended without a verdict (read --return): history on the
	// readers table, neither of them a read out.
	w.s.Readers.Put(&Card{ID: ReadCardID("s1-1", 2, "reader-b"), Rev: 1, Fields: map[string]string{
		"kind": "read", "primary": "s1-1", "reader": "reader-b", "attempt": "2",
		"head": "head-s1-1", "retired": stamp(t0), "retired_by": RetiredByReturned}})

	// One definition: a read was asked at this attempt, so the ask verb's two
	// refusals and the no-stall rule's decisions agree (askedAtAttempt).
	require.True(t, askedAtAttempt(w.s, pr), "a read was asked at attempt 2")
	require.Equal(t, 2, ReadsWanted(w.s, pr), "the attempt's two reads are still wanted")

	// The check reports the gap before the tick: a card in review with no read
	// out and no open judgment is a producer whose consumer is missing.
	require.Contains(t, quietSubjects(ConsumersOut(w.s)), "s1-1",
		"a card in review with no read out is a check violation")
	require.Zero(t, len(liveReadsAt(w.s, pr, 2)), "no read of the attempt is out")

	// ask --another is refused only when no read was asked; here one was, so it
	// asks another reader rather than refusing "not asked yet"
	another := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Another: true})
	for _, r := range another.Refused {
		assert.NotContains(t, r.Why, "not asked yet at attempt 2",
			"a read already asked: --another must not say it was never asked")
	}
	require.NotEmpty(t, another.Units, "ask --another asks a reader")

	// the plain ask is free to ask the read again: its reads are not all out, so
	// the two verbs agree (the bug refused this one "asked already").
	plain := Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}})
	require.Empty(t, plain.Refused, "ask must reach a reader: %+v", plain.Refused)

	// the next tick's ask re-asks the attempt: a read card goes out again
	p, _ := TickAsk(w.s, TickReq{})
	require.NotEmpty(t, p.Units, "the tick's ask re-asks the withdrawn and returned reads")
	w.must(p)
	require.NotEmpty(t, readsAt(w.s, pr, 2), "a read of attempt 2 is out after the tick")
	require.Zero(t, ReadsWanted(w.s, pr), "the tick asked every read it wanted")

	// the gap is closed: no read is missing now, and the tick's own check
	// (TickCheck) is quiet for the card.
	assert.NotContains(t, quietSubjects(ConsumersOut(w.s)), "s1-1",
		"the read the tick asked closes the check's gap")
	checked, _ := TickCheck(w.s, TickReq{})
	for _, n := range checked.Notes {
		assert.NotContains(t, n.What, "no read out and no open judgment",
			"the check is quiet once the tick asked the read")
	}
}
