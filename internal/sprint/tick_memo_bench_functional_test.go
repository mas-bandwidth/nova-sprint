//go:build functional

package sprint

import (
	"runtime"
	"testing"
	"time"
)

// The bench is a shared 64-way machine whose load average ranged from about 100
// to 265 while these figures were taken on 2026-10-18, so a wall-time budget is
// set against the slowest observed run, not the average. Post-fix, on that date
// and under that load:
//
//	5000 x 2000:  5.3 s - 10.6 s/op, 645 MB/op, 20.7 M allocs/op
//	 600 x  330:  149 ms - 253 ms/op,  22 MB/op,   523 k allocs/op
//	  40 x   10:                  see BenchmarkTick40Cards10Judgments
//
// BASE (sprint/v17 before the memo; the same fixture and tick) at 40 x 10 was
// 106 ms/op, 15.2 MB/op, 535 k allocs/op, measured 2026-10-18 with
// `BenchmarkBaseTick/40x10` on the same tree. BASE at 600 x 330 was 237 s/op and
// 47.0 GB/op (attempt 2's run; a re-run on this attempt was still going after
// five minutes and was abandoned, since the fixture is judgments x cards). At
// 5000 x 2000 BASE is correspondingly hours, so its BASE figure is not run and
// is not claimed here.
//
// The budgets are about 2x the slowest post-fix figure above, so a loaded bench
// does not fail the gate while a real regression does. The card's target is under
// 200 ms per tick for 5000 x 2000; that is not met on this build. The next hot
// spot in the profile is TickCheck's needs resolution: needHolder 42% cum and
// closesFor 19% cum in needs_resolve.go, which this card does not change.

// tickBudget5000x2000 is the wall-time budget for a full tick of the 5000 x 2000
// fixture (ReadCardExtras, TickCheck and JudgmentOverdue over every open judgment).
const tickBudget5000x2000 = 30 * time.Second

// tickAllocBudget5000x2000 is the allocations budget per tick, about 2x 645 MB/op.
const tickAllocBudget5000x2000 = 1350e6

// tickBudget600x330 and tickAllocBudget600x330 are the same budgets for the smaller
// pair, about 2x 253 ms/op and 2x 22 MB/op.
const tickBudget600x330 = 750 * time.Millisecond
const tickAllocBudget600x330 = 45e6

// tickBudget40x10 and tickAllocBudget40x10 are the fast pair: the only size a BASE
// tick finishes quickly enough to reproduce, so it is kept as the before/after line.
// Post-fix measured 8.5 ms/op and 1.7 MB/op; BASE is 106 ms/op and 15.2 MB/op, so the
// budgets sit well above post-fix and below BASE.
const tickBudget40x10 = 60 * time.Millisecond
const tickAllocBudget40x10 = 6e6

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

// BenchmarkTick40Cards10Judgments is the same tick at the smallest size: it takes
// a BASE tick about 106 ms, so the before/after line can be reproduced quickly.
func BenchmarkTick40Cards10Judgments(b *testing.B) {
	benchmarkTick(b, 40, 10, tickBudget40x10, tickAllocBudget40x10)
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
