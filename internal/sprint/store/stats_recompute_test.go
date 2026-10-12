package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// RecomputeStats doc: it classifies the stored take ends (CardCostOf) by cause and writes
// the per-member and per-route counters to keyStatsRecompute; a dry run prints the same
// figures and writes nothing (stats_recompute.go:22). Five cards end five ways on one
// route: one ok, one failed work, one provider failure, one no result and one 134
// environment death. The recomputed ok% is ok over (ok + failed work + no result) alone —
// the provider failure and the environment death are outside the denominator.
func TestARecomputeCountsTheStoredEndsByCauseAndWritesTheRecord(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 5, briefOf("flash", ""))
	h.startMachine()
	h.machine() // the tick deals the five cards ready

	finish := func(card string, fr sprint.FinishReq) {
		t.Helper()
		wc := h.snap().Fleet.Card(card)
		require.NotNil(t, wc, "dealt: %s", card)
		gens := map[string]int{wc.ID: wc.Int("gen")}
		h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: gens, Who: wc.Row}))
		fr.As, fr.Sel, fr.Gens, fr.Who = wc.Row, sprint.Sel{IDs: []string{wc.ID}}, gens, wc.Row
		h.must(FinishStep(fr))
	}
	finish("s1-1.w1", sprint.FinishReq{Head: "abc123", Usage: "wall=1.00s"})                        // ok
	finish("s1-2.w1", sprint.FinishReq{Failed: true, Report: "verdict not-done", Usage: "wall=1.00s"}) // failed work
	finish("s1-3.w1", sprint.FinishReq{Failed: true, Report: providerLine, Usage: "wall=1.00s"})       // provider failure
	finish("s1-4.w1", sprint.FinishReq{Failed: true, Report: noResultLine, Usage: "wall=1.00s"})       // no result
	finish("s1-5.w1", sprint.FinishReq{Failed: true, Exit: 134, Report: "no result: aborted", Harness: "aborted", Usage: "wall=1.00s"})
	h.machine() // the pump applies the finishes' cost records to the stored work table

	var dryOut, dryErr strings.Builder
	require.NoError(t, h.st.RecomputeStats(h.ctx, false, true, &dryOut, &dryErr))
	assert.Contains(t, dryOut.String(), "RECOMPUTE DRY-RUN")
	assert.Contains(t, dryOut.String(), "ok=1 failed=1 no_result=1 provider=1 coordination=0 environment=1 harness=0 okpct=33.3%")
	_, ok, err := h.st.RecomputeStatsRecord(h.ctx)
	require.NoError(t, err)
	assert.False(t, ok, "a dry run writes no record")

	var out, errb strings.Builder
	require.NoError(t, h.st.RecomputeStats(h.ctx, false, false, &out, &errb))
	assert.Contains(t, out.String(), "RECOMPUTE OK")

	rec, ok, err := h.st.RecomputeStatsRecord(h.ctx)
	require.NoError(t, err)
	require.True(t, ok, "the recompute record is written and readable")
	assert.Contains(t, rec.Routes, "flash-a", "the route's counters are written")
	assert.Equal(t, RecomputeCounts{OK: 1, Failed: 1, NoResult: 1, Provider: 1, Environment: 1, OkPct: "33.3%"}, rec.Routes["flash-a"])
}

// RecomputeStats doc: --json is one object, the counters as stored (stats_recompute.go:22).
func TestARecomputeJSONCarriesTheCounters(t *testing.T) {
	t.Parallel()
	h := routeHarness(t, route("flash-a", "flash"))
	h.addReady("s1", 1, briefOf("flash", ""))
	h.startMachine()
	h.machine()
	wc := h.snap().Fleet.Card("s1-1.w1")
	require.NotNil(t, wc)
	gens := map[string]int{wc.ID: wc.Int("gen")}
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: gens, Who: wc.Row}))
	h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: gens, Failed: true, Report: providerLine, Who: wc.Row}))
	h.machine() // the pump applies the finish's cost record to the stored work table

	var out strings.Builder
	require.NoError(t, h.st.RecomputeStats(h.ctx, true, false, &out, &out))
	assert.Contains(t, out.String(), `"okpct":"-"`, "a provider failure alone is no work end, so ok% is -")
	assert.Contains(t, out.String(), `"provider":1`)
}
