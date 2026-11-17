package steps

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureQuestionsAsker struct {
	inner Asker
	last  Questions
}

func (a *captureQuestionsAsker) Ask(q Questions) Answers {
	a.last = q
	return a.inner.Ask(q)
}

func TestV3R50ConsumerUnionAndSoftClaimRoomFormula(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.bench("b1", []string{"linux"}, map[string]int{"cpu": 12, "memory": 16}, "pro")

	// p1 needs 2 cpu, 12 GiB memory; p2 needs 6 cpu, 2 GiB memory; p3 needs 1 cpu, 4 GiB memory.
	s.must("add",
		store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Score: 1, Brief: "tier: pro\nRESOURCES: cpu 2, memory 12\n\ndo p1", Paths: []string{"a/1"}},
		store.PutCard{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro", Score: 2, Brief: "tier: pro\nRESOURCES: cpu 6, memory 2\n\ndo p2", Paths: []string{"a/2"}},
		store.PutCard{ID: "p3", Kind: "producer", Stream: "s1", Tier: "pro", Score: 3, Brief: "tier: pro\nRESOURCES: cpu 1, memory 4\n\ndo p3", Paths: []string{"a/3"}},
	)
	s.tick() // resolve all three
	s.tick() // deal p1 (mem 12) and p2 (mem 2) -> 14/16 GiB used; p3 (mem 4) waits!
	require.Empty(t, s.rejected)
	s.requireState("p1.w1", "working")
	s.requireState("p2.w1", "working")
	s.requireState("p3", "ready")

	// Withdraw p1.w1 in the store, while p1.w1@epoch is still holding its lease in nova-reserve
	// AND p2.w1@epoch is also holding its lease (verifying @epoch deduplication and max(declared, live_held)).
	ep := s.snap().Epoch
	wPlan, r := card.MoveConsumer(card.FromStore(s.card("p1.w1")), card.FromStore(s.card("p1")), []card.Card{card.FromStore(s.card("p1.w1"))}, s.cardRow(s.snap().Rows["b1"]), card.Withdraw{Reason: "released: test", Stream: card.Stream{Name: "s1"}})
	require.Nil(t, r)
	s.move(wPlan, nil)
	s.must("hold-p1", store.SetCardState{Card: "p1", State: "waiting"}, store.SetCardReason{Card: "p1", Reason: "held: pause p1"})
	s.reserve["b1"] = BenchReserve{
		SprintHolding: map[string]map[string]int{
			"p1.w1@" + itoa(ep): {"cpu": 2, "memory": 12},
			"p2.w1@" + itoa(ep): {"cpu": 6, "memory": 2},
		},
	}

	// Under R50 union: C_sprint = {p1.w1, p2.w1} -> cpu = 2+6 = 8, memory = 12+2 = 14.
	// Only 2 GiB memory is free, so p3 (needs 4 GiB) MUST stay ready.
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p3", "ready")

	// Place a soft person claim for 14 GiB memory while p2.w1 (2 GiB) is still running ->
	// total used+claimed = 16 GiB (or even 18 GiB over-claim): running p2.w1 is NEVER killed,
	// and p3 still waits until the soft claim releases!
	s.reserve["b1"] = BenchReserve{
		NonSprintUsed: map[string]int{"cpu": 8, "memory": 15},
		SprintHolding: map[string]map[string]int{"p2.w1@" + itoa(ep): {"cpu": 6, "memory": 2}},
	}
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p2.w1", "working")
	s.requireState("p3", "ready")

	// Soft claim releases, leaving only 2 GiB external hold + p2.w1 (2 GiB) -> free memory = 12 >= 4!
	s.reserve["b1"] = BenchReserve{
		NonSprintUsed: map[string]int{"cpu": 1, "memory": 2},
		SprintHolding: map[string]map[string]int{"p2.w1@" + itoa(ep): {"cpu": 6, "memory": 2}},
	}
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p3", "working")
	assert.Equal(t, "b1", s.card("p3.w1").Holder)
}

func TestV3ReadUsesDefaultReadNeedsNoTags(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.bench("m1", []string{"mac"}, map[string]int{"cpu": 8, "memory": 32, "ps5": 1}, "pro")
	s.bench("l1", []string{"linux"}, map[string]int{"cpu": 1, "memory": 1}, "pro", "heavy")
	s.must("add", store.PutCard{
		ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro",
		Brief: "tier: pro\nRESOURCES: mac, ps5, cpu 8, memory 32\n\ndo p1", Paths: []string{"a/x"},
	})
	s.run(2)
	s.requireState("p1.w1", "working")
	assert.Equal(t, "m1", s.card("p1.w1").Holder, "work consumer needs mac, ps5, cpu 8, memory 32")

	s.take("p1.w1")
	s.report("p1.w1", "ok", sha(1), "done")
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p1", "review")
	s.requireState("p1.r1.1", "working")
	assert.Equal(t, "l1", s.card("p1.r1.1").Holder, "read consumer needs only cpu 1, memory 1, zero tags")
}

func TestV3R52NoDealOrPreemptBehindAnyPaused(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	capAsk := &captureQuestionsAsker{inner: benchAsk{}}
	s.ask = capAsk
	s.stream("s1")
	s.bench("b1", []string{"linux"}, map[string]int{"cpu": 8, "memory": 16}, "pro")
	s.producer("p1", "s1", "pro", "a/1")
	s.run(2)
	s.take("p1.w1")

	// Mark b1 as AnyPaused in live nova-reserve state (displaced behind CI).
	s.reserve["b1"] = BenchReserve{AnyPaused: true}
	s.producer("p2", "s1", "pro", "a/2")
	s.producer("blk", "s1", "pro", "a/3")
	s.must("prio", store.SetCardPriority{Card: "blk", Priority: "blocker"})
	s.run(2)
	require.Empty(t, s.rejected)
	s.requireState("p2", "ready")
	s.requireState("blk", "ready")
	s.requireState("p1.w1", "working")
	require.NotEmpty(t, capAsk.last.Rows)
	for _, rf := range capAsk.last.Rows[0].Candidates {
		if rf.Name == "b1" {
			assert.Equal(t, map[string]int{"cpu": 0, "memory": 0}, rf.Free, "AnyPaused bench exposes 0 free resources")
		}
	}

	// Clear AnyPaused: both blk and p2 now deal onto b1 without preemption because b1 has 7 free cpu.
	s.reserve["b1"] = BenchReserve{AnyPaused: false}
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("blk", "working")
	s.requireState("p2", "working")
}

func TestV3RankedBottleneckFractionWidthKinOnlyAndNudge(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	capAsk := &captureQuestionsAsker{inner: benchAsk{}}
	s.ask = capAsk
	s.stream("s1")
	s.bench("b1", []string{"linux"}, map[string]int{"cpu": 8, "memory": 8}, "pro")
	s.bench("b2", []string{"linux"}, map[string]int{"cpu": 8, "memory": 8}, "pro")
	s.kin("f1", "friend/pro", 2, "pro")

	// Live non-sprint load on both benches so neither is idle: b1 has 7/8 mem used (0.125 free),
	// b2 has 4/8 cpu and 4/8 mem used (0.5 free), f1 is idle initially.
	s.reserve["b1"] = BenchReserve{NonSprintUsed: map[string]int{"cpu": 1, "memory": 7}}
	s.reserve["b2"] = BenchReserve{NonSprintUsed: map[string]int{"cpu": 4, "memory": 4}}

	s.producer("p1", "s1", "pro", "a/1")
	s.producer("p2", "s1", "pro", "a/2")
	s.tick() // resolve p1, p2
	p := s.tick()
	require.Empty(t, s.rejected)

	// Questions.Width must ask ONLY for kin row f1, never b1 or b2!
	require.Len(t, capAsk.last.Width, 1)
	assert.Equal(t, "f1", capAsk.last.Width[0].Name)

	assert.Equal(t, "f1", s.card("p1.w1").Holder)
	assert.Equal(t, "b2", s.card("p2.w1").Holder)

	p = s.tick()
	require.Len(t, p.Nudges, 2)
	byRow := map[string]string{}
	for _, nd := range p.Nudges {
		byRow[nd.Row] = nd.Line
	}
	assert.Contains(t, byRow["f1"], "and your width is 2. Please work to width!")
	assert.Contains(t, byRow["b2"], "(cpu 3/8 free, memory 3/8 GiB free).")
	assert.False(t, strings.Contains(byRow["b2"], "width"))
}

func TestV3R53MultiVictimPreemptionAndGuards(t *testing.T) {
	t.Parallel()
	t.Run("multi-victim minimal lowest-priority pruning", func(t *testing.T) {
		t.Parallel()
		s := newSim(t)
		s.stream("s1")
		s.bench("b1", []string{"linux"}, map[string]int{"cpu": 6, "memory": 10}, "pro")
		s.must("add",
			store.PutCard{ID: "plow", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "low", Score: 1, Brief: "tier: pro\nRESOURCES: cpu 2, memory 1\n\nlow", Paths: []string{"a/1"}},
			store.PutCard{ID: "phigh", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "high", Score: 2, Brief: "tier: pro\nRESOURCES: cpu 2, memory 1\n\nhigh", Paths: []string{"a/2"}},
			store.PutCard{ID: "pcrit", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "critical", Score: 3, Brief: "tier: pro\nRESOURCES: cpu 2, memory 8\n\ncrit", Paths: []string{"a/3"}},
		)
		s.run(2)
		s.take("plow.w1")
		s.take("phigh.w1")
		s.take("pcrit.w1")

		s.must("add-blk", store.PutCard{
			ID: "blk", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "blocker",
			Brief: "tier: pro\nRESOURCES: cpu 4, memory 8\n\nblk", Paths: []string{"a/4"},
		})
		s.tick()
		p := s.tick()
		require.Empty(t, s.rejected)
		s.requireState("blk", "working")
		s.requireState("plow.w1", "fail")
		s.requireState("pcrit.w1", "fail")
		s.requireState("phigh.w1", "working")
		var stopped []string
		for _, e := range p.Edges {
			if e.Kind == "stop" {
				stopped = append(stopped, e.Consumer)
			}
		}
		slices.Sort(stopped)
		assert.Equal(t, []string{"pcrit.w1", "plow.w1"}, stopped)
	})

	t.Run("NonSprintUsed CI/claim guard prevents futile victim kills", func(t *testing.T) {
		t.Parallel()
		s := newSim(t)
		s.stream("s1")
		s.bench("b1", []string{"linux"}, map[string]int{"cpu": 8, "memory": 16}, "pro")
		s.producer("p1", "s1", "pro", "a/1")
		s.run(2)
		s.take("p1.w1")

		// CI or person soft claim holds 6 of 8 CPU on b1; blocker needs 4 CPU -> futile!
		s.reserve["b1"] = BenchReserve{NonSprintUsed: map[string]int{"cpu": 6, "memory": 4}}
		s.must("add-blk", store.PutCard{
			ID: "blk", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "blocker",
			Brief: "tier: pro\nRESOURCES: cpu 4, memory 4\n\nblk", Paths: []string{"a/2"},
		})
		s.run(2)
		require.Empty(t, s.rejected)
		s.requireState("p1.w1", "working")
		s.requireState("blk", "ready")
	})

	t.Run("friend row preempts lowest-priority consumer", func(t *testing.T) {
		t.Parallel()
		s := newSim(t)
		s.stream("s1")
		s.kin("f1", "friend/pro", 2, "pro")
		s.must("add",
			store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "low", Brief: "low", Paths: []string{"a/1"}},
			store.PutCard{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "high", Brief: "high", Paths: []string{"a/2"}},
		)
		s.run(2)
		s.take("p1.w1")
		s.take("p2.w1")
		s.must("add-blk", store.PutCard{ID: "blk", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "blocker", Brief: "blk", Paths: []string{"a/3"}})
		s.run(2)
		require.Empty(t, s.rejected)
		s.requireState("blk", "working")
		s.requireState("p1.w1", "fail")
		s.requireState("p2.w1", "working")
	})
}

func TestV3R39bPausedMillisExcludesWorkingMaxAndLandingStall(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.bench("b1", []string{"linux"}, map[string]int{"cpu": 4, "memory": 8}, "pro")
	s.bench("b2", []string{"linux"}, map[string]int{"cpu": 4, "memory": 8}, "pro", "heavy")
	s.producer("p1", "s1", "pro", "a/1")
	s.run(2)
	s.ack("p1.w1")
	s.fold()
	dealt := s.v.TakenAt["p1.w1"]
	ep := s.snap().Epoch

	s.reserve["b1"] = BenchReserve{PausedMillis: map[string]int64{"p1.w1@" + itoa(ep): 500_000}}
	limit := Engine.Timeouts.ReadyTake + Engine.Timeouts.Consumer["pro"]
	s.now = dealt.Add(seconds(limit + 200))
	s.tick()
	s.requireState("p1.w1", "working")

	s.now = dealt.Add(seconds(limit + 505))
	s.tick()
	s.requireState("p1.w1", "fail")

	s.tick()
	s.requireState("p1.w2", "working")
	s.report("p1.w2", "ok", sha(2), "done")
	s.must("land-stamp", store.PutCard{ID: "p0", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "p0"},
		store.SetCardState{Card: "p0", State: "landed"})
	s.tick()
	readHolder := s.card("p1.r1.1").Holder
	s.ack("p1.r1.1")
	s.fold()
	landedAt := s.v.Landed["s1"]
	s.reserve[readHolder] = BenchReserve{PausedMillis: map[string]int64{"p1.r1.1@" + itoa(ep): 40_000}}
	s.now = landedAt.Add(time.Duration(Engine.Timeouts.LandingStall+20) * time.Second)
	s.tick()
	assert.Empty(t, s.openJudgements("stall"), "PausedMillis excluded from landing_stall")

	s.now = landedAt.Add(time.Duration(Engine.Timeouts.LandingStall+45) * time.Second)
	s.tick()
	assert.Equal(t, []string{"stall.s1.1"}, s.openJudgements("stall"))
}

func TestV3R54IdleUtilizationAndSuppression(t *testing.T) {
	t.Parallel()
	t.Run("suppressed when open bench has AnyPaused", func(t *testing.T) {
		t.Parallel()
		s := newSim(t)
		s.stream("s1")
		s.bench("b1", []string{"linux"}, map[string]int{"cpu": 8, "memory": 16}, "pro")
		s.reserve["b1"] = BenchReserve{AnyPaused: true}
		s.must("add",
			store.PutCard{ID: "p0", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "p0"},
			store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "p1", Needs: []string{"p0"}},
		)
		s.run(2)
		assert.Empty(t, s.openJudgements("idle"), "AnyPaused suppresses fleet idle alarm")
	})

	t.Run("suppressed when ready card waits on busy exclusive resource or soft claim", func(t *testing.T) {
		t.Parallel()
		s := newSim(t)
		s.stream("s1")
		s.bench("b1", []string{"linux"}, map[string]int{"cpu": 32, "memory": 64, "ps5": 1}, "pro")
		s.must("add",
			store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "tier: pro\nRESOURCES: cpu 2, memory 4, ps5\n\np1", Paths: []string{"a/1"}},
			store.PutCard{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "tier: pro\nRESOURCES: cpu 2, memory 4, ps5\n\np2", Paths: []string{"a/2"}},
		)
		s.tick()
		s.tick()
		s.reserve["b1"] = BenchReserve{
			SprintHolding:    map[string]map[string]int{"p1.w1": {"cpu": 2, "memory": 4, "ps5": 1}},
			ActiveClaimedCPU: 2,
		}
		for _, id := range s.openJudgements("idle") {
			s.must("exp", store.ExpireJudgement{ID: id, Reason: "clear"})
		}
		s.tick()
		s.requireState("p1.w1", "working")
		s.requireState("p2", "ready")
		assert.Empty(t, s.openJudgements("idle"), "ready card waiting on busy ps5 suppresses idle even at 6% CPU")
	})
}
