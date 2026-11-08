package steps

import (
	"fmt"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTickPreemption: a ready blocker with every serving row at width
// preempts the youngest working consumer on one row in one op with its
// Start, a stop request rides with the op, and the victim's producer is
// ready again with its branch kept (BlockerPreemptsInOneTick,
// PreemptYoungest, EdgeAfterAccepted).
func TestTickPreemption(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "a/y")
	s.run(2)
	s.take("p1.w1")
	s.take("p2.w1")
	s.producer("b", "s1", "pro", "a/z")
	s.must("priority", store.SetCardPriority{Card: "b", Priority: "blocker"})
	s.tick()
	s.requireState("b", "ready")
	p := s.tick()
	require.Empty(t, s.rejected)
	require.Len(t, s.tickOps(), 1, "the preemption is one op")
	s.requireState("p2.w1", "fail")
	s.requireState("p2", "ready")
	assert.Contains(t, s.card("p2.w1").Reason, "preempted by blocker b")
	assert.Equal(t, "sprint/s1/p2", s.card("p2.w1").Branch, "the branch is kept")
	s.requireState("b", "working")
	require.Len(t, p.Edges, 1)
	assert.Equal(t, "stop", p.Edges[0].Kind)
	assert.Equal(t, "p2.w1", p.Edges[0].Consumer)
	assert.Equal(t, s.tickOps()[0].ID, p.Edges[0].After, "the stop goes out only with its op")
}

// TestTickPreemptionRefusedByRace: the victim reports between the fold and
// the tick; the op is refused whole, nothing half-applies, the stop request
// is not dispatched, and the next tick deals the blocker to the width the
// report freed (RefusalChangesNothing, NeverHalfApplied).
func TestTickPreemptionRefusedByRace(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	s.take("p1.w1")
	s.producer("b", "s1", "pro", "a/z")
	s.must("priority", store.SetCardPriority{Card: "b", Priority: "blocker"})
	s.tick()
	s.requireState("b", "ready")
	s.fold()
	in := s.inputs()
	s.report("p1.w1", "ok", sha(1), "done") // the race, after the fold
	plan := Tick(s.v, in, benchAsk{})
	plan.Ops = tickOnly(plan.Ops)
	require.NotEmpty(t, plan.Ops)
	rc, err := s.f.Apply(t.Context(), plan.Ops[0])
	require.NoError(t, err)
	require.False(t, rc.Accepted, "the fence on p1.w1's gen refuses the op")
	s.requireState("p1", "review")
	s.requireState("b", "ready")
	assert.Len(t, plan.Edges, 1, "the stop is in the plan, waiting on the op")
	s.refused = []RefusedOp{{ID: plan.Ops[0].ID, Reason: rc.Reason, Groups: OpGroups(plan.Ops[0])}}
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("b", "working")
	assert.Empty(t, s.inflight, "no stop went out")
}

// TestTickDropTakesDependants: a failed need fails the closure of its
// dependants in one group, their live consumers withdrawn first (LESSONS
// 2.4), and a sentinel with it.
func TestTickDropTakesDependants(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "a/y")
	s.must("add", store.PutCard{ID: "p3", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p3", Needs: []string{"p1"}, Paths: []string{"a/z"}})
	s.must("add", store.PutCard{ID: "p4", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "do p4", Needs: []string{"p3"}, Paths: []string{"a/w"}})
	s.must("add", store.PutCard{ID: "g", Kind: "sentinel", Stream: "s1", Tier: "pro", Needs: []string{"p4", "p2"}})
	s.move(s.plan("p1", card.Cancel{Reason: "dropped"}))
	s.tick()
	require.Empty(t, s.rejected)
	require.Len(t, s.tickOps(), 1, "one op: the closure, with p2's Resolve")
	s.tick()
	s.take("p2.w1")
	for _, id := range []string{"p3", "p4", "g"} {
		s.requireState(id, "fail")
	}
	assert.Equal(t, "need p1 failed", s.card("p3").Reason)
	assert.Equal(t, "need p3 failed", s.card("p4").Reason)
	s.requireState("p2", "working")
	s.requireState("p2.w1", "working")
}

// TestTickHolds: a stream hold parks what it names and the tick parks what
// it missed; a held row's untaken consumers are withdrawn at once, a down
// row's after the grace; working consumers stay; the hold never touches a
// landing.
func TestTickHolds(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.row("r2", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "a/y")
	s.producer("p3", "s1", "pro", "a/z")
	s.run(2)
	s.take("p1.w1")
	s.take("p2.w1")
	s.requireState("p3.w1", "working")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	s.take("p1.r1.1")
	s.report("p1.r1.1", "ok", sha(1), "fine")
	s.run(2)
	req := s.request("land", "s1")
	hold, r := card.MoveStream(card.StreamFromStore(s.snap().Streams["s1"]), card.HoldStream{Reason: "pause", Seq: s.snap().Seq})
	s.move(hold, r)
	s.must("park missed", store.SetCardState{Card: "p3", State: "ready"}, store.SetCardState{Card: "p3.w1", State: "fail"}) // a ready producer the hold's op missed
	s.land(req)
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p3", "waiting")
	assert.Contains(t, s.card("p3").Reason, "held")
	s.requireState("p1", "landed")
}

// TestTickRowHoldAndDown: a held row's unacknowledged consumers are
// withdrawn at once, a down row's after the grace; acknowledged ones stay.
func TestTickRowHoldAndDown(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "a/y")
	s.run(2)
	s.take("p1.w1")
	s.requireState("p2.w1", "working")
	s.move(card.MoveRow(s.cardRow(s.snap().Rows["r1"]), card.Hold{Reason: "rest"}))
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1.w1", "working")
	s.requireState("p2.w1", "fail")
	s.requireState("p2", "ready")
	assert.Equal(t, "row r1 is held; redealt", s.card("p2.w1").Reason)
	s.move(card.MoveRow(s.cardRow(s.snap().Rows["r1"]), card.Unhold{Reason: "back"}))
	s.run(1)
	s.requireState("p2.w2", "working")
	s.move(card.MoveRow(s.cardRow(s.snap().Rows["r1"]), card.Gone{}))
	s.tick()
	s.requireState("p2.w2", "working")
	s.now = s.now.Add(seconds(Engine.Timeouts.RowSilentGone))
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p2.w2", "fail")
	s.requireState("p1.w1", "working")
	assert.Equal(t, "row r1 is down; redealt", s.card("p2.w2").Reason)
}

// TestTickHoldAtLimit: a producer at the bounce limit is held (waiting with
// the hold's reason, never dealt) with its judgement in the same op; a fault
// never counts.
func TestTickHoldAtLimit(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	for i := 1; i < Engine.Limits.Bounces; i++ {
		s.take(fmt.Sprintf("p1.w%d", i))
		s.report(fmt.Sprintf("p1.w%d", i), "fail", "", "broke")
		s.tick()
		s.requireState("p1", "working")
	}
	s.take(fmt.Sprintf("p1.w%d", Engine.Limits.Bounces))
	s.report(fmt.Sprintf("p1.w%d", Engine.Limits.Bounces), "fault", "", "exit 137")
	s.tick()
	s.requireState("p1", "working")
	s.take(fmt.Sprintf("p1.w%d", Engine.Limits.Bounces+1))
	s.report(fmt.Sprintf("p1.w%d", Engine.Limits.Bounces+1), "fail", "", "broke again")
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "waiting")
	assert.Equal(t, fmt.Sprintf("held: bounced %d times, the limit is %d", Engine.Limits.Bounces, Engine.Limits.Bounces), s.snap().Cards["p1"].Reason)
	js := s.openJudgements("held")
	require.Equal(t, []string{"held.p1.1"}, js)
	s.run(3)
	s.requireState("p1", "waiting")
	assert.True(t, s.snap().Judgements["held.p1.1"].Deadline.IsZero(), "held has no deadline")
}

// TestTickUnholdFreshBudget: the coordinator's unhold of a producer held at
// the bounce limit gives it a fresh budget: the next ticks deal it rather
// than hold it again, a further limit's worth of bounces holds it again, and
// its total bounces stay as counted (live 2026-11-08: unhold was re-held on
// the next tick, the only way out drop and re-add).
func TestTickUnholdFreshBudget(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	limit, n := Engine.Limits.Bounces, 0
	bounce := func() {
		n++
		s.requireState("p1", "working")
		s.take(fmt.Sprintf("p1.w%d", n))
		s.report(fmt.Sprintf("p1.w%d", n), "fail", "", "broke")
		s.tick()
	}
	for range limit {
		bounce()
	}
	s.requireState("p1", "waiting")
	require.Equal(t, []string{"held.p1.1"}, s.openJudgements("held"))
	rc, ref := s.verb(Request{Actor: "rowan", Judgement: "held.p1.1", Answer: "unhold", Card: "p1", Event: card.UnholdCard{Reason: "reviewed"}})
	require.Nil(t, ref)
	require.True(t, rc.Accepted, rc.Reason)
	s.run(3)
	require.Empty(t, s.rejected)
	assert.Empty(t, s.openJudgements("held"), "an unhold is not held again by the same bounces")
	assert.Equal(t, limit, s.snap().Cards["p1"].Bounces, "the total stays as counted")
	for range limit - 1 {
		bounce()
	}
	assert.Empty(t, s.openJudgements("held"), "a bounce short of a fresh limit")
	bounce()
	s.requireState("p1", "waiting")
	assert.Equal(t, fmt.Sprintf("held: bounced %d times since its last unhold, the limit is %d", limit, limit), s.snap().Cards["p1"].Reason)
	assert.Equal(t, 2*limit, s.snap().Cards["p1"].Bounces)
	assert.Equal(t, []string{"held.p1.2"}, s.openJudgements("held"))
}

// TestTickBudgetAndCarry: results over the budget are carried in arrival
// order, nothing lost, through in ceil(B/N) ticks (BudgetKept, CarryFIFO).
func TestTickBudgetAndCarry(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.ask = scripted{Answers{Version: 1, Models: benchModels(), Limits: &Limits{Bounces: 6, Faults: 3, SameFinding: 2, ReportsPerTick: 1000, RouteRestWindow: 10, RouteRestAfter: 3, Budget: 1, Pack: 1}}}
	s.row("r1", 4, "pro")
	s.row("r2", 4, "pro", "heavy")
	for i := 1; i <= 3; i++ {
		s.stream(fmt.Sprintf("s%d", i))
		s.producer(fmt.Sprintf("p%d", i), fmt.Sprintf("s%d", i), "pro", "a/x")
	}
	s.tick()
	assert.Equal(t, []string{"ready", "waiting", "waiting"}, []string{s.state("p1"), s.state("p2"), s.state("p3")}, "budget 1: the first non-blocker group goes, the rest wait")
	s.run(8)
	for i := 1; i <= 3; i++ {
		s.take(fmt.Sprintf("p%d.w1", i))
		s.report(fmt.Sprintf("p%d.w1", i), "ok", sha(i), "done")
	}
	s.run(4)
	for i := 1; i <= 3; i++ {
		s.take(fmt.Sprintf("p%d.r1.1", i))
		s.report(fmt.Sprintf("p%d.r1.1", i), "ok", sha(i), "fine")
	}
	s.run(8)
	for i := 1; i <= 3; i++ {
		s.land(s.request("land", fmt.Sprintf("s%d", i)))
	}
	p := s.tick()
	require.Len(t, s.tickOps(), 1, "budget 1: one landing")
	require.Len(t, p.Carry, 2, "two carried")
	assert.Equal(t, []string{"r2", "r3"}, []string{p.Carry[0].ID, p.Carry[1].ID}, "in arrival order")
	s.requireState("p1", "landed")
	s.requireState("p2", "merging")
	p = s.tick()
	require.Len(t, p.Carry, 1)
	assert.Equal(t, "r3", p.Carry[0].ID)
	s.tick()
	for i := 1; i <= 3; i++ {
		s.requireState(fmt.Sprintf("p%d", i), "landed")
	}
}

// TestTickRefusedOpSplitByGroup: an op of two groups refused by a race is
// split by group next tick: the unraced group lands alone, the raced one
// is planned again from the folded view.
func TestTickRefusedOpSplitByGroup(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.stream("s2")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s2", "pro", "a/y")
	s.fold()
	in := s.inputs()
	s.must("cancel p2", store.SetCardState{Card: "p2", State: "fail"}, store.SetCardReason{Card: "p2", Reason: "dropped"}) // the race
	plan := Tick(s.v, in, benchAsk{})
	plan.Ops = tickOnly(plan.Ops)
	require.Len(t, plan.Ops, 1, "both resolves packed in one op")
	rc, err := s.f.Apply(t.Context(), plan.Ops[0])
	require.NoError(t, err)
	require.False(t, rc.Accepted)
	s.requireState("p1", "waiting")
	s.refused = []RefusedOp{{ID: plan.Ops[0].ID, Reason: rc.Reason, Groups: OpGroups(plan.Ops[0])}}
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "ready")
	assert.Len(t, s.tickOps(), 1)
}

// TestTickUnlistedRow: a store row the roster lacks is dealt nothing and is
// a judgement (Glenn, 2026-11-06: nova-config is the one source).
func TestTickUnlistedRow(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	delete(s.roster.Workers, "r1")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(3)
	s.requireState("p1", "ready")
	assert.Equal(t, []string{"roster.r1.1"}, s.openJudgements("roster"))
	assert.Equal(t, "row r1 is not in nova-config; nothing is dealt to it until nova-config lists it", s.snap().Judgements["roster.r1.1"].Question)
	assert.Equal(t, []string{"unserved.pro.1"}, s.openJudgements("unserved"))
}

// TestTickUnhookedRow: a row up on the bus but not hooked in to the sprint
// is never dealt to (Glenn, 2026-11-06); hooking in opens it.
func TestTickUnhookedRow(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.must("unhook", store.PutRow{Name: "r1"})
	s.producer("p1", "s1", "pro", "a/x")
	s.run(3)
	s.requireState("p1", "ready")
	assert.Equal(t, []string{"unserved.pro.1"}, s.openJudgements("unserved"))
	s.must("hook", store.PutRow{Name: "r1", Session: "hook-r1", Role: "worker"})
	s.run(2)
	s.requireState("p1", "working")
	assert.Empty(t, s.openJudgements("unserved"), "the cause cleared")
}

// TestTickSilentRow: a row unseen past the withdraw bound has its working
// consumers withdrawn with the branch kept; past the gone bound it goes
// down; a flap judgement at the limit.
func TestTickSilentRow(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	s.take("p1.w1")
	s.fold()
	in := s.inputs()
	in.Seen["r1"] = in.Now.Add(-seconds(Engine.Timeouts.RowSilentGone))
	plan := Tick(s.v, in, benchAsk{})
	plan.Ops = tickOnly(plan.Ops)
	require.Len(t, plan.Ops, 1)
	gs := OpGroups(plan.Ops[0])
	require.Len(t, gs, 1)
	assert.Equal(t, "r1", gs[0].Row, "Gone claims the row")
	rc, err := s.f.Apply(t.Context(), plan.Ops[0])
	require.NoError(t, err)
	require.True(t, rc.Accepted, rc.Reason)
	assert.Equal(t, "down", s.snap().Rows["r1"].State)
	s.requireState("p1.w1", "working")
	s.fold()
	in = s.inputs()
	in.Seen["r1"] = in.Now.Add(-seconds(Engine.Timeouts.RowSilentWithdraw))
	plan = Tick(s.v, in, benchAsk{})
	plan.Ops = tickOnly(plan.Ops)
	rc, err = s.f.Apply(t.Context(), plan.Ops[0])
	require.NoError(t, err)
	require.True(t, rc.Accepted, rc.Reason)
	s.requireState("p1.w1", "fail")
	s.requireState("p1", "ready")
	assert.Contains(t, s.card("p1.w1").Reason, "branch sprint/s1/p1 is kept")
	s.must("flaps", store.SetRowFlaps{Row: "r1", Flaps: 3})
	s.tick()
	assert.Equal(t, []string{"flap.r1.3"}, s.openJudgements("flap"))
}

// TestTickSilentRowUnseenSinceStart (finding 1): a row the run has not
// seen since its (re)start is silent from the view's start (Since), so a
// bench whose session died before a restart goes Gone past the gone bound
// and has its acknowledged working consumers withdrawn past the withdraw
// bound, its producer dealt again (tla/Steps.tla SilenceActs, witness
// StepsSilentUnseen).
func TestTickSilentRowUnseenSinceStart(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.row("r2", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.away = set{"r2": {}}
	s.run(2)
	s.take("p1.w1")
	require.Equal(t, "r1", s.card("p1.w1").Holder)
	// the run restarts: a new view built now, and r1's session is gone
	s.fold()
	s.v = Build(s.snap(), s.now)
	s.away = set{"r1": {}}
	s.now = s.now.Add(seconds(Engine.Timeouts.RowSilentGone))
	s.tick()
	assert.Equal(t, "down", s.snap().Rows["r1"].State, "Gone past the gone bound, measured from the restart")
	s.requireState("p1.w1", "working")
	s.now = s.now.Add(seconds(Engine.Timeouts.RowSilentWithdraw))
	s.tick()
	s.requireState("p1.w1", "fail")
	assert.Contains(t, s.card("p1.w1").Reason, "row r1 silent for")
	s.tick()
	s.requireState("p1", "working")
	assert.Equal(t, "r2", s.card("p1.w2").Holder, "dealt again to the row that is there")
}

// TestTickStableOpIDs: the same view and inputs name the same ops.
func TestTickStableOpIDs(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.fold()
	in := s.inputs()
	a, b := Tick(s.v, in, benchAsk{}), Tick(s.v, in, benchAsk{})
	require.Len(t, a.Ops, len(b.Ops))
	for i := range a.Ops {
		assert.Equal(t, a.Ops[i].ID, b.Ops[i].ID)
	}
	assert.Regexp(t, `^t1\.\d+\.[0-9a-f]{16}$`, a.Ops[0].ID)
	_ = time.Second
}

// TestDealRowsAnswerIsTheSet (finding 2): policy's rows answer is the set a
// card may go to (LAYER-5-POLICY.md rows), never a preference: with no
// room in it the card waits, outside it nothing is dealt, and no policy
// judgement opens, since the answer was in domain; the undealt judgement
// names the answer (tla/Steps.tla RowFor, RowsAnswerKept, witness
// StepsRowsPreference).
func TestDealRowsAnswerIsTheSet(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.row("r2", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.producer("p2", "s1", "pro", "a/y")
	s.ask = scripted{Answers{Version: 1, Models: benchModels(), Rows: map[string][]string{"p1": {"r1"}, "p2": {"r1"}}}}
	s.tick() // resolve
	s.tick() // the deal
	s.requireState("p1", "working")
	assert.Equal(t, "r1", s.card("p1.w1").Holder)
	s.requireState("p2", "ready")
	s.run(4)
	s.requireState("p2", "ready")
	for _, id := range s.openJudgements("policy") {
		assert.NotContains(t, id, "rows", "an answer in domain is no miss")
	}
	und := s.openJudgements("undealt")
	require.Equal(t, []string{"undealt.p2.1"}, und)
	assert.Contains(t, s.snap().Judgements[und[0]].Question, "policy's rows answer (r1) names no row with room")
}

// TestDealReadsKeepTheRowsAnswer (finding 2): a review owed more reads than
// the rows answer names rows with room gets reads only on those rows; the
// rest are not topped up from outside the answer.
func TestDealReadsKeepTheRowsAnswer(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.row("r2", 1, "pro")
	s.row("r3", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.ask = scripted{Answers{Version: 1, Models: benchModels(), Reads: map[string]int{"p1": 2}, Rows: map[string][]string{"p1": {"r1"}}}}
	s.run(2)
	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.ask = scripted{Answers{Version: 1, Models: benchModels(), Reads: map[string]int{"p1": 2}, Rows: map[string][]string{"p1": {"r2"}}}}
	s.tick()
	var reads []string
	for _, c := range s.snap().Cards {
		if c.Of == "p1" && c.Kind == "read" {
			reads = append(reads, c.Holder)
		}
	}
	assert.Equal(t, []string{"r2"}, reads, "two reads owed, one row in the answer: one read")
}
