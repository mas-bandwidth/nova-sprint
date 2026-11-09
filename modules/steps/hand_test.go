package steps

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dealOne is a sim with p1 dealt to r1 (width 1, tier pro): in r1's hand,
// working, from the deal (defect 52).
func dealOne(t *testing.T) *sim {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.tick()
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1.w1", "working")
	return s
}

// TestHandNudgesThenJudgesUnacked: a card in hand the row has not
// acknowledged is nudged each tick from timeouts ready_nag after the deal,
// in the human's words with the row's counts, naming the card and its
// command; at ready_take, with no other row to deal it to, one judgement
// names the row and the card; the acknowledgement stops both. The clock is
// the world's.
func TestHandNudgesThenJudgesUnacked(t *testing.T) {
	t.Parallel()
	s := dealOne(t)
	p := s.tick() // the first one-second tick after the deal: ready_nag is 1 s
	require.Len(t, p.Nudges, 1)
	n := p.Nudges[0]
	assert.Equal(t, Nudge{Row: "r1", Card: "p1.w1"}, Nudge{Row: n.Row, Card: n.Card})
	assert.True(t, strings.HasPrefix(n.Line, "NUDGE you have a new ready card. you have 1 ready cards, you are currently working on 0, and your width is 1. Please work to width! card=p1.w1 "), n.Line)
	assert.Contains(t, n.Line, "nova-sprint take p1.w1@")
	assert.Empty(t, s.openJudgements("unacked"), "a nudge, not yet a judgement")
	p = s.tick()
	assert.Len(t, p.Nudges, 1, "a nudge each tick")
	s.now = s.now.Add(seconds(Engine.Timeouts.ReadyTake))
	s.tick()
	js := s.openJudgements("unacked")
	require.Len(t, js, 1)
	q := s.snap().Judgements[js[0]].Question
	assert.Contains(t, q, "row r1")
	assert.Contains(t, q, "card p1.w1")
	s.tick()
	assert.Len(t, s.openJudgements("unacked"), 1, "one judgement, not one a tick")
	s.ack("p1.w1")
	p = s.tick()
	assert.Empty(t, p.Nudges, "acknowledged: no nudge")
}

// TestHandNoNudgeAfterTheReport: a row that reports without a take is not
// nudged; the report is the first word the nudges wait for.
func TestHandNoNudgeAfterTheReport(t *testing.T) {
	t.Parallel()
	s := dealOne(t)
	s.report("p1.w1", "ok", sha(1), "done")
	s.now = s.now.Add(seconds(Engine.Timeouts.ReadyTake))
	p := s.tick()
	for _, n := range p.Nudges {
		assert.NotEqual(t, "p1.w1", n.Card)
	}
	assert.Empty(t, s.openJudgements("unacked"))
}

// TestHandReadyTakeRedeals: a work card in hand, not acknowledged past
// timeouts ready_take, is withdrawn and its producer dealt, in the same op,
// to another row with room, never the slow row; one under the deadline is
// nudged and left where it is (the human, 2026-11-07: "By leaving cards in
// ready we are violating this promise").
func TestHandReadyTakeRedeals(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.row("r2", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.tick()
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1.w1", "working")
	slow := s.card("p1.w1").Holder
	s.now = s.now.Add(time.Duration(Engine.Timeouts.ReadyTake-2) * time.Second)
	p := s.tick()
	require.Empty(t, s.tickOps(), "under the deadline: nudged, left alone")
	require.Len(t, p.Nudges, 1)
	s.requireState("p1.w1", "working")
	s.now = s.now.Add(2 * time.Second)
	p = s.tick()
	require.Empty(t, s.rejected)
	require.Len(t, s.tickOps(), 1, "the withdraw and the re-deal are one op")
	assert.Empty(t, p.Nudges, "a re-dealt card is not nudged")
	assert.Empty(t, s.openJudgements("unacked"), "re-dealt, not judged")
	s.requireState("p1.w1", "fail")
	assert.Regexp(t, `^in `+slow+`'s hand 18\d s, not taken; dealt again$`, s.card("p1.w1").Reason)
	s.requireState("p1", "working")
	s.requireState("p1.w2", "working")
	assert.NotEqual(t, slow, s.card("p1.w2").Holder, "the slow row is excluded")
}

// TestHandModellessIsAcked: the live store at the upgrade to v2.0.1 (PR
// 641 F2): a consumer the line before dealt is working with no model and
// no take cursor, which that line never wrote. It counts as acknowledged:
// past ready_take, with another row with room, it is neither nudged,
// withdrawn nor judged, and stays in its row's hand.
func TestHandModellessIsAcked(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.row("r2", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.move(s.plan("p1", card.Resolve{}))
	s.move(s.plan("p1", card.Start{Row: "r1", Branch: "sprint/s1/p1", Base: base})) // the old deal: no model
	require.Equal(t, []string{"p1.w1"}, s.unacked(), "no take cursor")
	require.Empty(t, s.card("p1.w1").Model)
	s.now = s.now.Add(2 * seconds(Engine.Timeouts.ReadyTake))
	p := s.tick()
	require.Empty(t, s.rejected)
	assert.Empty(t, p.Nudges)
	assert.Empty(t, s.tickOps(), "nothing withdrawn or dealt again")
	assert.Empty(t, s.openJudgements("unacked"))
	s.requireState("p1.w1", "working")
	assert.Equal(t, "r1", s.card("p1.w1").Holder)
}

// TestHandUndealtJudged: a ready producer left undealt past UndealtAfter
// while a serving row has room opens one undealt judgement naming the row,
// the card and the cause. Here the tick budget (1 card) is spent each tick
// by the next producer's Resolve, so the deal never comes to p1.
func TestHandUndealtJudged(t *testing.T) { // unitlint: serial, it writes the package's Engine, which every tick reads
	was := Engine.Limits
	Engine.Limits.Budget = 1
	t.Cleanup(func() { Engine.Limits = was })
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 3, "pro")
	for _, p := range []string{"p1", "p2", "p3"} {
		s.producer(p, "s1", "pro", "a/"+p)
	}
	s.tick() // p1 resolves
	s.requireState("p1", "ready")
	assert.Empty(t, s.openJudgements("undealt"), "ready for less than UndealtAfter")
	s.tick() // p2 resolves; p1 has waited two seconds
	s.requireState("p1", "ready")
	js := s.openJudgements("undealt")
	require.Len(t, js, 1)
	j := s.snap().Judgements[js[0]]
	assert.Equal(t, "p1", j.About)
	assert.Contains(t, j.Question, "row r1")
	assert.Contains(t, j.Question, "budget")
}

// TestHandNoUndealtWithoutRoom: a ready producer with no serving row with
// room is waiting for room, not stalled: no judgement.
func TestHandNoUndealtWithoutRoom(t *testing.T) {
	t.Parallel()
	s := dealOne(t)
	s.producer("p2", "s1", "pro", "a/y")
	s.tick()
	s.requireState("p2", "ready")
	s.now = s.now.Add(10 * time.Duration(Engine.UndealtAfter) * time.Second)
	s.ack("p1.w1")
	s.tick()
	assert.Empty(t, s.openJudgements("undealt"), "r1 is at width")
}

// TestHandRouteFaultWaits: a work fault naming a missing route leaves the
// producer waiting with that reason on the first fault (a card with a problem waits:
// Glenn, 2026-11-07), opens a held judgement naming the row and what is
// missing, and the producer is never dealt again by itself (defect 48);
// the coordinator's unhold answer deals it again.
func TestHandRouteFaultWaits(t *testing.T) {
	t.Parallel()
	s := dealOne(t)
	s.report("p1.w1", "fault", "", card.MissingRoute+" adb no device on this row")
	s.requireState("p1", "waiting")
	s.tick()
	js := s.openJudgements("held")
	require.Len(t, js, 1)
	q := s.snap().Judgements[js[0]].Question
	assert.Contains(t, q, "row r1")
	assert.Contains(t, q, "adb no device on this row")
	assert.Contains(t, q, "declares NEEDS-ROUTE: adb, so the deal sends it only to a row serving route:adb", "the fix in the routes' one vocabulary")
	s.run(4)
	s.requireState("p1", "waiting")
	_, redealt := s.snap().Cards["p1.w2"]
	assert.False(t, redealt, "never dealt again by itself")
	assert.Len(t, s.openJudgements("held"), 1)
	rc, ref := s.verb(Request{Actor: "rowan", Card: "p1", Event: card.UnholdCard{Reason: "adb attached"}})
	require.Nil(t, ref)
	require.True(t, rc.Accepted, rc.Reason)
	s.tick()
	assert.Empty(t, s.openJudgements("held"), "the judgement clears once the card is no longer held")
	s.run(2)
	s.requireState("p1.w2", "working")
}

// TestHandPlainFaultRedeals: a fault that names no route goes back to
// ready and is dealt again, as before.
func TestHandPlainFaultRedeals(t *testing.T) {
	t.Parallel()
	s := dealOne(t)
	s.report("p1.w1", "fault", "", "exit 137")
	s.requireState("p1", "ready")
	s.tick()
	s.requireState("p1.w2", "working")
	assert.Empty(t, s.openJudgements("held"))
}

// TestDealSkipsRowAbsentFromBus (D1', 2026-11-07): a row hooked and up in
// the store, with room, that is not on the bus this tick is dealt nothing,
// whatever its session record says: a configured row is not a present one.
// On the bus again, it is dealt.
func TestDealSkipsRowAbsentFromBus(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.away = set{"r1": {}}
	s.tick()
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "ready")
	assert.NotContains(t, s.snap().Cards, "p1.w1", "an absent row is dealt nothing")
	s.away = nil
	s.tick()
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1.w1", "working")
	assert.Equal(t, "r1", s.card("p1.w1").Holder)
}

// TestHandAbsentRowNotNudgedAndRedealt (D1'): a row that leaves the bus with
// a card in hand is not nudged (the bus would only queue it); the card still
// counts toward ready_take, and at the bound it is withdrawn and dealt to a
// present row, never to another absent one.
func TestHandAbsentRowNotNudgedAndRedealt(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.row("r2", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.tick()
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1.w1", "working")
	slow := s.card("p1.w1").Holder
	other := map[string]string{"r1": "r2", "r2": "r1"}[slow]
	s.away = set{slow: {}, other: {}}
	p := s.tick()
	assert.Empty(t, p.Nudges, "an absent row is not nudged")
	s.now = s.now.Add(seconds(Engine.Timeouts.ReadyTake))
	p = s.tick()
	require.Empty(t, s.rejected)
	assert.Empty(t, p.Nudges)
	s.requireState("p1.w1", "working") // the only other row is absent too: not re-dealt there
	js := s.openJudgements("unacked")
	require.Len(t, js, 1)
	assert.Contains(t, s.snap().Judgements[js[0]].Question, "not nudged: the row is absent from the bus")
	delete(s.away, other)
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1.w1", "fail")
	s.requireState("p1.w2", "working")
	assert.Equal(t, other, s.card("p1.w2").Holder, "re-dealt to the present row")
}

// TestDealPresentThroughOneFailedWho (D1' cold read): a `nova-bus who` read
// that fails leaves every row as the last good read saw it, within
// PresentGrace: the row is still dealt and no unserved judgement opens.
// Past the grace the row is absent: nothing is dealt and unserved opens.
func TestDealPresentThroughOneFailedWho(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "b/x")
	s.lapse = time.Second // the read this tick failed; the last good one was a tick ago
	s.tick()
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1.w1", "working")
	assert.Empty(t, s.openJudgements("unserved"), "one failed read judges nothing")
	s.report("p1.w1", "ok", sha(1), "done")
	s.lapse = time.Duration(Engine.PresentGrace+1) * time.Second
	s.tick()
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p2", "ready")
	assert.Len(t, s.openJudgements("unserved"), 1, "absent past the grace: no row serves")
}

// TestHandRedealOverBudgetStillNudges: past ready_take with another row
// with room, a re-deal the tick's spent budget refuses plans nothing, so
// the card is still in its row's hand and the row is still nudged; a
// redeal that planned nothing never reports that it dealt.
func TestHandRedealOverBudgetStillNudges(t *testing.T) { // unitlint: serial, it writes the package's Engine, which every tick reads
	was := Engine.Limits
	Engine.Limits.Budget = 1
	t.Cleanup(func() { Engine.Limits = was })
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.row("r2", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.tick()
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1.w1", "working")
	s.producer("p2", "s1", "pro", "a/y") // its Resolve spends the budget before the re-deal
	s.now = s.now.Add(seconds(Engine.Timeouts.ReadyTake))
	p := s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1.w1", "working")
	assert.Len(t, p.Nudges, 1, "not re-dealt, so still nudged")
}
