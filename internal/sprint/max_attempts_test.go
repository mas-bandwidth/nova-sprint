package sprint_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The attempt cap (docs/SPEC-SPRINT.md, the bounds; brief_bound.go; the owner, 2026-10-12,
// after epoch 16 ran one card to attempt 11 and another to 22: "11/22 attempts is tooooo many.
// max attempts should be configurable in nova-config, and default to 4"; "The max attempts 4 =
// max attempts before you do judgement and decide what to do"). A card runs at most
// max_attempts attempts between two coordinator decisions, every attempt counted whatever
// ended it; at the bound it is the coordinator's as one judgment and is dealt to no one; the
// coordinator's decision opens a fresh window. On the twin store (store.Mem), every rule on,
// each driving the real deal, take, finish, read, rule and merge steps, never AtBriefBound.

// deadlineReport is the report of an attempt the deadline failed with no worker report: a
// harness fault (HarnessFault class deadline), recorded "no" in the attempt record, which the
// cap of 2026-10-04 did not count.
const deadlineReport = "deadline: the take ran 601s past its deadline with no report"

// failedAttempt drives the primary's dealt (or ready) attempt to a failed finish with the
// report: the kind of end ruleFailed answers.
func (r *conflictRig) failedAttempt(id, report string) {
	r.t.Helper()
	if r.snap().Work.Card(id).Col == sprint.Ready {
		r.must(dealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{id}}}))
	}
	s := r.snap()
	wc := s.Fleet.Card(s.Work.Card(id).F("work"))
	require.NotNil(r.t, wc, "%s has a dealt attempt", id)
	r.must(store.TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}}))
	wc = r.snap().Fleet.Card(wc.ID)
	r.must(store.FinishStep(sprint.FinishReq{Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}, Failed: true, Report: report}))
}

// snapRoutes is the rig's snapshot with the routes and the sprint row's bars read into it, as
// a step with Routes set plans (store.Store.RoutesInto): what the attempt cap is read from.
func (r *conflictRig) snapRoutes() *sprint.Snapshot {
	r.t.Helper()
	s := r.snap()
	require.NoError(r.t, r.st.RoutesInto(r.ctx, s))
	return s
}

// attemptOf is the primary's attempt as the work table holds it.
func (r *conflictRig) attemptOf(id string) int { return r.snap().Work.Card(id).Int("attempt") }

// fourAttemptsEndEveryWay runs r-1 through four attempts that each end a different way, by the
// rules between them: the deadline (a harness fault, reworked by the failed rule), a reader's
// finding outside PATHS (the read-broken rule widens the brief in place), a reader's finding
// inside PATHS (the read-broken rule reworks with it as the fix), and failed work. Each end but
// the last is answered by the machine, never by a mind. It returns with the fourth attempt
// ended and the tick run once after it.
func (r *conflictRig) fourAttemptsEndEveryWay() {
	r.t.Helper()
	r.failedAttempt("r-1", deadlineReport)
	r.tick()
	require.Equal(r.t, 2, r.attemptOf("r-1"), "the deadline's attempt is reworked by the failed rule: attempt 2")
	require.Equal(r.t, 2, sprint.AttemptsSince(r.snap().Work.Card("r-1")), "a deadline counts as an attempt")

	r.brokenRead("r-1", "internal/y/b.go:40 still calls the old name, outside PATHS; rename the call too")
	r.tick()
	require.Equal(r.t, sprint.Ready, r.snap().Work.Card("r-1").Col, "widened in place by the read-broken rule")
	require.Contains(r.t, r.snap().Work.Card("r-1").F("brief"), "internal/y/b.go", "PATHS widened by rule")
	r.tick()
	require.Equal(r.t, 3, r.attemptOf("r-1"), "the widened brief's attempt is dealt: attempt 3")
	require.Equal(r.t, 3, sprint.AttemptsSince(r.snap().Work.Card("r-1")), "a rule's widen is no decision: the count stands")

	r.brokenRead("r-1", "internal/x/a.go:12 drops the error from Close; return it")
	r.tick()
	require.Equal(r.t, 4, r.attemptOf("r-1"), "the finding inside PATHS is reworked by rule: attempt 4")
	require.Equal(r.t, 4, sprint.AttemptsSince(r.snap().Work.Card("r-1")), "a rule's rework is no decision: the count stands")

	r.failedAttempt("r-1", "tests red: TestX fails on the new branch")
	r.tick()
}

// noFifthAttempt asserts the card is the coordinator's: in review under exactly one attempt-cap
// judgment naming max_attempts, and twenty ticks of the machine (its deal, its rules, its
// friends' deal) deal it no attempt past n.
func (r *conflictRig) noAttemptPast(id string, n int) {
	r.t.Helper()
	pr := r.snap().Work.Card(id)
	require.Equal(r.t, sprint.Review, pr.Col, "%s waits for the coordinator in review", id)
	require.Equal(r.t, n, pr.Int("attempt"), "attempt %d is its last", n)
	open := r.openOnCard(sprint.NBriefWrong, id)
	require.Len(r.t, open, 1, "exactly one attempt-cap judgment: %+v", open)
	assert.Contains(r.t, open[0].Note.What, "brief defect after "+strconv.Itoa(sprint.AttemptsSince(pr))+" attempts (max_attempts "+strconv.Itoa(r.snapRoutes().AttemptsCap("r"))+")", "the judgment names the attempts and the cap")
	assert.Contains(r.t, open[0].Note.What, "findings: attempt ", "and the attempts' history")
	assert.Equal(r.t, sprint.Decisions[sprint.NBriefWrong], open[0].Note.Decisions, "its decisions are the coordinator's: carry, brief, re-tier, drop")
	for range 20 {
		r.tick()
	}
	assert.Nil(r.t, r.snap().Fleet.Card(sprint.WorkCardID(id, n+1)), "no attempt %d is dealt without a decision", n+1)
	assert.Equal(r.t, n, r.attemptOf(id), "the card stays at attempt %d", n)
	assert.Len(r.t, r.openOnCard(sprint.NBriefWrong, id), 1, "the judgment is raised once, never again while it holds")
	assert.Equal(r.t, sprint.Review, r.snap().Work.Card(id).Col)
}

// T1 and T4 of the card: with no max_attempts in nova-config the cap is 4; a card whose four
// attempts ended as a deadline, a finding outside PATHS (widened by rule), a finding inside
// PATHS (reworked by rule) and failed work is one judgment after the fourth, and no fifth
// attempt is dealt. Red before the fix: the deadline did not count and the widen reset the
// count, so the failed rule redealt attempt 5.
func TestACardsFourthAttemptRaisesOneJudgmentAndNoFifthIsDealt(t *testing.T) {
	t.Parallel()
	r := newConflictRig(t)
	r.readCard()
	require.Equal(t, sprint.AttemptsDefault, r.snapRoutes().AttemptsCap("r"), "no max_attempts applied: the default")
	r.fourAttemptsEndEveryWay()
	r.noAttemptPast("r-1", 4)
	r.clean("at the attempt cap")
}

// T3 of the card: the coordinator's brief opens a fresh window of max_attempts. After the
// judgment the seat corrects the brief in place; attempts 5 to 8 run (a finding inside PATHS,
// failed work, another finding, a landing refused in the shared ledgers alone, each answered
// by the machine) and the second judgment comes at 8; no attempt 9 without a decision. Red
// before the fix: a landing refused in the ledgers alone was exempt from the cap and the card
// was reworked at the tip for attempt 9.
func TestACoordinatorsDecisionOpensAFreshWindowOfAttempts(t *testing.T) {
	t.Parallel()
	r := newConflictRig(t)
	r.readCard()
	r.fourAttemptsEndEveryWay()
	require.Len(t, r.openOnCard(sprint.NBriefWrong, "r-1"), 1)

	corrected := "c: the change (r) tier: flash\nREPO: mas-bandwidth/nova-tools\nPATHS: internal/x/a.go,internal/y/b.go\n\nThe task, corrected by the coordinator after four attempts.\n"
	r.must(store.BriefStep(sprint.BriefReq{ID: "r-1", Brief: corrected, Who: "coordinator"}))
	pr := r.snap().Work.Card("r-1")
	assert.Equal(t, "4", pr.F(sprint.FieldDecidedAttempt), "the brief is the coordinator's decision at attempt 4")
	assert.Equal(t, 0, sprint.AttemptsSince(pr), "the count starts again")
	assert.Empty(t, r.openOnCard(sprint.NBriefWrong, "r-1"), "the judgment is answered by the decision")
	r.tick()
	require.Equal(t, 5, r.attemptOf("r-1"), "attempt 5 is dealt after the decision")

	r.brokenRead("r-1", "internal/x/a.go:30 the corrected task still misses the nil check")
	r.tick()
	require.Equal(t, 6, r.attemptOf("r-1"))
	r.failedAttempt("r-1", "build broken: internal/x/a.go:31: undefined: y")
	r.tick()
	require.Equal(t, 7, r.attemptOf("r-1"))
	r.brokenRead("r-1", "internal/y/b.go:44 the renamed call drops its second return")
	r.tick()
	require.Equal(t, 8, r.attemptOf("r-1"))
	r.failedAttempt("r-1", "build broken: internal/y/b.go:45: missing return")
	r.tick()
	r.noAttemptPast("r-1", 8)
}

// T2 of the card: the number comes from nova-config's sprint row. sprint:max_attempts "2" bounds
// a card at 2; a stream's own `stream set --attempts 6` holds over it; "x" in Redis is the
// default, 4. Red before the fix: the sprint read the work table's attempts property and
// nova-config's key was not read at all.
func TestMaxAttemptsFromNovaConfigBoundsTheCap(t *testing.T) {
	t.Parallel()
	r := newConflictRig(t)
	r.readCard()
	r.m.SetMaxAttempts("2")
	require.Equal(t, 2, r.snapRoutes().AttemptsCap("r"), "nova-config's max_attempts is the sprint's cap")

	r.brokenRead("r-1", "internal/x/a.go:12 drops the error from Close; return it")
	r.tick()
	require.Equal(t, 2, r.attemptOf("r-1"), "below the cap the read-broken rule reworks it")
	r.failedAttempt("r-1", "tests red: TestX fails on the new branch")
	r.tick()
	r.noAttemptPast("r-1", 2)

	r.must(store.SetStep(sprint.SetReq{Streams: []string{"r"}, Attempts: "6", Who: "coordinator"}))
	assert.Equal(t, 6, r.snapRoutes().AttemptsCap("r"), "a stream's own cap holds over nova-config's")
	assert.Equal(t, 2, r.snapRoutes().AttemptsCap("s1"), "the other streams keep nova-config's")
	r.m.SetMaxAttempts("x")
	assert.Equal(t, sprint.AttemptsDefault, r.snapRoutes().AttemptsCap("s1"), "an unparseable max_attempts is the default")
	r.m.SetMaxAttempts("")
	assert.Equal(t, sprint.AttemptsDefault, r.snapRoutes().AttemptsCap("s1"), "an absent max_attempts is the default")
	r.m.SetMaxAttempts("0")
	assert.Equal(t, sprint.AttemptsDefault, r.snapRoutes().AttemptsCap("s1"), "a cap under 1 is the default")
}

// T5 of the card: the sprint-wide set --attempts is retired, one source and no twin: the step
// is refused naming nova-config's verb and what the sprint reads now; a stream's own stays.
func TestSetAttemptsIsRefusedNamingNovaConfig(t *testing.T) {
	t.Parallel()
	r := newConflictRig(t)
	res, err := r.st.Run(r.ctx, store.SetStep(sprint.SetReq{Attempts: "3", Who: "coordinator"}))
	require.NoError(t, err)
	require.Len(t, res.Refused, 1, "set --attempts is refused")
	assert.Contains(t, res.Refused[0].Why, "nova-config sprint set --max_attempts <n>")
	assert.Contains(t, res.Refused[0].Why, "max-attempts 4 (the default", "it says what the sprint reads now")
	assert.Equal(t, sprint.AttemptsDefault, r.snapRoutes().AttemptsCap("s1"), "nothing was changed")
	r.m.SetMaxAttempts("7")
	res, err = r.st.Run(r.ctx, store.SetStep(sprint.SetReq{Attempts: "3", Who: "coordinator"}))
	require.NoError(t, err)
	require.Len(t, res.Refused, 1)
	assert.Contains(t, res.Refused[0].Why, "max-attempts 7 (nova-config sprint:max_attempts)")
	r.must(store.SetStep(sprint.SetReq{Streams: []string{"s1"}, Attempts: "3", Who: "coordinator"}))
	assert.Equal(t, 3, r.snapRoutes().AttemptsCap("s1"), "a stream's own cap is still set here")
}
