package sprint

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// tickCountMu serializes the measured phases: both this test and the equivalence
// test run parallel, and the equivalence test must not be counted. tickCountEnabled
// is off outside these phases, so its ticks leave the counters alone.
var tickCountMu sync.Mutex

// resetTickCounters zeroes the package-level tick counters (unexported, a few ns
// each) around a measured phase.
func resetTickCounters() {
	atomic.StoreInt64(&tickCountBlockersWalk, 0)
	atomic.StoreInt64(&tickCountReadGenOf, 0)
	atomic.StoreInt64(&tickCountReadNextGen, 0)
	atomic.StoreInt64(&tickCountUnresolvedNeeds, 0)
}

func tickCounts() (walk, genof, nextgen, unres int64) {
	return atomic.LoadInt64(&tickCountBlockersWalk),
		atomic.LoadInt64(&tickCountReadGenOf),
		atomic.LoadInt64(&tickCountReadNextGen),
		atomic.LoadInt64(&tickCountUnresolvedNeeds)
}

// readMatrix runs ReadCardExtras (the read-generation phase) on a fresh fixture
// and returns the readGenOf and readNextGen counts.
func readMatrix(t *testing.T, nCards, nJudgments int) (genof, nextgen int64) {
	t.Helper()
	f := buildTickFixture(t, nCards, nJudgments)
	tickCountMu.Lock()
	defer tickCountMu.Unlock()
	atomic.StoreInt32(&tickCountEnabled, 1)
	defer atomic.StoreInt32(&tickCountEnabled, 0)
	resetTickCounters()
	ReadCardExtras(f.s)
	_, g, x, _ := tickCounts()
	return g, x
}

// overdueMatrix runs JudgmentOverdue over every open judgment (the judgment phase,
// which drives quietedByBlocker and the Blockers memo) on a fresh fixture.
func overdueMatrix(t *testing.T, nCards, nJudgments int) (walk, unres int64) {
	t.Helper()
	f := buildTickFixture(t, nCards, nJudgments)
	tickCountMu.Lock()
	defer tickCountMu.Unlock()
	atomic.StoreInt32(&tickCountEnabled, 1)
	defer atomic.StoreInt32(&tickCountEnabled, 0)
	resetTickCounters()
	for _, o := range f.s.Open {
		if o.Note.Kind == Judgment {
			JudgmentOverdue(f.s, f.r, o.Note)
		}
	}
	w, _, _, u := tickCounts()
	return w, u
}

// TestTickCallCountsDoNotGrowWithJudgmentsTimesCards asserts the 5000 x 2000 fake
// store runs the Blockers walk exactly once across every overdue judgment, reads
// each read index card at most once and each (primary, reader) generation at most
// once, and resolves each card's needs at most once per walk. Then it runs the
// same phases at 1/4 and 1/2 size and fails if any count grows faster than
// linearly in cards (full over quarter <= 4.5). It uses the package counters: no
// wall time is read.
func TestTickCallCountsDoNotGrowWithJudgmentsTimesCards(t *testing.T) {

	g, n := readMatrix(t, 5000, 2000)
	require.LessOrEqual(t, g, int64(300)+1, "readGenOf at most once per read-index card")
	require.LessOrEqual(t, n, int64(2000), "readNextGen at most once per (primary, reader)")

	w, u := overdueMatrix(t, 5000, 2000)
	require.Equal(t, int64(1), w, "the Blockers walk runs exactly once across the overdue judgments")
	require.LessOrEqual(t, u, int64(5000+16), "UnresolvedNeeds at most once per card per walk (sentinels included)")

	gq, _ := readMatrix(t, 1250, 500)
	gFull := float64(g) / float64(max64(gq, 1))
	require.LessOrEqual(t, gFull, 4.5, "readGenOf grew faster than linearly in cards")

	_, uq := overdueMatrix(t, 1250, 500)
	uFull := float64(u) / float64(max64(uq, 1))
	require.LessOrEqual(t, uFull, 4.5, "UnresolvedNeeds grew faster than linearly in cards")

	_, uh := overdueMatrix(t, 2500, 1000)
	require.LessOrEqual(t, float64(uh)/float64(max64(uq, 1)), 2.25, "UnresolvedNeeds grew faster than linearly at half size")

	// Join the parallel phase only after the measured phases: the counters are
	// package-level, and another parallel test's tick must not land inside a count.
	t.Parallel()
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
