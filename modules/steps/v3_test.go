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

func TestV3R50ConsumerUnionRoomFormula(t *testing.T) {
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

	// Withdraw p1.w1 in the store, but keep p1.w1@0 in SprintConsumers (winding down on bench b1)
	// AND include p2.w1@0 in SprintConsumers to verify @epoch deduplication with working p2.w1!
	ep := s.snap().Epoch
	wPlan, r := card.MoveConsumer(card.FromStore(s.card("p1.w1")), card.FromStore(s.card("p1")), []card.Card{card.FromStore(s.card("p1.w1"))}, s.cardRow(s.snap().Rows["b1"]), card.Withdraw{Reason: "released: test", Stream: card.Stream{Name: "s1"}})
	require.Nil(t, r)
	s.move(wPlan, nil)
	s.must("hold-p1", store.SetCardState{Card: "p1", State: "waiting"}, store.SetCardReason{Card: "p1", Reason: "held: pause p1"})
	s.must("beat-winding-down", store.SetRowLoad{
		Row:   "b1",
		Tiers: "pro",
		Resources: store.RowResources{
			Total:           map[string]int{"cpu": 12, "memory": 16},
			HeldSprint:      map[string]int{"cpu": 2, "memory": 12}, // stale beat before p2 grabbed hold
			SprintConsumers: []string{"p1.w1@" + itoa(ep), "p2.w1@" + itoa(ep)},
		},
	})

	// Under R50 union: C_active = {p1.w1, p2.w1} -> cpu = 2+6 = 8, memory = 12+2 = 14.
	// Only 2 GiB memory is free, so p3 (needs 4 GiB) MUST stay ready (max() would have seen 12 GiB and over-committed to 18 GiB!).
	s.tick()
	require.Empty(t, s.rejected)
	s.requireState("p3", "ready")

	// Now p1.w1 exits; HeldOther takes 2 GiB memory, SprintConsumers has only p2.w1@ep -> used memory = 2 + 2 = 4, free = 12 >= 4.
	s.must("beat-cleared", store.SetRowLoad{
		Row:   "b1",
		Tiers: "pro",
		Resources: store.RowResources{
			Total:           map[string]int{"cpu": 12, "memory": 16},
			HeldSprint:      map[string]int{"cpu": 6, "memory": 2},
			HeldOther:       map[string]int{"cpu": 1, "memory": 2},
			SprintConsumers: []string{"p2.w1@" + itoa(ep)},
		},
	})
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

	// Mark b1 as AnyPaused (e.g., paused person/sprint hold behind CI).
	s.must("beat-paused", store.SetRowLoad{
		Row: "b1", Tiers: "pro",
		Resources: store.RowResources{
			Total:     map[string]int{"cpu": 8, "memory": 16},
			AnyPaused: true,
		},
	})
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
	s.must("beat-unpaused", store.SetRowLoad{
		Row: "b1", Tiers: "pro",
		Resources: store.RowResources{
			Total:     map[string]int{"cpu": 8, "memory": 16},
			AnyPaused: false,
		},
	})
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

	// Put CI load on both benches so neither is idle: b1 has 7/8 mem used (1/8 = 0.125 free),
	// b2 has 4/8 cpu and 4/8 mem used (0.5 free), f1 has 1 working consumer (1/2 = 0.5 free, after b2? f1 is idle initially!).
	s.must("b1-load", store.SetRowLoad{Row: "b1", Tiers: "pro", Resources: store.RowResources{HeldOther: map[string]int{"cpu": 1, "memory": 7}}})
	s.must("b2-load", store.SetRowLoad{Row: "b2", Tiers: "pro", Resources: store.RowResources{HeldOther: map[string]int{"cpu": 4, "memory": 4}}})

	s.producer("p1", "s1", "pro", "a/1")
	s.producer("p2", "s1", "pro", "a/2")
	s.tick() // resolve p1, p2
	p := s.tick()
	require.Empty(t, s.rejected)

	// Questions.Width must ask ONLY for kin row f1, never b1 or b2!
	require.Len(t, capAsk.last.Width, 1)
	assert.Equal(t, "f1", capAsk.last.Width[0].Name)

	// p1 picks f1 first because f1 is idle (used == 0); then f1 has frac 1/2 = 0.5, b2 has frac 4/8 = 0.5, b1 has bottleneck 1/8 = 0.125.
	// Between b2 (0.5) and f1 (0.5), alphabetical name picks "b2" before "f1" (and never "b1" at 0.125!).
	assert.Equal(t, "f1", s.card("p1.w1").Holder)
	assert.Equal(t, "b2", s.card("p2.w1").Holder)

	// Next tick nudges both unacknowledged consumers: f1 gets width nudge, b2 gets resource nudge!
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

		// Blocker needs cpu 4, memory 8: freeing pcrit (2, 8) + plow (2, 1) suffices (4, 9 >= 4, 8), sparing phigh!
		s.must("add-blk", store.PutCard{
			ID: "blk", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "blocker",
			Brief: "tier: pro\nRESOURCES: cpu 4, memory 8\n\nblk", Paths: []string{"a/4"},
		})
		s.tick() // resolve blk
		p := s.tick()
		require.Empty(t, s.rejected)
		s.requireState("blk", "working")
		s.requireState("plow.w1", "fail")
		s.requireState("pcrit.w1", "fail")
		s.requireState("phigh.w1", "working") // spared by backward minimal pruning!
		var stopped []string
		for _, e := range p.Edges {
			if e.Kind == "stop" {
				stopped = append(stopped, e.Consumer)
			}
		}
		slices.Sort(stopped)
		assert.Equal(t, []string{"pcrit.w1", "plow.w1"}, stopped)
	})

	t.Run("HeldOther CI guard prevents futile victim kills", func(t *testing.T) {
		t.Parallel()
		s := newSim(t)
		s.stream("s1")
		s.bench("b1", []string{"linux"}, map[string]int{"cpu": 8, "memory": 16}, "pro")
		s.producer("p1", "s1", "pro", "a/1")
		s.run(2)
		s.take("p1.w1")

		// CI holds 6 of 8 CPU on b1; blocker needs 4 CPU, so even killing p1.w1 only leaves 2 CPU!
		s.must("ci-hold", store.SetRowLoad{
			Row: "b1", Tiers: "pro",
			Resources: store.RowResources{HeldOther: map[string]int{"cpu": 6, "memory": 4}},
		})
		s.must("add-blk", store.PutCard{
			ID: "blk", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "blocker",
			Brief: "tier: pro\nRESOURCES: cpu 4, memory 4\n\nblk", Paths: []string{"a/2"},
		})
		s.run(2)
		require.Empty(t, s.rejected)
		s.requireState("p1.w1", "working") // NOT killed!
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
		s.take("p2.w1") // p2 is younger, but higher priority than p1!
		s.must("add-blk", store.PutCard{ID: "blk", Kind: "producer", Stream: "s1", Tier: "pro", Priority: "blocker", Brief: "blk", Paths: []string{"a/3"}})
		s.run(2)
		require.Empty(t, s.rejected)
		s.requireState("blk", "working")
		s.requireState("p1.w1", "fail")    // lowest priority preempted
		s.requireState("p2.w1", "working") // higher priority spared
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

	// p1.w1 was paused behind CI for 500s (500_000 ms).
	s.must("paused-work", store.SetRowLoad{
		Row: "b1", Tiers: "pro",
		Resources: store.RowResources{PausedMillis: map[string]int64{"p1.w1@" + itoa(ep): 500_000}},
	})
	limit := Engine.Timeouts.ReadyTake + Engine.Timeouts.Consumer["pro"]
	s.now = dealt.Add(seconds(limit + 200)) // 200s past nominal bound, but 300s under paused-adjusted bound!
	s.tick()
	s.requireState("p1.w1", "working")

	s.now = dealt.Add(seconds(limit + 505))
	s.tick()
	s.requireState("p1.w1", "fail")

	// Now verify landing_stall subtracts PausedMillis on a review consumer.
	s.tick() // re-deal p1.w2
	s.requireState("p1.w2", "working")
	s.report("p1.w2", "ok", sha(2), "done")
	s.must("land-stamp", store.PutCard{ID: "p0", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "p0"},
		store.SetCardState{Card: "p0", State: "landed"})
	s.tick() // deal read p1.r1.1 to b2 (or b1)
	readHolder := s.card("p1.r1.1").Holder
	s.ack("p1.r1.1")
	s.fold()
	landedAt := s.v.Landed["s1"]
	s.must("paused-read", store.SetRowLoad{
		Row: readHolder, Tiers: s.snap().Rows[readHolder].Tiers,
		Resources: store.RowResources{PausedMillis: map[string]int64{"p1.r1.1@" + itoa(ep): 40_000}},
	})
	s.now = landedAt.Add(time.Duration(Engine.Timeouts.LandingStall+20) * time.Second) // 80s - 40s = 40s < 60s
	s.tick()
	assert.Empty(t, s.openJudgements("stall"), "PausedMillis excluded from landing_stall")

	s.now = landedAt.Add(time.Duration(Engine.Timeouts.LandingStall+45) * time.Second) // 105s - 40s = 65s >= 60s
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
		s.must("paused", store.SetRowLoad{
			Row: "b1", Tiers: "pro",
			Resources: store.RowResources{Total: map[string]int{"cpu": 8, "memory": 16}, AnyPaused: true},
		})
		s.must("add",
			store.PutCard{ID: "p0", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "p0"},
			store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "p1", Needs: []string{"p0"}},
		)
		s.run(2)
		assert.Empty(t, s.openJudgements("idle"), "AnyPaused suppresses fleet idle alarm")
	})

	t.Run("suppressed when ready card waits on busy exclusive resource", func(t *testing.T) {
		t.Parallel()
		s := newSim(t)
		s.stream("s1")
		s.bench("b1", []string{"linux"}, map[string]int{"cpu": 32, "memory": 64, "ps5": 1}, "pro")
		s.must("add",
			store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "tier: pro\nRESOURCES: cpu 2, memory 4, ps5\n\np1", Paths: []string{"a/1"}},
			store.PutCard{ID: "p2", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "tier: pro\nRESOURCES: cpu 2, memory 4, ps5\n\np2", Paths: []string{"a/2"}},
		)
		s.tick() // resolve p1, p2 (resolved non-empty; p1 not yet working)
		// Clear any initial resolve-tick idle and set b1 beat showing 2/32 CPU (6.25% < 50%), ps5 held by p1.w1.
		s.tick() // p1 starts on b1 (holds ps5 1); p2 stays ready waiting on ps5 1!
		s.must("beat", store.SetRowLoad{
			Row: "b1", Tiers: "pro",
			Resources: store.RowResources{
				Total:           map[string]int{"cpu": 32, "memory": 64, "ps5": 1},
				HeldSprint:      map[string]int{"cpu": 2, "memory": 4, "ps5": 1},
				SprintConsumers: []string{"p1.w1"},
			},
		})
		for _, id := range s.openJudgements("idle") {
			s.must("exp", store.ExpireJudgement{ID: id, Reason: "clear"})
		}
		s.tick()
		s.requireState("p1.w1", "working")
		s.requireState("p2", "ready")
		assert.Empty(t, s.openJudgements("idle"), "ready card waiting on busy ps5 suppresses idle even at 6% CPU")
	})
}
