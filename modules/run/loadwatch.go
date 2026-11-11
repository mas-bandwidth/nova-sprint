package run

import (
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// LoadWatch keeps each bench row's load across ticks, from its beats as the
// store holds them (store.Row Load1Centi and Cores, the last SetRowLoad;
// no new poll). A row is over while its 1-minute load is above factor x its
// cores. A run of over, or of under, that has lasted span is the row's fact
// for the steps (steps.Overload, the overload judgement): overloaded, or
// back under; a shorter run changes nothing, so a spike neither opens one
// nor a dip clears one. A row with no cores (it never beat; a friend) is
// not watched. Observe runs once a tick with the tick's clock; the beats
// are its samples.
type LoadWatch struct {
	rows map[string]*rowLoad
}

// rowLoad is one row's current run of over or under, and the last fact.
type rowLoad struct {
	over      bool
	since     time.Time
	low, peak int
	fact      *steps.Overload
}

// Observe takes this tick's sample of each row and returns each watched
// row's fact, by name; a row whose runs have not yet lasted span has none.
func (w *LoadWatch) Observe(rows map[string]store.Row, now time.Time, factor float64, span time.Duration) map[string]steps.Overload {
	if w.rows == nil {
		w.rows = map[string]*rowLoad{}
	}
	out := map[string]steps.Overload{}
	for name := range w.rows {
		if rows[name].Cores <= 0 {
			delete(w.rows, name)
		}
	}
	for name, r := range rows {
		if r.Cores <= 0 {
			continue
		}
		over := float64(r.Load1Centi) > factor*float64(r.Cores)*100
		l := w.rows[name]
		if l == nil {
			l = &rowLoad{over: over, since: now, low: r.Load1Centi, peak: r.Load1Centi}
			w.rows[name] = l
		} else if l.over != over {
			l.over, l.since, l.low, l.peak = over, now, r.Load1Centi, r.Load1Centi
		}
		l.low, l.peak = min(l.low, r.Load1Centi), max(l.peak, r.Load1Centi)
		if now.Sub(l.since) >= span {
			l.fact = &steps.Overload{Over: over, Cores: r.Cores, LowCenti: l.low, PeakCenti: l.peak, For: now.Sub(l.since), Factor: factor, Span: span}
		}
		if l.fact != nil {
			f := *l.fact
			f.Load1Centi, f.SlotsUsed, f.SlotsTotal = r.Load1Centi, r.SlotsUsed, r.SlotsTotal
			out[name] = f
		}
	}
	return out
}
