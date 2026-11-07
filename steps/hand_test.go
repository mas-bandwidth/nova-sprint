package steps

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
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
// acknowledged is nudged each tick from NudgeAfter after the deal, naming
// the card and its command; at UnackedAfter one judgement names the row and
// the card; the acknowledgement stops both. The clock is the world's.
func TestHandNudgesThenJudgesUnacked(t *testing.T) {
	s := dealOne(t)
	p := s.tick()
	assert.Empty(t, p.Nudges, "inside NudgeAfter: no nudge")
	s.now = s.now.Add(seconds(Engine.NudgeAfter))
	p = s.tick()
	require.Len(t, p.Nudges, 1)
	n := p.Nudges[0]
	assert.Equal(t, Nudge{Row: "r1", Card: "p1.w1"}, Nudge{Row: n.Row, Card: n.Card})
	assert.True(t, strings.HasPrefix(n.Line, "NUDGE card=p1.w1 "), n.Line)
	assert.Contains(t, n.Line, "nova-sprint take p1.w1@")
	assert.Empty(t, s.openJudgements("unacked"), "a nudge, not yet a judgement")
	p = s.tick()
	assert.Len(t, p.Nudges, 1, "a nudge each tick")
	s.now = s.now.Add(seconds(Engine.UnackedAfter))
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
	s := dealOne(t)
	s.report("p1.w1", "ok", sha(1), "done")
	s.now = s.now.Add(seconds(Engine.UnackedAfter))
	p := s.tick()
	for _, n := range p.Nudges {
		assert.NotEqual(t, "p1.w1", n.Card)
	}
	assert.Empty(t, s.openJudgements("unacked"))
}

// TestHandUndealtJudged: a ready producer left undealt past UndealtAfter
// while a serving row has room opens one undealt judgement naming the row,
// the card and the cause. Here the tick budget (1 card) is spent each tick
// by the next producer's Resolve, so the deal never comes to p1.
func TestHandUndealtJudged(t *testing.T) {
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
	s := dealOne(t)
	s.producer("p2", "s1", "pro", "a/y")
	s.tick()
	s.requireState("p2", "ready")
	s.now = s.now.Add(10 * time.Duration(Engine.UndealtAfter) * time.Second)
	s.ack("p1.w1")
	s.tick()
	assert.Empty(t, s.openJudgements("undealt"), "r1 is at width")
}

// TestHandRouteFaultBlocks: a work fault naming a missing route blocks the
// producer on the first fault, opens a blocked judgement naming the row and
// what is missing, and the producer is never dealt again by itself
// (defect 48).
func TestHandRouteFaultBlocks(t *testing.T) {
	s := dealOne(t)
	s.report("p1.w1", "fault", "", card.MissingRoute+" no adb on this row")
	s.requireState("p1", "blocked")
	s.tick()
	js := s.openJudgements("blocked")
	require.Len(t, js, 1)
	q := s.snap().Judgements[js[0]].Question
	assert.Contains(t, q, "row r1")
	assert.Contains(t, q, "no adb on this row")
	s.run(4)
	s.requireState("p1", "blocked")
	_, redealt := s.snap().Cards["p1.w2"]
	assert.False(t, redealt, "never dealt again by itself")
	assert.Len(t, s.openJudgements("blocked"), 1)
}

// TestHandPlainFaultRedeals: a fault that names no route goes back to
// ready and is dealt again, as before.
func TestHandPlainFaultRedeals(t *testing.T) {
	s := dealOne(t)
	s.report("p1.w1", "fault", "", "exit 137")
	s.requireState("p1", "ready")
	s.tick()
	s.requireState("p1.w2", "working")
	assert.Empty(t, s.openJudgements("blocked"))
}
