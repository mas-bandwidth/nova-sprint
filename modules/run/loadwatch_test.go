package run

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// TestLoadWatchNeedsTheWholeSpan: a bench row is overloaded only once its
// load has been over factor x cores on every tick's sample for the span
// (a shorter spike is no fact), stays so through a dip shorter than the
// span, and is back under only once under for the span. A row with no
// cores (a friend, or one that never beat) is not watched; a row gone from
// the store is forgotten.
func TestLoadWatchNeedsTheWholeSpan(t *testing.T) {
	t.Parallel()
	var w LoadWatch
	now := time.Unix(1000, 0).UTC()
	span := 5 * time.Minute
	sample := func(load int) map[string]store.Row {
		return map[string]store.Row{"vision": {Name: "vision", Load1Centi: load, Cores: 64, SlotsUsed: 2, SlotsTotal: 16}, "kin1": {Name: "kin1", Load1Centi: 90000}}
	}
	at := func(load int, d time.Duration) (over, known bool) {
		now = now.Add(d)
		got := w.Observe(sample(load), now, 2, span)
		if _, ok := got["kin1"]; ok {
			t.Fatal("a row with no cores is watched")
		}
		f, ok := got["vision"]
		return f.Over, ok
	}
	if _, known := at(51200, 0); known {
		t.Fatal("one sample over is a fact")
	}
	if _, known := at(12800, time.Minute); known { // at the bound is not over it
		t.Fatal("a spike shorter than the span is a fact")
	}
	at(51200, time.Minute)
	if _, known := at(83000, 4*time.Minute); known {
		t.Fatal("over for 4m of a 5m span is a fact")
	}
	if over, known := at(35000, time.Minute); !over || !known {
		t.Fatalf("over for the span: over %v known %v", over, known)
	}
	got := w.Observe(sample(35000), now, 2, span)["vision"]
	if got.LowCenti != 35000 || got.PeakCenti != 83000 || got.For != span || got.Load1Centi != 35000 || got.SlotsTotal != 16 {
		t.Fatalf("the fact %+v", got)
	}
	if over, _ := at(1000, 3*time.Minute); !over {
		t.Fatal("a dip shorter than the span cleared it")
	}
	if over, _ := at(1000, 2*time.Minute); over {
		t.Fatal("under for the span is still over")
	}
	if got := w.Observe(map[string]store.Row{}, now, 2, span); len(got) != 0 || len(w.rows) != 0 {
		t.Fatalf("a row gone from the store is kept: %+v", got)
	}
}
