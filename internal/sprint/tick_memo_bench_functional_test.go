//go:build functional

package sprint

import (
	"runtime"
	"testing"
	"time"
)

// tickBudget5000x2000 is the wall-time budget for a full tick of the 5000 x 2000
// fixture (ReadCardExtras, TickCheck and JudgmentOverdue over every open judgment),
// set at about 2x the figure measured on this machine 2026-10-18 (about 1.8 s/op
// here; BASE is 237 s/op at 600 x 330, so 5000 x 2000 at BASE is hours). It fails
// loudly when a tick regresses. The target is under 200 ms per tick on the bench
// machine; the remaining cost is TickCheck's needs resolution
// (needHolder/closesFor, needs_resolve.go), which this card does not touch.
const tickBudget5000x2000 = 4 * time.Second

// tickAllocBudget5000x2000 is the allocations budget per tick, about 2x the figure
// measured on this machine 2026-10-18 (about 644 MB/op; BASE is 47.0 GB/op at
// 600 x 330).
const tickAllocBudget5000x2000 = 1350e6

// tickBudget600x330 and tickAllocBudget600x330 are the same budgets for the smaller
// pair, about 2x the figures measured on this machine 2026-10-18 (about 51 ms/op
// and 22 MB/op); the BASE lines are 237 s/op and 47.0 GB/op.
const tickBudget600x330 = 110 * time.Millisecond
const tickAllocBudget600x330 = 45e6

// BenchmarkTick5000Cards2000Judgments times one tick of the 5000 x 2000 fake
// store with the fake clock, with the real clock read only to measure it. It
// asserts the tick stays inside the named time and allocation budgets.
func BenchmarkTick5000Cards2000Judgments(b *testing.B) {
	benchmarkTick(b, 5000, 2000, tickBudget5000x2000, tickAllocBudget5000x2000)
}

// BenchmarkTick600Cards330Judgments is the same tick at the smaller size, the pair
// the card records against BASE.
func BenchmarkTick600Cards330Judgments(b *testing.B) {
	benchmarkTick(b, 600, 330, tickBudget600x330, tickAllocBudget600x330)
}

func benchmarkTick(b *testing.B, nCards, nJudgments int, timeBudget time.Duration, allocBudget uint64) {
	b.Helper()
	f := buildTickFixture(b, nCards, nJudgments)
	b.ReportAllocs()
	var totalAlloc uint64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// a fresh tick: the memos from the last iteration must not carry over
		f.s.blockersSet, f.s.blockersMemo = false, nil
		f.s.readIndex, f.s.readIndexPlaced = nil, nil
		var m1, m2 runtime.MemStats
		runtime.ReadMemStats(&m1)
		start := time.Now()
		runTickDecisions(f)
		elapsed := time.Since(start)
		runtime.ReadMemStats(&m2)
		totalAlloc += m2.TotalAlloc - m1.TotalAlloc
		if elapsed > timeBudget {
			b.Fatalf("%d x %d tick took %v, budget %v", nCards, nJudgments, elapsed, timeBudget)
		}
	}
	perTick := totalAlloc / uint64(b.N)
	if perTick > allocBudget {
		b.Fatalf("%d x %d tick allocated %d bytes, budget %d", nCards, nJudgments, perTick, allocBudget)
	}
}
