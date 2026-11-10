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

// TestTickRowHoldAndDown: a hand left on a row that is held, down or not
// hooked in (an op of an older release closed the row and left its cards,
// live 2026-11-10: aurora unhooked and held with 15 cards) goes back to the
// queue on the next tick, taken and untaken alike, each released with the
// row's reason, no grace (Glenn, 2026-11-10: "Aurora is down, but she still
// has cards. This is forbidden.").
func TestTickRowHoldAndDown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, why string
		close     func(s *sim)
	}{
		{"held", "held", func(s *sim) { s.move(card.MoveRow(s.cardRow(s.snap().Rows["r1"]), card.Hold{Reason: "rest"})) }},
		{"down", "down", func(s *sim) { s.move(card.MoveRow(s.cardRow(s.snap().Rows["r1"]), card.Gone{})) }},
		{"unhooked", "not hooked in", func(s *sim) { s.must("unhook", store.PutRow{Name: "r1"}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newSim(t)
			s.stream("s1")
			s.row("r1", 2, "pro")
			s.producer("p1", "s1", "pro", "a/x")
			s.producer("p2", "s1", "pro", "a/y")
			s.run(2)
			s.take("p1.w1")
			s.requireState("p2.w1", "working")
			tc.close(s)
			s.requireState("p1.w1", "working") // the hand the old op left
			s.tick()
			require.Empty(t, s.rejected)
			for _, id := range []string{"p1.w1", "p2.w1"} {
				s.requireState(id, "fail")
				assert.Equal(t, card.ReleasedPrefix+card.Returned("r1", tc.why), s.card(id).Reason, id)
				assert.Empty(t, s.card(id).Result, "released, no blame: %s", id)
			}
			s.requireState("p1", "ready")
			s.requireState("p2", "ready")
		})
	}
}

// TestRowHoldAndGoneReturnTheHandInTheirOp: the coordinator's hold of a row
// and the tick's Gone of a silent one return its whole hand as they read it,
// taken and untaken, in the same op that closes the row (a card dealt after
// that read goes back on the next tick: TestTickRowHoldAndDown).
func TestRowHoldAndGoneReturnTheHandInTheirOp(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) *sim {
		s := newSim(t)
		s.stream("s1")
		s.row("r1", 2, "pro")
		s.producer("p1", "s1", "pro", "a/x")
		s.producer("p2", "s1", "pro", "a/y")
		s.run(2)
		s.take("p1.w1")
		s.requireState("p2.w1", "working")
		return s
	}
	t.Run("hold", func(t *testing.T) {
		t.Parallel()
		s := setup(t)
		rc, ref := s.verb(Request{Actor: "rowan", Coordinators: []string{"rowan"}, Row: "r1", Event: card.Hold{Reason: "rest"}})
		require.Nil(t, ref)
		require.True(t, rc.Accepted, rc.Reason)
		assert.True(t, s.snap().Rows["r1"].Held)
		for _, id := range []string{"p1.w1", "p2.w1"} {
			s.requireState(id, "fail")
			assert.Equal(t, card.ReleasedPrefix+card.Returned("r1", "held"), s.card(id).Reason, id)
		}
		s.requireState("p1", "ready")
		s.requireState("p2", "ready")
	})
	t.Run("gone", func(t *testing.T) {
		t.Parallel()
		s := setup(t)
		s.fold()
		in := s.inputs()
		in.Seen["r1"] = in.Now.Add(-seconds(Engine.Timeouts.RowSilentGone))
		plan := Tick(s.v, in, benchAsk{})
		plan.Ops = tickOnly(plan.Ops)
		require.Len(t, plan.Ops, 1)
		rc, err := s.f.Apply(t.Context(), plan.Ops[0])
		require.NoError(t, err)
		require.True(t, rc.Accepted, rc.Reason)
		assert.Equal(t, "down", s.snap().Rows["r1"].State)
		for _, id := range []string{"p1.w1", "p2.w1"} {
			s.requireState(id, "fail")
			assert.Equal(t, card.ReleasedPrefix+card.Returned("r1", "down"), s.card(id).Reason, id)
		}
	})
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
		s.report(fmt.Sprintf("p1.w%d", n), "fail", sha(n), "broke") // each pushes a head: progress, so only the bounce limit holds it
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

// TestTickNoProgressHolds: once escalation is spent (frontier_after
// bounces before the run), same_finding work attempts running that fail
// with no new head on the same brief hold the producer with a held
// judgement, whatever the reasons say: the same brief dealt again at the top
// rank fails again (live 2026-11-09). Below the top rank the card escalates
// as before. A fault or a withdraw with no head neither counts nor breaks
// the run; a fail that pushes a head breaks it; an unhold is a fresh run.
func TestTickNoProgressHolds(t *testing.T) {
	t.Parallel()
	s := noProgressSim(t)
	n, top := Engine.Limits.SameFinding, Engine.Limits.FrontierAfter
	require.Equal(t, [2]int{2, 4}, [2]int{n, top}, "the default policy's same_finding and frontier_after")
	end := func(c, result, head, reason string) {
		s.requireState("p1", "working")
		s.take(c)
		s.report(c, result, head, reason)
		s.tick()
	}
	end("p1.w1", "fail", "", "make failed")
	end("p1.w2", "fail", "", "make failed") // two running, below the top rank: escalated, not held
	end("p1.w3", "fail", sha(7), "pushed a partial fix")
	end("p1.w4", "fail", sha(8), "pushed more") // progress; 4 bounces: the top rank from here
	end("p1.w5", "fail", "", "tests still red")
	end("p1.w6", "fault", "", "exit 137") // a fault spends nothing and breaks nothing
	s.requireState("p1", "working")
	assert.Empty(t, s.openJudgements("held"))
	end("p1.w7", "fail", "", "tests still red")
	s.requireState("p1", "waiting")
	assert.Equal(t, fmt.Sprintf("held: failed %d top-rank attempts running with no new head on the same brief, the limit is %d", n, n), s.snap().Cards["p1"].Reason)
	js := s.openJudgements("held")
	require.Equal(t, []string{"held.p1.1"}, js)
	s.run(3)
	s.requireState("p1", "waiting")
	rc, ref := s.verb(Request{Actor: "rowan", Judgement: js[0], Answer: "unhold", Card: "p1", Event: card.UnholdCard{Reason: "one more try"}})
	require.Nil(t, ref)
	require.True(t, rc.Accepted, rc.Reason)
	s.run(3)
	assert.Empty(t, s.openJudgements("held"), "an unhold is a fresh run: the same fails do not hold it again")
	end("p1.w8", "fail", "", "still red")
	assert.Empty(t, s.openJudgements("held"), "one fail since the unhold")
	end("p1.w9", "fail", "", "still red")
	s.requireState("p1", "waiting")
	assert.Equal(t, []string{"held.p1.2"}, s.openJudgements("held"))
}

// noProgressSim is p1 dealt on r1 under the engine's limits with the default
// policy's bounce limit, 12, so the bounce limit is not reached first.
func noProgressSim(t *testing.T) *sim {
	s := newSim(t)
	l := Engine.Limits
	l.Bounces = 12
	s.ask = scripted{Answers{Version: 1, Models: benchModels(), Limits: &l}}
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.run(2)
	return s
}

// TestTickNoProgressFromTheStart: a producer that pushes nothing from its
// first attempt is held once same_finding of its no-head fails were dealt at
// the top rank, at frontier_after + same_finding bounces, not at the bounce
// limit: the attempts below the top rank were escalation's, and do not count.
func TestTickNoProgressFromTheStart(t *testing.T) {
	t.Parallel()
	s := noProgressSim(t)
	at := Engine.Limits.FrontierAfter + Engine.Limits.SameFinding
	for i := 1; i < at; i++ {
		s.requireState("p1", "working")
		s.take(fmt.Sprintf("p1.w%d", i))
		s.report(fmt.Sprintf("p1.w%d", i), "fail", "", "red")
		s.tick()
		assert.Empty(t, s.openJudgements("held"), "w%d: one top-rank fail at most", i)
	}
	s.take(fmt.Sprintf("p1.w%d", at))
	s.report(fmt.Sprintf("p1.w%d", at), "fail", "", "red")
	s.tick()
	s.requireState("p1", "waiting")
	assert.Equal(t, at, s.snap().Cards["p1"].Bounces)
	assert.Equal(t, []string{"held.p1.1"}, s.openJudgements("held"))
}

// TestTickNoProgressEditedBrief: an edit of the brief's paths between two
// no-head fails ends the run: the brief is no longer the same.
func TestTickNoProgressEditedBrief(t *testing.T) {
	t.Parallel()
	s := noProgressSim(t)
	end := func(c, head string) {
		s.requireState("p1", "working")
		s.take(c)
		s.report(c, "fail", head, "red")
		s.tick()
	}
	for i := 1; i <= Engine.Limits.FrontierAfter; i++ {
		end(fmt.Sprintf("p1.w%d", i), sha(i)) // progress up to the top rank
	}
	s.take("p1.w5")
	s.report("p1.w5", "fail", "", "PATHS too narrow")
	rc, ref := s.verb(Request{Actor: "rowan", Card: "p1", Event: card.Edit{Paths: []string{"a/x", "a/y"}, Reason: "widened"}})
	require.Nil(t, ref)
	require.True(t, rc.Accepted, rc.Reason)
	s.tick()
	end("p1.w6", "")
	assert.Empty(t, s.openJudgements("held"), "the edit ended the run")
	end("p1.w7", "")
	assert.Equal(t, []string{"held.p1.1"}, s.openJudgements("held"))
}

// TestTickBudgetAndCarry: results over the budget are carried in arrival
// order, nothing lost, through in ceil(B/N) ticks (BudgetKept, CarryFIFO).
func TestTickBudgetAndCarry(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.ask = scripted{Answers{Version: 1, Models: benchModels(), Limits: &Limits{Bounces: 6, Faults: 3, SameFinding: 2, ReportsPerTick: 1000, RouteRestWindow: 10, RouteRestAfter: 3, EscalateAfter: 2, FrontierAfter: 4, FlapLimit: 3, IdleFloor: 0.5, LandTarget: 0.9, Budget: 1, Pack: 1}}}
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
	s.requireState("p1.w1", "fail") // its hand goes back with the Gone
	s.requireState("p1", "ready")
	s.must("seen", store.SetRowState{Row: "r1", State: "up"})
	s.run(1)
	s.take("p1.w2")
	s.fold()
	in = s.inputs()
	in.Seen["r1"] = in.Now.Add(-seconds(Engine.Timeouts.RowSilentWithdraw))
	plan = Tick(s.v, in, benchAsk{})
	plan.Ops = tickOnly(plan.Ops)
	rc, err = s.f.Apply(t.Context(), plan.Ops[0])
	require.NoError(t, err)
	require.True(t, rc.Accepted, rc.Reason)
	s.requireState("p1.w2", "fail")
	s.requireState("p1", "ready")
	assert.Contains(t, s.card("p1.w2").Reason, "branch sprint/s1/p1 is kept")
	s.must("flaps", store.SetRowFlaps{Row: "r1", Flaps: 3})
	s.tick()
	assert.Equal(t, []string{"flap.r1.3"}, s.openJudgements("flap"))
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

// TestTickRefusedAloneLeftOut: a group the store refused alone on the state
// the next tick sees again (its cards at the same gens: no race, a defect)
// is left out with one engine judgement carrying the store's reason, and
// stays out while that judgement is open and its cards do not change
// (LAYER-3 tick step 7); it is never resent every tick.
func TestTickRefusedAloneLeftOut(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.fold()
	plan := Tick(s.v, s.inputs(), benchAsk{})
	plan.Ops = tickOnly(plan.Ops)
	require.Len(t, plan.Ops, 1, "p1's resolve")
	s.refused = []RefusedOp{{ID: plan.Ops[0].ID, Reason: "a store rule the steps did not pre-check", Groups: OpGroups(plan.Ops[0])}}
	s.tick()
	require.Empty(t, s.rejected)
	assert.Empty(t, s.tickOps(), "left out, not resent")
	js := s.openJudgements("engine")
	require.Len(t, js, 1)
	assert.Contains(t, s.snap().Judgements[js[0]].Question, "a store rule the steps did not pre-check")
	s.tick()
	assert.Empty(t, s.tickOps(), "still left out while the judgement is open and p1 is unchanged")
	s.requireState("p1", "waiting")
}

// TestTickRefusedAloneClearsWhenItsCardChanges: once a card the left-out
// group read moves, the engine judgement's cause has cleared: it expires,
// and the group is planned again on the new state.
func TestTickRefusedAloneClearsWhenItsCardChanges(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	s.fold()
	plan := Tick(s.v, s.inputs(), benchAsk{})
	plan.Ops = tickOnly(plan.Ops)
	s.refused = []RefusedOp{{ID: plan.Ops[0].ID, Reason: "refused", Groups: OpGroups(plan.Ops[0])}}
	s.tick()
	require.Len(t, s.openJudgements("engine"), 1)
	s.must("touch p1", store.SetCardReason{Card: "p1", Reason: "edited"}) // p1's gen moves
	s.tick()
	assert.Empty(t, s.openJudgements("engine"), "its cause cleared")
	s.requireState("p1", "ready")
}

// policyLimits is a policy version answering limits l and nothing else of
// note: the steps must read each validated limit from it, not the engine's.
func policyLimits(l Limits) scripted {
	return scripted{Answers{Version: 1, Models: benchModels(), Limits: &l}}
}

// TestTickLimitsAreReadFromPolicy: every validated limit a step uses is
// policy's answer (flap_limit, same_finding, reports_per_tick here;
// route_rest_window and escalate_after below), the engine's only the
// default when limits() fails.
func TestTickLimitsAreReadFromPolicy(t *testing.T) {
	t.Parallel()
	l := Engine.Limits
	l.FlapLimit, l.SameFinding, l.ReportsPerTick = 1, 1, 1
	t.Run("flap_limit", func(t *testing.T) {
		t.Parallel()
		s := newSim(t)
		s.ask = policyLimits(l)
		s.row("r1", 1, "pro")
		s.must("flap", store.SetRowFlaps{Row: "r1", Flaps: 1})
		s.tick()
		assert.Len(t, s.openJudgements("flap"), 1, "one flap at a limit of 1")
	})
	t.Run("same_finding", func(t *testing.T) {
		t.Parallel()
		s := newSim(t)
		s.ask = policyLimits(l)
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
		assert.Len(t, s.openJudgements("brief"), 1, "the first finding at a same_finding of 1")
	})
	t.Run("reports_per_tick", func(t *testing.T) {
		t.Parallel()
		s := newSim(t)
		s.row("r1", 1, "pro")
		s.fold()
		in := s.inputs()
		in.Inbox = []InboxItem{{ID: "a", Kind: "report", Row: "r1", Consumer: "nope"}, {ID: "b", Kind: "report", Row: "r1", Consumer: "nope"}}
		plan := Tick(s.v, in, policyLimits(l))
		assert.Len(t, plan.Answers, 1, "one report a tick")
		require.Len(t, plan.Carry, 1)
		assert.Equal(t, "b", plan.Carry[0].ID)
	})
}

// TestTickRouteRestWindowFromPolicy: the rest counts faults among the row's
// last route_rest_window finishes: three faults then an ok rest the row
// under a window of 10, not under a window of 3 (two faults in it).
func TestTickRouteRestWindowFromPolicy(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000, 0)
	v := &View{Rows: map[string]store.Row{}, Ends: map[string][]End{"r1": {{now, true}, {now, true}, {now, true}, {now, false}}}}
	for _, window := range []int{10, 3} {
		tk := newTick(v, Inputs{Now: now})
		tk.lim, tk.tmo = Engine.Limits, Engine.Timeouts
		tk.lim.RouteRestWindow = window
		tk.rest()
		_, rested := tk.rested["r1"]
		assert.Equal(t, window == 10, rested, "window %d", window)
	}
}

// TestTickRestingRowIsNotDealtTo: after route_rest_after faults on its only
// serving row, the next tick deals the producer nowhere (the rest is a deal
// filter, LPM R34) and opens unserved; the card stays ready.
func TestTickRestingRowIsNotDealtTo(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	for i := 1; i <= Engine.Limits.RouteRestAfter; i++ {
		s.run(3)
		id := fmt.Sprintf("p1.w%d", i)
		s.take(id)
		s.report(id, "fault", "", "the harness crashed")
	}
	s.tick()
	next := fmt.Sprintf("p1.w%d", Engine.Limits.RouteRestAfter+1)
	_, dealt := s.snap().Cards[next]
	assert.False(t, dealt, "%s was dealt to resting row r1", next)
	s.requireState("p1", "ready")
	open := s.openJudgements("unserved")
	require.Len(t, open, 1)
	assert.Contains(t, s.snap().Judgements[open[0]].Question, "resting after faults, dealt to again when the rest ends: r1", "the question names the resting row")
	// The rest holds: the judgement stays open (a resting row does not clear
	// it), with no new episode, and nothing is dealt.
	s.tick()
	s.tick()
	assert.Equal(t, open, s.openJudgements("unserved"), "unserved closed or reopened while r1 rests")
	_, dealt = s.snap().Cards[next]
	assert.False(t, dealt, "%s was dealt to resting row r1", next)
}

// TestTickRowsNamingRestingRowIsNoMiss: policy names a row it was offered
// that rests after faults; the engine deals to another serving row and no
// policy miss is opened.
func TestTickRowsNamingRestingRowIsNoMiss(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.ask = scripted{Answers{Version: 1, Models: benchModels(), Rows: map[string][]string{"p1": {"r1"}}}}
	s.stream("s1")
	s.row("r1", 1, "pro")
	s.row("r2", 1, "pro")
	s.producer("p1", "s1", "pro", "a/x")
	for i := 1; i <= Engine.Limits.RouteRestAfter; i++ {
		s.run(3)
		id := fmt.Sprintf("p1.w%d", i)
		require.Equal(t, "r1", s.card(id).Holder, id)
		s.take(id)
		s.report(id, "fault", "", "the harness crashed")
	}
	s.run(3)
	next := fmt.Sprintf("p1.w%d", Engine.Limits.RouteRestAfter+1)
	assert.Equal(t, "r2", s.card(next).Holder, "r1 rests: the engine deals to r2")
	for _, id := range s.openJudgements("policy") {
		assert.NotContains(t, s.snap().Judgements[id].Question, "rows(p1)", "%s: naming a resting row it was offered is no policy miss", id)
	}
}

// TestTickEscalateAfterFromPolicy: escalation's bounces are policy's.
func TestTickEscalateAfterFromPolicy(t *testing.T) {
	t.Parallel()
	v := &View{Rows: map[string]store.Row{}}
	tk := newTick(v, Inputs{})
	tk.lim = Engine.Limits
	tk.lim.EscalateAfter, tk.lim.FrontierAfter = 1, 2
	c := store.Card{ID: "p1", Kind: "producer", Tier: "flash", Bounces: 1}
	assert.Equal(t, tierIndex("flash")+1, tk.desired(c, "work"), "one up at 1 bounce")
	c.Bounces = 2
	assert.Equal(t, tk.topRank(), tk.desired(c, "work"), "the top at 2")
}
