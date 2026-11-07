package steps

import (
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// answers is every choice a judgement may offer: each moves something,
// asks again, or is seen (verbs.answerRequest maps each).
var answers = []string{"unblock", "drop", "hold", "land", "fix", "yes", "rework", "retry", "read", "wait"}

func splitChoices(list string) []string {
	var out []string
	for _, c := range strings.Split(list, "|") {
		out = append(out, strings.TrimSpace(c))
	}
	return out
}

// TestJudgeChoicesNeverRetryOnly: no judgement's only choice is retry
// (directive 60), every kind's, an engine judgement's about a card in each
// state and about a stream; every choice is one the judge verb answers.
func TestJudgeChoicesNeverRetryOnly(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.fold()
	tk := newTick(s.v, s.inputs())
	lists := map[string]string{"engine stream": tk.engineChoices("s1"), "engine producer": tk.engineChoices("p1")}
	for kind, list := range choices {
		lists[kind] = list
	}
	for _, st := range []string{"waiting", "ready", "working", "review", "merging", "blocked", "landed"} {
		c := s.v.Cards["p1"]
		c.State = st
		s.v.Cards["p1"] = c
		lists["engine "+st] = tk.engineChoices("p1")
	}
	for what, list := range lists {
		cs := splitChoices(list)
		assert.NotEqual(t, []string{"retry"}, cs, "%s: retry is not a choice on its own", what)
		for _, c := range cs {
			assert.Contains(t, answers, c, "%s: choice %q", what, c)
		}
	}
}

// TestTickEngineSitsOnce: an engine judgement has no deadline, so it is not
// raised again every Judgement timeout (directive 60); retry asks afresh and
// the cause, still there, is raised once more; gone, the card moves.
func TestTickEngineSitsOnce(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.edit = func(in *Inputs) { delete(in.Heads, "s1") }
	s.run(3)
	require.Equal(t, []string{"engine.s1.1"}, s.openJudgements("engine"))
	j := s.snap().Judgements["engine.s1.1"]
	assert.True(t, j.Deadline.IsZero(), "no deadline")
	assert.Contains(t, j.Command, "choices: retry | hold")
	s.now = s.now.Add(3 * seconds(Engine.Timeouts.Judgement))
	s.run(3)
	assert.Equal(t, []string{"engine.s1.1"}, s.openJudgements("engine"), "past the Judgement timeout it is the same one")
	_, ref := s.verb(Request{Actor: "rowan", Judgement: "engine.s1.1", Answer: "retry"})
	require.Nil(t, ref)
	s.run(3)
	assert.Equal(t, []string{"engine.s1.2"}, s.openJudgements("engine"), "retry re-plans; the cause holds, so once more")
	s.edit = nil
	_, ref = s.verb(Request{Actor: "rowan", Judgement: "engine.s1.2", Answer: "retry"})
	require.Nil(t, ref)
	s.run(3)
	assert.Empty(t, s.openJudgements("engine"))
	s.requireState("p1", "working")
}

// TestTickEngineClaimsItsCard: while an engine judgement about a card is
// open no step plans the card (the refused plan is not met again every
// tick); retry closes it and the card is planned afresh from the state.
func TestTickEngineClaimsItsCard(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.must("judge", store.PutJudgement{ID: "engine.p1.1", Kind: "engine", About: "p1", Question: "a defect",
		Command: "nova-sprint judge engine.p1.1 <choice> --reason <why>   choices: retry | hold | drop"})
	s.run(3)
	s.requireState("p1", "waiting")
	_, ref := s.verb(Request{Actor: "rowan", Judgement: "engine.p1.1", Answer: "retry"})
	require.Nil(t, ref)
	s.run(3)
	s.requireState("p1", "working")
	assert.Empty(t, s.openJudgements("engine"))
}

// TestTickStallOncePerStall: one stall judgement per stream per stall, no
// deadline; wait quiets it until the stall ends (a landing), and the next
// stall is a new episode.
func TestTickStallOncePerStall(t *testing.T) {
	s := newSim(t)
	toReview(s)
	s.take("p1.r1.1")
	s.now = s.now.Add(seconds(Engine.Timeouts.LandingStall))
	s.run(3)
	require.Equal(t, []string{"stall.s1.1"}, s.openJudgements("stall"))
	assert.Contains(t, s.snap().Judgements["stall.s1.1"].Command, "choices: wait | hold")
	_, ref := s.verb(Request{Actor: "rowan", Judgement: "stall.s1.1", Answer: "wait"})
	require.Nil(t, ref)
	s.now = s.now.Add(2 * seconds(Engine.Timeouts.LandingStall))
	s.run(3)
	assert.Empty(t, s.openJudgements("stall"), "seen: not raised again in the same stall")
	s.report("p1.r1.1", "ok", sha(1), "fine")
	s.run(3)
	s.land(s.request("land", "s1"))
	s.run(3)
	s.requireState("p1", "landed")
	s.producer("p2", "s1", "pro", "a/y")
	s.run(3)
	s.take("p2.w1")
	s.report("p2.w1", "ok", sha(2), "done")
	s.run(3)
	s.take("p2.r1.1")
	s.now = s.now.Add(seconds(Engine.Timeouts.LandingStall))
	s.run(3)
	assert.Equal(t, []string{"stall.s1.2"}, s.openJudgements("stall"), "a new stall after the landing")
}

// TestTickNoStallUnderLandJudgement: a stream whose card is under a land
// judgement is not also a stall: the land judgement is the stall's cause,
// already on the board (directive 60).
func TestTickNoStallUnderLandJudgement(t *testing.T) {
	s := newSim(t)
	req := toMerging(s)
	s.arrive(InboxItem{Kind: "land", Request: req.ID, Outcome: "ci_red", Reason: "failing unit"})
	s.run(2)
	require.Equal(t, []string{"land.p1.1"}, s.openJudgements("land"))
	s.now = s.now.Add(3 * seconds(Engine.Timeouts.LandingStall))
	s.run(3)
	assert.Empty(t, s.openJudgements("stall"))
}

// TestTickAfterIsShown: a card that lands after one under a land judgement
// is shown waiting behind it (an after judgement naming it, choices land or
// wait), never an engine judgement nobody can answer (directive 78); it
// clears when the target leaves merging, and the card lands.
func TestTickAfterIsShown(t *testing.T) {
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 4, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/x")
	s.must("add", store.PutCard{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p2", After: []string{"p1"}, Paths: []string{"a/y"}})
	s.run(2)
	s.take("p1.w1")
	s.take("p2.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	s.take("p1.r1.1")
	s.report("p1.r1.1", "ok", sha(1), "fine")
	s.run(2)
	s.arrive(InboxItem{Kind: "land", Request: s.request("land", "s1").ID, Outcome: "ci_red", Reason: "failing unit"})
	s.run(2)
	require.Equal(t, []string{"land.p1.1"}, s.openJudgements("land"))
	s.report("p2.w1", "ok", sha(2), "done")
	s.tick()
	s.take("p2.r1.1")
	s.report("p2.r1.1", "ok", sha(2), "fine")
	s.run(3)
	s.requireState("p2", "merging")
	require.Equal(t, []string{"after.p2.1"}, s.openJudgements("after"))
	assert.Contains(t, s.snap().Judgements["after.p2.1"].Question, "after p1")
	assert.Contains(t, s.snap().Judgements["after.p2.1"].Command, "choices: land | wait")
	assert.Empty(t, s.openJudgements("engine"))
	_, ref := s.verb(Request{Actor: "rowan", Card: "p1", Event: card.Red{Head: sha(1), Reason: "unit fails: fix TestX"}, Judgement: "land.p1.1", Answer: "fix"})
	require.Nil(t, ref)
	s.run(3)
	assert.Empty(t, s.openJudgements("after"), "the target left merging")
	i := slices.IndexFunc(s.inflight, func(r EdgeRequest) bool { return r.Kind == "land" && slices.Contains(r.Cards, "p2") })
	assert.GreaterOrEqual(t, i, 0, "p2 lands: %+v", s.inflight)
}
