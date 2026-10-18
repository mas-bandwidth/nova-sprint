//go:build functional

package sprint

import (
	"runtime"
	"testing"
	"time"
)

// tickBudget5000x2000 is the wall-time budget for a full tick of the 5000 x 2000
// fixture (ReadCardExtras, TickCheck and JudgmentOverdue over every open judgment),
// set at about 2x the figure measured on this machine 2026-10-18 (a tick is about
// 7.8 s here; the profiled live tick is milliseconds). It fails loudly when a tick
// regresses. The target is under 200 ms per tick on the bench machine.
const tickBudget5000x2000 = 16 * time.Second

// tickAllocBudget5000x2000 is the allocations budget per tick, about 2x the figure
// measured on this machine 2026-10-18 (about 3.1 GB/op).
const tickAllocBudget5000x2000 = 7e9

// BenchmarkTick5000Cards2000Judgments times one tick of the 5000 x 2000 fake
// store with the fake clock, with the real clock read only to measure it. It
// asserts the tick stays inside the named time and allocation budgets.
func BenchmarkTick5000Cards2000Judgments(b *testing.B) {
	f := buildTickFixture(b, 5000, 2000)
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
		if elapsed > tickBudget5000x2000 {
			b.Fatalf("tick took %v, budget %v", elapsed, tickBudget5000x2000)
		}
	}
	perTick := totalAlloc / uint64(b.N)
	if perTick > uint64(tickAllocBudget5000x2000) {
		b.Fatalf("tick allocated %d bytes, budget %d", perTick, uint64(tickAllocBudget5000x2000))
	}
}
