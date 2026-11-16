package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// TestV3BenchCandidatesAndCardResources: width(row) is asked only for kin rows;
// bench candidates expose tags, resources and R50/R52 free (with no width
// attribute); card exposes tags and resources across unworded() while stripping
// prose; and prefer() orders benches with multi-resource room ahead of busy ones.
func TestV3BenchCandidatesAndCardResources(t *testing.T) {
	t.Parallel()
	ro := roster.Roster{
		Workers: map[string]roster.Worker{
			"b1": {
				Name:      "b1",
				Tiers:     []string{"pro"},
				Tags:      []string{"amd64", "gpu", "linux"},
				Resources: map[string]int{"cpu": 8, "memory": 32, "ps5": 1},
				Harness:   roster.WorkerHarness,
			},
			"b2": {
				Name:      "b2",
				Tiers:     []string{"pro"},
				Tags:      []string{"amd64", "gpu", "linux"},
				Resources: map[string]int{"cpu": 8, "memory": 32, "ps5": 1},
				Harness:   roster.WorkerHarness,
			},
			"b3": {
				Name:      "b3",
				Tiers:     []string{"pro"},
				Tags:      []string{"amd64", "gpu", "linux"},
				Resources: map[string]int{"cpu": 8, "memory": 32, "ps5": 1},
				Harness:   roster.WorkerHarness,
			},
		},
		Kin: map[string]roster.Kin{
			"k1": {Name: "k1", Model: "m-pro", Harness: "claude", Width: 2, Tiers: []string{"pro"}},
		},
	}
	// On b1: p-wind.w1 was withdrawn (State == "fail") while still winding down in
	// SprintConsumers ("p-wind.w1@1", needing memory 28), AND p-act.w1 is newly
	// working (needing cpu 4, memory 2) and ALREADY in SprintConsumers ("p-act.w1@1").
	// Exact R50 union:
	//   sprint_used(b1) = (cpu 1 + 4 = 5, memory 28 + 2 = 30) + HeldOther(cpu 1, memory 1)
	//   free(b1) = {cpu: 2, memory: 1, ps5: 1} -> NOT roomy for p1 (needs cpu 4, memory 8, ps5 1)!
	// On b2: idle -> free(b2) = {cpu: 8, memory: 32, ps5: 1} -> roomy!
	// On b3: AnyPaused == true -> free(b3) = {cpu: 0, memory: 0, ps5: 0} -> NOT roomy!
	snap := store.Snapshot{
		Epoch:   1,
		Streams: map[string]store.Stream{"s1": {Name: "s1"}},
		Rows: map[string]store.Row{
			"b1": {
				Name: "b1", State: "up", Session: "s-b1", Role: "worker",
				Resources: store.RowResources{
					HeldOther:       map[string]int{"cpu": 1, "memory": 1},
					SprintConsumers: []string{"p-wind.w1@1", "p-act.w1@1"},
				},
			},
			"b2": {Name: "b2", State: "up", Session: "s-b2", Role: "worker"},
			"b3": {
				Name: "b3", State: "up", Session: "s-b3", Role: "worker",
				Resources: store.RowResources{AnyPaused: true},
			},
			"k1": {Name: "k1", State: "up", Session: "s-k1", Role: "worker"},
		},
		Cards: map[string]store.Card{
			"p-wind":    {ID: "p-wind", Kind: "producer", Stream: "s1", State: "ready", Tier: "pro", Brief: "pro\nRESOURCES: memory 28\n\nold"},
			"p-wind.w1": {ID: "p-wind.w1", Kind: "work", Of: "p-wind", Stream: "s1", State: "fail", Tier: "pro", Holder: "b1", Brief: "pro\nRESOURCES: memory 28\n\nold"},
			"p-act":     {ID: "p-act", Kind: "producer", Stream: "s1", State: "working", Tier: "pro", Brief: "pro\nRESOURCES: cpu 4, memory 2\n\nact"},
			"p-act.w1":  {ID: "p-act.w1", Kind: "work", Of: "p-act", Stream: "s1", State: "working", Tier: "pro", Holder: "b1", Brief: "pro\nRESOURCES: cpu 4, memory 2\n\nact"},
			"p1": {
				ID: "p1", Kind: "producer", Stream: "s1", State: "ready", Tier: "pro", Score: 1,
				Brief: "pro\nRESOURCES: cpu 4, memory 8, gpu, ps5\nDONE-WHEN: secret-gate\n\nsecret prose body",
				Prose: "secret prose body",
			},
		},
	}

	inspect, _ := override("rows", `def rows(card, kind, candidates):
    if card.id == "p1":
        if card.tags != ("gpu",):
            fail("card.tags=%r" % (card.tags,))
        if dict(card.resources) != {"cpu": 4, "memory": 8, "ps5": 1}:
            fail("card.resources=%r" % card.resources)
        by_name = {c.name: c for c in candidates}
        if hasattr(by_name["b1"], "width"):
            fail("bench b1 must not expose width")
        if not hasattr(by_name["k1"], "width") or by_name["k1"].width != 2:
            fail("kin k1 must expose width=2")
        if by_name["b1"].tags != ("amd64", "gpu", "linux"):
            fail("b1.tags=%r" % (by_name["b1"].tags,))
        if dict(by_name["b1"].resources) != {"cpu": 8, "memory": 32, "ps5": 1}:
            fail("b1.resources=%r" % by_name["b1"].resources)
        if dict(by_name["b1"].free) != {"cpu": 2, "memory": 1, "ps5": 1}:
            fail("b1.free=%r" % by_name["b1"].free)
        if dict(by_name["b3"].free) != {"cpu": 0, "memory": 0, "ps5": 0}:
            fail("paused b3.free=%r" % by_name["b3"].free)
    return prefer(candidates, _want(card, kind), card.resources if kind == "work" else {})`)

	// Check unworded() preserves RESOURCES: while dropping prose and DONE-WHEN.
	uw := unworded(snap)
	assert.NotContains(t, uw.Cards["p1"].Brief, "secret")
	assert.Empty(t, uw.Cards["p1"].Prose)

	// Run through in-memory pipe childEngine so gob + unworded() are exercised in pure unit mode.
	f := &fakeChild{begin: make(chan struct{}), clock: newFakeClock()}
	close(f.begin)
	e := childEngine(newFakeClock(), f)
	q := Questions{Rows: []RowsQuestion{{"p1", KindWork}}}
	a, fs := e.Answer(Versions{Active: pv(2, inspect)}, snap, ro, q)
	require.Empty(t, fs)
	assert.Equal(t, 2, a.Version)
	assert.Equal(t, map[string]Width{"k1": {Work: 2, Ahead: 0}}, a.Width, "only kin k1 is in a.Width")
	// b2 and k1 have room for p1; b1 (tight memory/cpu) and b3 (paused) are ordered after them.
	assert.Equal(t, []string{"b2", "k1", "b1", "b3"}, a.Rows[RowsQuestion{"p1", KindWork}])
}

// TestV3JointRowsBenchCapableAndZeroKinRefused: benches are inherently capable
// in jointRows even with no a.Width entry; naming only a zero-work kin row
// while a bench candidate exists is refused naming the bench.
func TestV3JointRowsBenchCapableAndZeroKinRefused(t *testing.T) {
	t.Parallel()
	w := &world{
		t:  t,
		st: store.NewFake(),
		ro: handRoster(
			rc{"b1", roster.KindWorker, 4, []string{"pro"}, ""},
			rc{"k1", roster.KindKin, 2, []string{"pro"}, "m-pro"},
		),
	}
	w.apply(
		store.PutStream{Name: "s1", Repo: "git@example.com:r.git", Base: "main", Pos: 1},
		hooked("b1"), hooked("k1"),
		store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Score: 1, Brief: "do p1", Prose: "do p1"},
	)
	w.apply(
		store.SetRowState{Row: "b1", State: "up"},
		store.SetRowState{Row: "k1", State: "up"},
		store.SetCardState{Card: "p1", State: "ready"},
	)

	// Zero-work kin + bench both named -> passes because b1 is inherently capable.
	zeroKin, _ := override("width", "def width(row):\n    return {\"work\": 0, \"ahead\": 0}")
	a, fs := testEngine().Answer(Versions{Active: pv(2, zeroKin)}, w.snap(), w.ro, FullQuestions(w.snap(), nil))
	require.Empty(t, fs)
	assert.Equal(t, 2, a.Version)

	// Naming ONLY zero-work k1 while b1 could take p1 -> refused by jointRows!
	onlyKin, _ := overrideIn(zeroKin, "rows", "def rows(card, kind, candidates):\n    return [\"k1\"]")
	a2, fs2 := testEngine().Answer(Versions{Active: pv(3, onlyKin)}, w.snap(), w.ro, FullQuestions(w.snap(), nil))
	require.Len(t, fs2, 1)
	assert.Equal(t, 0, a2.Version)
	assert.Contains(t, fs2[0].Error(), "rows(p1 work): every row named has work 0 in width, while row b1 could take it")
}

// TestFunctionalV3EvaluatorPreservesCardResourcesAcrossUnworded verifies the
// real child policy-eval subprocess receives card.tags and card.resources.
func TestFunctionalV3EvaluatorPreservesCardResourcesAcrossUnworded(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.ro.Workers["r1"] = roster.Worker{
		Name:      "r1",
		Tiers:     []string{"flash", "pro", "heavy"},
		Tags:      []string{"linux", "metal"},
		Resources: map[string]int{"cpu": 8, "memory": 32},
		Harness:   roster.WorkerHarness,
	}
	w.apply(store.PutCard{
		ID: "pgpu", Kind: "producer", Stream: "s1", Tier: "pro", Score: 5,
		Brief: "pro\nRESOURCES: cpu 4, memory 16, metal\n\ndo metal work",
		Prose: "do metal work",
	})
	w.apply(store.SetCardState{Card: "pgpu", State: "ready"})

	body, _ := override("rows", `def rows(card, kind, candidates):
    if card.id == "pgpu":
        if card.tags != ("metal",) or dict(card.resources) != {"cpu": 4, "memory": 16}:
            fail("got tags=%r resources=%r" % (card.tags, card.resources))
    return prefer(candidates, _want(card, kind), card.resources if kind == "work" else {})`)
	e := NewEngine(newFakeClock())
	snap := w.snap()
	a, fs := e.Answer(Versions{Active: pv(2, body)}, snap, w.ro, FullQuestions(snap, nil))
	require.Empty(t, fs)
	assert.Equal(t, 2, a.Version)
}
