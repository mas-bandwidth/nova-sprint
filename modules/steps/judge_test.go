package steps

import (
	"fmt"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEngineNoHeadJudgementClearsWhenTheHeadIsRead is defect D6 (2026-11-07):
// a stream whose base tip the edge failed to read opens an engine judgement
// at the deal; once the inputs carry its head again the cause has cleared,
// and the judgement expires on its own instead of sitting until its
// deadline or the coordinator's retry. A stream still with no head keeps it.
func TestEngineNoHeadJudgementClearsWhenTheHeadIsRead(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("r1", 8, "pro")
	s.stream("s1")
	s.stream("s2")
	s.producer("p1", "s1", "pro", "a.go")
	s.producer("p2", "s2", "pro", "b.go")
	s.headless = set{"s1": {}, "s2": {}}
	s.run(2)
	require.Equal(t, "ready", s.state("p1"))
	require.Equal(t, []string{"engine.s1.1", "engine.s2.1"}, s.openJudgements("engine"))
	assert.Contains(t, s.snap().Judgements["engine.s1.1"].Question, "stream s1 has no head in the tick's inputs")
	delete(s.headless, "s1")
	s.run(3)
	require.Equal(t, "working", s.state("p1"), "s1 deals once its head is read")
	require.Equal(t, []string{"engine.s2.1"}, s.openJudgements("engine"), "s1's judgement cleared; s2 still has no head")
	j := s.snap().Judgements["engine.s1.1"]
	assert.Equal(t, "expired", j.State)
	assert.Equal(t, "stream s1 has a head in the tick's inputs again", j.Answer)
}

// TestLandRateNeedsACardLeftToLand: a stream under its land target opens
// land-rate while it has a producer that may still land; once none is left
// its rate cannot move, so the judgement clears and no other opens.
func TestLandRateNeedsACardLeftToLand(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("r1", 1, "pro")
	s.stream("s1")
	var put, land []store.Write
	for i := range Engine.LandSample {
		id := fmt.Sprintf("n%02d", i)
		put = append(put, store.PutCard{ID: id, Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do " + id, Paths: []string{"n/" + id}})
		land = append(land, store.SetCardBounces{Card: id, Bounces: 1}, store.SetCardState{Card: id, State: "landed"})
	}
	s.must("seed", put...)
	s.must("land by hand", land...)
	s.producer("p1", "s1", "pro", "a.go")
	s.run(2)
	require.Equal(t, []string{"land-rate.s1.1"}, s.openJudgements("land-rate"))
	s.must("fail by hand", store.SetCardState{Card: "p1", State: "fail"})
	s.run(2)
	assert.Empty(t, s.openJudgements("land-rate"))
	assert.Equal(t, "stream s1 has no card left to land", s.snap().Judgements["land-rate.s1.1"].Answer)
}

// TestExpiryReopensWhatStillHolds is item 3 (2026-11-08: 153 stall, 83
// land-rate, 39 engine and 35 overlap judgements expired unanswered with no
// effect). At its deadline a judgement whose cause the tick still detects
// is never closed silently: it expires naming the episode that reopens it,
// and that episode opens in the same op with the repeat count, pushed again
// as NEEDS YOU. One whose cause no step detects any more closes as cleared.
func TestExpiryReopensWhatStillHolds(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("r1", 2, "pro")
	s.stream("s1")
	s.producer("p1", "s1", "heavy", "a.go") // no row serves heavy: unserved, every tick
	s.must("test", store.PutJudgement{ID: "engine.x.1", Kind: "engine", About: "x", Question: "a one-off defect", Group: "engine",
		Deadline: s.now.Add(time.Hour)})
	s.run(2)
	require.Equal(t, []string{"unserved.heavy.1"}, s.openJudgements("unserved"))
	first := s.snap().Judgements["unserved.heavy.1"]
	require.False(t, first.Deadline.IsZero())
	s.now = first.Deadline.Add(time.Second)
	s.tick()
	j := s.snap().Judgements
	assert.Equal(t, "expired", j["unserved.heavy.1"].State)
	assert.Equal(t, "deadline passed unanswered and it still holds: reopened as unserved.heavy.2 (repeat 1)", j["unserved.heavy.1"].Answer)
	require.Equal(t, []string{"unserved.heavy.2"}, s.openJudgements("unserved"))
	assert.Equal(t, "UNANSWERED 1 TIME, ESCALATED: "+first.Question, j["unserved.heavy.2"].Question)
	assert.Equal(t, "expired", j["engine.x.1"].State)
	assert.Equal(t, "cleared: no step detects its cause at its deadline", j["engine.x.1"].Answer)
	assert.Empty(t, s.openJudgements("engine"))

	s.now = j["unserved.heavy.2"].Deadline.Add(time.Second)
	s.tick()
	j = s.snap().Judgements
	assert.Equal(t, "deadline passed unanswered and it still holds: reopened as unserved.heavy.3 (repeat 2)", j["unserved.heavy.2"].Answer)
	assert.Equal(t, "UNANSWERED 2 TIMES, ESCALATED: "+first.Question, j["unserved.heavy.3"].Question)
}
