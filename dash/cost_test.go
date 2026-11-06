package dash

import (
	"github.com/mas-bandwidth/nova-sprint/store"
	"math/big"
	"testing"
	"time"
)

func TestTierCentsLargestRemainder(t *testing.T) {
	v := 0.0
	w := &workRow{Cost: &v, ByTier: map[string]float64{}, tierMicros: map[string]*big.Int{"flash": big.NewInt(11000), "heavy": big.NewInt(9000), "pro": big.NewInt(9000)}}
	finishCosts(w)
	if *w.Cost != 0.03 || w.ByTier["flash"] != 0.01 || w.ByTier["heavy"] != 0.01 || w.ByTier["pro"] != 0.01 {
		t.Fatalf("unexpected partition %+v total %v", w.ByTier, *w.Cost)
	}
	w = &workRow{Cost: &v, ByTier: map[string]float64{}, tierMicros: map[string]*big.Int{"flash": big.NewInt(1000), "heavy": big.NewInt(1000), "pro": big.NewInt(1000)}}
	finishCosts(w)
	if *w.Cost != 0.01 || w.ByTier["flash"] != 0.01 || w.ByTier["heavy"] != 0 || w.ByTier["pro"] != 0 {
		t.Fatalf("tie partition %+v total %v", w.ByTier, *w.Cost)
	}
}

func TestExactStreamTotalSeparateFromDisplay(t *testing.T) {
	s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{"a": {ID: "a", Kind: "work", Stream: "s", Tier: "flash", Cost: 1000, Priced: true}, "b": {ID: "b", Kind: "work", Stream: "s", Tier: "pro", Cost: 1000, Priced: true}, "c": {ID: "c", Kind: "work", Stream: "s", Tier: "heavy", Cost: 1000, Priced: true}}}
	d := mapSnapshot(s, time.Time{})
	w := d.Tables["work"].(map[string]*workRow)["s"]
	if d.StreamCosts["s"]["total_cost"] != 0.003 || w.Cost == nil || *w.Cost != 0.01 {
		t.Fatalf("raw=%v display=%v", d.StreamCosts["s"]["total_cost"], w.Cost)
	}
}
func TestMoneyCellGrouping(t *testing.T) {
	v := 1234.56
	if got := moneyCell(&v); got != "$1,234.56" {
		t.Fatalf("money=%q", got)
	}
}
