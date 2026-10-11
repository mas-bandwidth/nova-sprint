package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIdleTraceCountsCardsBehindADroppedNeed pins TraceIdle's drop-blocked words
// (idle.go:147, docs/SPEC-SPRINT.md section 14): the cards behind a dropped need
// count behind the direct children, and the oldest open NBlocked judgment ages it.
func TestIdleTraceCountsCardsBehindADroppedNeed(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"root"}}))
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"a", "b", "c"}, Needs: []string{"root"}}))
	w.seedDroppedNeed("root")
	w.must(Resolve(w.s, ResolveReq{}))
	w.tick(6 * time.Minute)
	got := TraceIdle(w.s, TickReq{})
	assert.Contains(t, got, "3 behind 3 drop-blocked judgments (oldest 6m0s)")
}

// TestIdleTraceCountsMissingNeedsAndSurvivesACycle pins TraceIdle's missing-needs
// words and its cycle guard (idle.go:126, :145, :223): an absent need and two cards
// waiting on each other both count, and tracing them terminates.
func TestIdleTraceCountsMissingNeedsAndSurvivesACycle(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.s.Work.SetRows(append(w.s.Work.Rows(), "sx", "sy", "sz"))
	w.s.Work.Put(&Card{ID: "x", Row: "sx", Col: Waiting, Score: 1, Rev: 1, Fields: map[string]string{"kind": "primary", "needs": "y"}})
	w.s.Work.Put(&Card{ID: "y", Row: "sy", Col: Waiting, Score: 2, Rev: 1, Fields: map[string]string{"kind": "primary", "needs": "x"}})
	w.s.Work.Put(&Card{ID: "z", Row: "sz", Col: Waiting, Score: 3, Rev: 1, Fields: map[string]string{"kind": "primary", "needs": "gone"}})
	done := make(chan string, 1)
	go func() { done <- TraceIdle(w.s, TickReq{}) }()
	select {
	case got := <-done:
		assert.Contains(t, got, "cards with missing needs")
	case <-time.After(5 * time.Second):
		t.Fatal("TraceIdle did not return on a cyclic need")
	}
}

// TestIdleTraceNamesHeldAndSentinelRoots pins TraceIdle's held and sentinel roots
// (idle.go:131-154): a held card, a held sentinel, a reached sentinel and a
// waiting sentinel each give their own words.
func TestIdleTraceNamesHeldAndSentinelRoots(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.s.Work.SetRows(append(w.s.Work.Rows(), "sh", "shs", "srs", "sus", "s1", "s2", "s3"))
	w.s.Work.Put(&Card{ID: "h", Row: "sh", Col: Waiting, Score: 1, Rev: 1, Fields: map[string]string{"kind": "primary", "held": "yes"}})
	w.s.Work.Put(&Card{ID: "hs", Row: "shs", Col: Waiting, Score: 2, Rev: 1, Fields: map[string]string{"kind": "sentinel", "held": "yes"}})
	w.s.Work.Put(&Card{ID: "rs", Row: "srs", Col: Waiting, Score: 3, Rev: 1, Fields: map[string]string{"kind": "sentinel", "reached": stamp(t0)}})
	w.s.Work.Put(&Card{ID: "us", Row: "sus", Col: Waiting, Score: 4, Rev: 1, Fields: map[string]string{"kind": "sentinel"}})
	w.s.Work.Put(&Card{ID: "w1", Row: "s1", Col: Waiting, Score: 5, Rev: 1, Fields: map[string]string{"kind": "primary", "needs": "hs"}})
	w.s.Work.Put(&Card{ID: "w2", Row: "s2", Col: Waiting, Score: 6, Rev: 1, Fields: map[string]string{"kind": "primary", "needs": "rs"}})
	w.s.Work.Put(&Card{ID: "w3", Row: "s3", Col: Waiting, Score: 7, Rev: 1, Fields: map[string]string{"kind": "primary", "needs": "us"}})
	got := TraceIdle(w.s, TickReq{})
	assert.Contains(t, got, "h (held)")
	assert.Contains(t, got, "sentinel hs (held)")
	assert.Contains(t, got, "sentinel rs (reached, not released)")
	assert.Contains(t, got, "sentinel us (waiting for the cards before it)")
}

// TestIdleTraceNamesAtMostEightRoots pins TraceIdle's root cap (idle.go:206):
// with nine distinct roots, eight are named and the rest are one "roots more".
func TestIdleTraceNamesAtMostEightRoots(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	for i := 1; i <= 9; i++ {
		w.s.Work.SetRows(append(w.s.Work.Rows(), "s"+itoa(i)))
		w.s.Work.Put(&Card{ID: "h" + itoa(i), Row: "s" + itoa(i), Col: Waiting, Score: float64(i), Rev: 1,
			Fields: map[string]string{"kind": "primary", "held": "yes"}})
	}
	got := TraceIdle(w.s, TickReq{})
	assert.Contains(t, got, "and 1 roots more")
	assert.NotContains(t, got, "h9 (held)")
}

// TestTickIdleWritesTheEpisodeAndOneNote pins TickIdle's episode (idle.go:264-276,
// docs/SPEC-SPRINT.md section 14): the first idle tick writes idle_since, past
// IdleWindow one NIdle note names the roots and idle_said is written, and a second
// idle tick says nothing more.
func TestTickIdleWritesTheEpisodeAndOneNote(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 1}))
	w.s.Work.SetRows(append(w.s.Work.Rows(), "s1"))
	w.s.Work.Put(&Card{ID: "a", Row: "s1", Col: Waiting, Score: 1, Rev: 1, Fields: map[string]string{"kind": "primary", "needs": "gone"}})
	r := TickReq{IdleAlarm: true}

	p, _ := TickIdle(w.s, r)
	require.Len(t, p.Props, 1, "the first idle tick writes the episode: %+v", p.Props)
	assert.Equal(t, PropIdleSince, p.Props[0].Name)
	w.must(p)

	w.tick(6 * time.Minute)
	p, _ = TickIdle(w.s, r)
	require.Len(t, p.Units, 1, "past the window: %+v", p)
	require.Len(t, p.Units[0].Notes, 1, "one note to the coordinator: %+v", p.Units[0])
	n := p.Units[0].Notes[0]
	assert.Equal(t, NIdle, n.Type)
	assert.Equal(t, "coordinator", n.To)
	assert.Contains(t, n.What, "1 behind 1 cards with missing needs")
	w.must(p)

	w.tick(6 * time.Minute)
	p, _ = TickIdle(w.s, r)
	assert.Empty(t, p.Units, "once an episode: %+v", p)
}
