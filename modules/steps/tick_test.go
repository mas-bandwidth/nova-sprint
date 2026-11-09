package steps

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTickHappyPath: one producer from waiting to landed, tick by tick,
// every op accepted (conformance, happy).
func TestTickHappyPath(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	p := s.tick()
	require.Len(t, s.tickOps(), 1, "one op: Resolve")
	require.Empty(t, s.rejected)
	s.requireState("p1", "ready")
	p = s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "working")
	s.requireState("p1.w1", "working")
	assert.Equal(t, "r1", s.card("p1.w1").Holder, "the idle lane with most room, by name")
	assert.Equal(t, "sprint/s1/p1", s.card("p1.w1").Branch)
	assert.Equal(t, base, s.card("p1.w1").BaseSHA)
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	p = s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "review")
	s.requireState("p1.r1.1", "working")
	assert.Equal(t, "r2", s.card("p1.r1.1").Holder, "the rank above the writer's, never the author")
	s.take("p1.r1.1")
	s.report("p1.r1.1", "ok", sha(1), "fine")
	p = s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "merging")
	p = s.tick()
	require.Len(t, p.Edges, 1, "a land request")
	req := s.request("land", "s1")
	assert.Equal(t, []string{"p1"}, req.Cards)
	assert.Equal(t, []string{sha(1)}, req.Heads)
	assert.Equal(t, base, req.Base)
	p = s.tick()
	assert.Empty(t, p.Edges, "one land request in flight per stream")
	s.land(req)
	p = s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "landed")
	assert.Empty(t, p.Carry)
	s.settle()
	s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "landed", Landed: req.Heads})
	p = s.tick()
	assert.Empty(t, p.Ops, "a repeated result is consumed with no move")
	assert.Empty(t, p.Carry)
}

// TestTickFixPath: a read says fix; the producer goes back to ready with
// the finding, is dealt again carrying its head, and a second read says yes.
func TestTickFixPath(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	s.take("p1.r1.1")
	s.report("p1.r1.1", "fix", sha(1), "the test is wrong")
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "ready")
	assert.Equal(t, "the test is wrong", s.card("p1").Finding)
	assert.Equal(t, 1, s.card("p1").Bounces)
	s.tick()
	s.requireState("p1", "working")
	w2 := s.card("p1.w2")
	assert.Equal(t, sha(1), w2.Prior, "the fix continues from the head")
	assert.Equal(t, "the test is wrong", w2.Finding)
	s.take("p1.w2")
	s.report("p1.w2", "ok", sha(2), "fixed")
	s.tick()
	s.take("p1.r2.1")
	s.report("p1.r2.1", "ok", sha(2), "fine")
	s.tick()
	s.requireState("p1", "merging")
}

// toMerging takes p1 of s1 through one work and one ok read to merging,
// with its land request in flight.
func toMerging(s *sim) EdgeRequest {
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	s.take("p1.r1.1")
	s.report("p1.r1.1", "ok", sha(1), "fine")
	s.run(2)
	return s.request("land", "s1")
}

// TestTickLandJudgementEveryOutcome: anything but landed (intent rule 32)
// is one land judgement per card on the coordinator's row, that tick, with
// the facts and the choices land, fix and retry; nothing moves, nothing is
// blamed, and no new request is asked while it is open.
func TestTickLandJudgementEveryOutcome(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"ci_red", "no-ci", "conflict", "merge_refused", "pr_unopenable", "branch_failed", "ci_fault", "missing_head", "head_moved", "hand", "fault"} {
		s := newSim(t)
		first := toMerging(s)
		s.arrive(InboxItem{Kind: "land", Request: first.ID, Outcome: outcome, Reason: "rung 1; base tip " + base + "; failing unit; first failing lines: --- FAIL: TestX"})
		p := s.tick()
		require.Empty(t, s.rejected, outcome)
		s.requireState("p1", "merging")
		j := s.snap().Judgements["land.p1.1"]
		assert.Equal(t, "open", j.State, outcome)
		assert.Contains(t, j.Question, outcome)
		assert.Contains(t, j.Question, "--- FAIL: TestX")
		assert.Contains(t, j.Question, "card p1 (stream s1 onto ")
		assert.Contains(t, j.Question, "head "+first.Heads[0])
		assert.Contains(t, j.Command, "choices: land | fix | retry")
		assert.Empty(t, p.Edges, outcome)
		s.run(3)
		assert.Empty(t, s.inflight, "no request while the judgement is open (%s)", outcome)
		assert.Empty(t, s.openJudgements("engine"), "a judged card is not unlanded (%s)", outcome)
	}
}

// TestTickLandAnswers: fix sends the producer to fix with the finding (a
// red: review +1); retry asks a new landing; land records it landed.
func TestTickLandAnswers(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	first := toMerging(s)
	s.arrive(InboxItem{Kind: "land", Request: first.ID, Outcome: "ci_red", Reason: "failing unit"})
	s.run(2)
	_, ref := s.verb(Request{Actor: "rowan", Judgement: "land.p1.1", Answer: "retry"})
	require.Nil(t, ref)
	s.run(2)
	second := s.request("land", "s1")
	assert.Equal(t, first.Heads, second.Heads, "retry asks the landing again")
	s.arrive(InboxItem{Kind: "land", Request: second.ID, Outcome: "ci_red", Reason: "failing unit again"})
	s.run(2)
	_, ref = s.verb(Request{Actor: "rowan", Card: "p1", Event: card.Red{Head: sha(1), Reason: "unit fails: fix TestX"}, Judgement: "land.p1.2", Answer: "fix"})
	require.Nil(t, ref)
	s.requireState("p1", "ready")
	assert.True(t, s.card("p1").Red)
	assert.Equal(t, 2, s.card("p1").Review)
	assert.Equal(t, "unit fails: fix TestX", s.card("p1").Finding)
	s.run(2)
	s.requireState("p1.w2", "working")
	assert.Equal(t, "unit fails: fix TestX", s.card("p1.w2").Finding, "the fix attempt carries the finding")

	h := newSim(t)
	req := toMerging(h)
	h.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "conflict"})
	h.run(2)
	_, ref = h.verb(Request{Actor: "rowan", Card: "p1", Event: card.Landed{Head: sha(1), Merge: sha(7), Reason: "by hand: merged"}, Judgement: "land.p1.1", Answer: "land"})
	require.Nil(t, ref)
	h.requireState("p1", "landed")
	assert.Equal(t, "landed at "+sha(7)+": by hand: merged", h.card("p1").Reason)
}

// TestTickLadderRungs: one head waiting is a request of its own (rung 1);
// at policy's backed_up (2 by default) every waiting head is one request
// (rung 2), and the request names each head's branch and the CI wait.
func TestTickLadderRungs(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 4, "pro")
	s.row("r2", 4, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "a/y")
	s.run(2)
	for i, p := range []string{"p1", "p2"} {
		s.take(p + ".w1")
		s.report(p+".w1", "ok", sha(i+1), "done")
	}
	s.tick()
	for _, p := range []string{"p1", "p2"} {
		s.take(p + ".r1.1")
		s.report(p+".r1.1", "ok", s.card(p).Head, "fine")
	}
	s.run(2)
	req := s.request("land", "s1")
	assert.Equal(t, []string{"p1", "p2"}, req.Cards, "backed up: one request for both")
	assert.Equal(t, []string{"sprint/s1/p1", "sprint/s1/p2"}, req.Branches)
	assert.Equal(t, Engine.Landing.CIWait, req.Wait)
}

// TestTickFaultSpendsNothing: a fault report returns the producer to ready
// with no bounce, and the next deal carries the pushed head.
func TestTickFaultSpendsNothing(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	s.take("p1.w1")
	s.report("p1.w1", "fault", "", "exit 137")
	s.requireState("p1", "ready")
	assert.Equal(t, 0, s.card("p1").Bounces)
	s.tick()
	s.requireState("p1.w2", "working")
}

// TestTickFaultedReadIsOwedAgain (defect 58, PR 641 F3): a review whose
// only read faulted is not stuck: the verdict is retry (no ok read, so
// never a yes), the faulted read is not counted, and the next tick deals a
// new read while the producer stays in review.
func TestTickFaultedReadIsOwedAgain(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 1, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.tick()
	s.tick()
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1.r1.1", "working")
	s.take("p1.r1.1")
	s.report("p1.r1.1", "fault", "", "exit 137")
	s.tick()
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "review")
	var dealt []string
	for _, c := range s.snap().Cards {
		if c.Of == "p1" && c.Kind == "read" && c.State == "working" {
			dealt = append(dealt, c.ID)
		}
	}
	require.Len(t, dealt, 1, "one new read owed and dealt")
	assert.NotEqual(t, "p1.r1.1", dealt[0])
}
