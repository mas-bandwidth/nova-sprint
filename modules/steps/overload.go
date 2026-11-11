package steps

import (
	"fmt"
	"time"
)

// An overloaded bench wakes the coordinator (2026-11-10: a 64-thread bench
// sat at load 350-830 for hours under CI jobs that each sized themselves by
// nproc; 56 of 69 CI jobs killed by the two-minute rule ran on two benches,
// and the board's load column was seen once and nothing was done). Layer 6
// keeps each bench row's load across its beats (run.LoadWatch) and passes,
// per row, whether it has stayed over policy's overload_factor x its cores
// for overload_span (Over), or back under for the span (not Over), in
// Inputs.Overloads; a row it says nothing of is no fact. An overloaded bench
// row that is up has one open overload judgement about the row; it clears by
// itself once the row is under the bound for the span, and escalates
// unanswered like the other watch kinds. Answered (retry), it stays quiet for
// the span, then opens again if the row is still over. Nothing is held,
// narrowed or dealt differently: the push only. A friend row has no machine
// load and never gets one. tla/Overload.tla is the model.

// Overload is one bench row's load as layer 6 read it across its beats.
type Overload struct {
	Over                  bool          // over the bound on every beat for the span; false: under it for the span
	Cores                 int           // the cores its beats measure
	Load1Centi            int           // the 1-minute load now, in hundredths
	LowCenti, PeakCenti   int           // the lowest and highest load over the current run (over or under)
	For                   time.Duration // how long it has been over (or under)
	SlotsUsed, SlotsTotal int           // the nova-reserve slots its beat counted
	Factor                float64       // the bound, as a multiple of Cores
	Span                  time.Duration // how long a run must last to count
}

// overloads opens the overload judgement of each bench row that is up and
// has stayed over the bound for the span.
func (t *tick) overloads() {
	for _, name := range sortedKeys(t.in.Overloads) {
		o := t.in.Overloads[name]
		if _, bench := t.in.Roster.Workers[name]; !o.Over || !bench || o.Cores <= 0 || t.v.Rows[name].State != "up" {
			continue
		}
		last := t.v.Judgements[fmt.Sprintf("overload.%s.%d", name, t.episodes("overload", name))]
		if last.State == "answered" && t.in.Now.Before(last.ClosedAt.Add(o.Span)) {
			continue // fixed, it says: the load takes a span to show it
		}
		t.judge("overload", name, t.overloadQuestion(name, o))
	}
}

func centi(n int) string { return fmt.Sprintf("%d.%02d", n/100, n%100) }

// overloadQuestion is the judgement's text, the instruction first and the
// card work inside the 300 bytes a push shows (it tells sprint load from
// other load, such as CI).
func (t *tick) overloadQuestion(name string, o Overload) string {
	return fmt.Sprintf("OVERLOAD row %s (%d cores): this machine is overloaded: find what is loading it and fix it now (runner count or per-job parallelism for CI, width for card work); "+
		"load %s now (%.1fx), %s-%s over %s, bound %.1fx (%s) for %s; cards: %d working of width %d, nova-reserve slots %d/%d; "+
		"answer retry once fixed: it clears by itself when the load stays under the bound for %s",
		name, o.Cores, centi(o.Load1Centi), float64(o.Load1Centi)/100/float64(o.Cores), centi(o.LowCenti), centi(o.PeakCenti), o.For.Truncate(time.Second),
		o.Factor, centi(int(o.Factor*float64(o.Cores)*100)), o.Span, t.v.Working(name), t.width(name), o.SlotsUsed, o.SlotsTotal, o.Span)
}

// clearedOverload is why an open overload judgement's cause no longer holds:
// the row has been under the bound for the span. No fact is never a clear.
func (t *tick) clearedOverload(name string) string {
	if o, ok := t.in.Overloads[name]; ok && !o.Over {
		return fmt.Sprintf("cleared: row %s is under %.1fx its %d cores for %s (load %s)", name, o.Factor, o.Cores, o.For.Truncate(time.Second), centi(o.Load1Centi))
	}
	return ""
}
