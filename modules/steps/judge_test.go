package steps

import (
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

type askReads struct{ n int }

func (a askReads) Ask(Questions) Answers {
	return Answers{Version: 1, Models: benchModels(), Reads: map[string]int{"p1": a.n}}
}

// TestJudgeAtEpisodesAndSupersede covers the live 2026-11-16 collision
// ("judgement flap.macbook.24 exists", 19,871 refused judge ops):
//  1. jindex tracks the highest trailing episode number in addition to the
//     judgement count, so lapse() after judgeAt("flap", "macbook", 12, ...)
//     opens flap.macbook.13 (and when .24 already exists while an older .2
//     lapses, opens .25 instead of colliding with .24).
//  2. When flaps grow while an earlier flap episode is still open, judgeAt
//     expires the older open episode in the same op so at most one episode is
//     open per (kind, about).
func TestJudgeAtEpisodesAndSupersede(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.row("macbook.local", 1, "pro")
	s.must("flaps", store.SetRowFlaps{Row: "macbook.local", Flaps: 12})
	s.tick()
	require.Equal(t, []string{"flap.macbook.local.12"}, s.openJudgements("flap"))

	// Past the deadline at the same flap count: lapse opens .13, never .2.
	s.now = s.snap().Judgements["flap.macbook.local.12"].Deadline.Add(time.Second)
	s.tick()
	require.Empty(t, s.rejected)
	require.Equal(t, []string{"flap.macbook.local.13"}, s.openJudgements("flap"))
	assert.Equal(t, "expired", s.snap().Judgements["flap.macbook.local.12"].State)

	// Flaps grow while .13 is open: .13 is expired and .15 opens as the one open episode.
	s.must("flaps", store.SetRowFlaps{Row: "macbook.local", Flaps: 15})
	s.tick()
	require.Empty(t, s.rejected)
	require.Equal(t, []string{"flap.macbook.local.15"}, s.openJudgements("flap"))
	assert.Equal(t, "expired", s.snap().Judgements["flap.macbook.local.13"].State)
	assert.Equal(t, "reopened at 15 as flap.macbook.local.15", s.snap().Judgements["flap.macbook.local.13"].Answer)

	// Even when a higher closed episode (.24) coexists with an older open episode (.2)
	// from before the fix, lapse advances past 24 to .25 instead of colliding with .24.
	s.must("close15", store.AnswerJudgement{ID: "flap.macbook.local.15", Answer: "retry: ack", Actor: "carol"})
	s.must("open24", store.PutJudgement{ID: "flap.macbook.local.24", Kind: "flap", About: "macbook.local", Question: "row flapped 24", Group: "flap", Deadline: s.now.Add(time.Hour)})
	s.must("close24",
		store.AnswerJudgement{ID: "flap.macbook.local.24", Answer: "retry: ack", Actor: "carol"},
		store.PutJudgement{ID: "flap.macbook.local.2", Kind: "flap", About: "macbook.local", Question: "legacy open", Group: "flap", Deadline: s.now.Add(time.Second)},
		store.SetRowFlaps{Row: "macbook.local", Flaps: 24},
	)
	s.now = s.snap().Judgements["flap.macbook.local.2"].Deadline.Add(time.Second)
	s.tick()
	require.Empty(t, s.rejected, "judgementOp must not collide with flap.macbook.local.24")
	require.Equal(t, []string{"flap.macbook.local.25"}, s.openJudgements("flap"))
	assert.Equal(t, "expired", s.snap().Judgements["flap.macbook.local.2"].State)
}

// TestCannotReadMatchesReadCandidates ensures cleared("cannot-read") uses the
// exact read-candidate eligibility of dealRead: a row already holding a live
// read of the same head does not falsely clear cannot-read when a second read
// is owed, and a tier-serving row without NEEDS-ROUTE clears cannot-read once
// open.
func TestCannotReadMatchesReadCandidates(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.ask = askReads{n: 2}
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 2, "pro")
	s.producer("p1", "s1", "pro", "a.go")
	s.run(2)
	w1 := "p1.w1"
	s.take(w1)
	s.report(w1, "ok", sha(1), "done")
	// First tick deals 1 read to `other`; 2 reads are owed, and no third row exists.
	s.tick()
	require.Equal(t, "review", s.state("p1"))
	// Next tick: `other` already holds p1.r1, so no row can take the 2nd read;
	// cannot-read opens and must STAY open (not be immediately expired by cleared).
	s.tick()
	require.Equal(t, []string{"cannot-read.p1.1"}, s.openJudgements("cannot-read"))
	s.tick()
	require.Equal(t, []string{"cannot-read.p1.1"}, s.openJudgements("cannot-read"), "a row already reading p1 must not clear cannot-read")
	assert.Equal(t, "open", s.snap().Judgements["cannot-read.p1.1"].State)

	// Once a third row serving pro (with no routes) joins, cannot-read clears and the 2nd read deals.
	s.row("r3", 2, "pro")
	s.run(2)
	assert.Empty(t, s.openJudgements("cannot-read"))
	assert.Equal(t, "row r3 may read p1", s.snap().Judgements["cannot-read.p1.1"].Answer)
}
