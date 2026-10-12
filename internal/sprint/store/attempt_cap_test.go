package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The attempt cap (docs/SPEC-SPRINT.md, "The brief is wrong, not the worker"; brief_bound.go
// AttemptsCap; the owner, 2026-10-04 and 2026-10-12). The night before, only a repeated finding
// bounded a card: one that failed differently each time was reworked 262 times. After the
// cap's attempts since the coordinator last decided on the card (nova-config's sprint row
// max_attempts, default 4; a stream's own by stream set --attempts) the card is not dealt
// again: one judgment "brief defect after N attempts (max_attempts N) ..., $X spent" lists
// every attempt's finding, with the decisions carry, brief, re-tier and drop; the coordinator's
// own rework is the decision and is never refused by the cap.

// snapRoutes is the harness's snapshot with the routes and the sprint row's bars read into
// it (Store.RoutesInto): what the attempt cap is read from.
func (h *harness) snapRoutes() *sprint.Snapshot {
	h.t.Helper()
	s := h.snap()
	require.NoError(h.t, h.st.RoutesInto(h.ctx, s))
	return s
}

func TestABrokenFindingIsReworkedPastTheAttemptCap(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 1, briefOf("flash", ""))
	h.must(DealStep(sprint.DealReq{}))
	rework := func() []sprint.Refusal {
		return h.run(ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}, Who: "tester"})).Refused
	}
	for a := 1; a <= sprint.AttemptsDefault+1; a++ {
		// each attempt's work costs a dollar, reported by the member (cost.go)
		wc := h.snap().Fleet.Card(h.snap().Work.Card("s1-1").F("work"))
		g := map[string]int{wc.ID: wc.Int("gen")}
		h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g}))
		h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: g, Head: "h" + wc.F("attempt"), Report: "r", Usage: "input=1 actual_usd=1 actual_by=harness"}))
		h.readBrokenAt("s1-1", "internal/f"+wc.F("attempt")+".go:1: wrong in attempt "+wc.F("attempt"))
		require.Empty(t, rework(), "attempt %d: a broken finding is reworked, not stopped at the cap", a)
		assert.Len(t, h.openOf(sprint.NBriefWrong), 0, "attempt %d: no brief defect for a broken finding", a)
	}
	pr := h.snap().Work.Card("s1-1")
	assert.Contains(t, pr.F(sprint.FieldFindings), "attempt 1: internal/f1.go:1: wrong in attempt 1", "the rework keeps every attempt's finding")
	h.clean("a broken finding past the cap")
}

// Failed work reaches the cap too, with the machine's rework between the attempts: a seat's
// rework is a coordinator decision and opens a fresh window (brief_bound.go, FieldDecidedAttempt),
// so the rework here is the rule's (TickRuleRework), as the night of 2026-10-12 ran them.
func TestFailedWorkReachesTheAttemptCapToo(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 1, briefOf("flash", ""))
	h.m.SetMaxAttempts("2")
	h.must(DealStep(sprint.DealReq{}))
	h.finishAttempt("s1-1", true, "")
	h.must(ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}, Who: "tick", PerCard: map[string]sprint.ReworkCard{"s1-1": {Fix: "r", Rule: sprint.RuleFailed, Said: "redealt"}}}))
	pr := h.snap().Work.Card("s1-1")
	assert.Equal(t, "attempt 1: r", pr.F(sprint.FieldFindings), "the failed report is the attempt's finding")
	assert.Empty(t, pr.F(sprint.FieldDecidedAttempt), "a rule's rework is no decision")
	h.finishAttempt("s1-1", true, "")
	open := h.openOf(sprint.NBriefWrong)
	require.Len(t, open, 1, "the second failure is the cap's judgment, with the cap at 2")
	assert.Contains(t, open[0].Note.What, "s1-1: brief defect after 2 attempts (max_attempts 2) without a coordinator decision, nothing priced; the brief is wrong, not the worker; findings: attempt 1: r; attempt 2: r")
	assert.Contains(t, open[0].Note.What, "nova-sprint brief s1-1 --brief-file <path>", "the finish's cap judgment names the brief command")
	assert.Contains(t, open[0].Note.What, "nova-sprint rework s1-1 --fix <text> --tier <a higher tier>", "the re-tier")
	assert.Contains(t, open[0].Note.What, "nova-sprint drop s1-1 --reason '<why>'", "and the drop command")
	assert.Equal(t, []string{sprint.DecisionCarry, "brief", sprint.DecisionReTier, "drop"}, open[0].Note.Decisions)
	assert.Empty(t, h.openOf(sprint.NWorkFailed))
	h.clean("failed work at the cap")
}

// The seat's rework at the cap is the coordinator's decision: never refused by the cap, and the
// card's count starts again (the owner, 2026-10-12: "max attempts before you do judgement and
// decide what to do").
func TestTheSeatsReworkAtTheCapIsADecisionAndOpensAFreshWindow(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 1, briefOf("flash", ""))
	h.m.SetMaxAttempts("2")
	h.must(DealStep(sprint.DealReq{}))
	h.finishAttempt("s1-1", true, "")
	h.must(ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}, Who: "tick", PerCard: map[string]sprint.ReworkCard{"s1-1": {Fix: "r", Rule: sprint.RuleFailed, Said: "redealt"}}}))
	h.finishAttempt("s1-1", true, "")
	require.Len(t, h.openOf(sprint.NBriefWrong), 1, "two attempts by the machine are the cap")
	h.must(ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}, Fix: "try this", Who: "tester"}))
	pr := h.snap().Work.Card("s1-1")
	assert.Equal(t, "2", pr.F(sprint.FieldDecidedAttempt), "the seat's rework is the decision at attempt 2")
	assert.Equal(t, 3, pr.Int("attempt"), "attempt 3 is cut at once")
	assert.Equal(t, 1, sprint.AttemptsSince(pr), "a fresh window: one attempt since the decision, the one cut")
	_, atCap := sprint.AtAttemptCap(pr, 2)
	assert.False(t, atCap, "under the cap again")
	assert.Empty(t, h.openOf(sprint.NBriefWrong), "the judgment is answered")
	h.clean("the seat's rework at the cap")
}

// The attempt cap is nova-config's sprint row max_attempts (the owner, 2026-10-12: "configurable
// in nova-config, and default to 4"), read with the routes, a stream's own over it; the
// sprint-wide set --attempts is retired and refuses naming nova-config's verb, so there is one
// source and no twin.
func TestTheAttemptCapIsNovaConfigsMaxAttemptsAndTheStreams(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)
	h.must(AddStep(sprint.AddReq{Brief: proBrief, Stream: "s2", Count: 1}))
	assert.Equal(t, sprint.AttemptsDefault, h.snapRoutes().AttemptsCap("s1"), "the default")
	h.m.SetMaxAttempts("7")
	assert.Equal(t, 7, h.snapRoutes().AttemptsCap("s1"), "nova-config's max_attempts, read with the routes")
	res := h.must(SetStep(sprint.SetReq{Streams: []string{"s2"}, Attempts: "3", Who: h.st.Actor}))
	assert.Equal(t, "stream s2 attempts 3", res.Moved[0])
	assert.Equal(t, 3, h.snapRoutes().AttemptsCap("s2"), "the stream's over the sprint's")
	assert.Equal(t, 7, h.snapRoutes().AttemptsCap("s1"))
	h.must(SetStep(sprint.SetReq{Streams: []string{"s2"}, Attempts: sprint.ReadTierDefault, Who: h.st.Actor}))
	assert.Equal(t, 7, h.snapRoutes().AttemptsCap("s2"), "default takes the stream's off")
	for _, bad := range []string{"x", "", "0", "-1", "101", "2.5"} {
		h.m.SetMaxAttempts(bad)
		assert.Equal(t, sprint.AttemptsDefault, h.snapRoutes().AttemptsCap("s2"), "max_attempts %q is the default: the sprint works with a nova-tools not yet adopted", bad)
	}
	for _, v := range []string{"7", "default", "0", "many"} {
		r := h.run(SetStep(sprint.SetReq{Attempts: v, Who: h.st.Actor}))
		require.Len(t, r.Refused, 1, "set --attempts %q is refused", v)
		assert.Contains(t, r.Refused[0].Why, sprint.MaxAttemptsSetting, "naming nova-config's verb")
	}
	assert.Equal(t, sprint.AttemptsDefault, h.snapRoutes().AttemptsCap("s1"), "nothing was written")
	h.clean("the cap set")
}
