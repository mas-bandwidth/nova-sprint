package dash

import (
	"encoding/json"
	"github.com/mas-bandwidth/nova-sprint/store"
	"math/big"
	"testing"
	"time"
)

func TestTierCentsLargestRemainder(t *testing.T) {
	v := new(big.Int)
	w := &workRow{Cost: v, ByTier: map[string]*big.Int{}, tierMicros: map[string]*big.Int{"flash": big.NewInt(11000), "heavy": big.NewInt(9000), "pro": big.NewInt(9000)}}
	finishCosts(w)
	if w.Cost.Int64() != 3 || w.ByTier["flash"].Int64() != 1 || w.ByTier["heavy"].Int64() != 1 || w.ByTier["pro"].Int64() != 1 {
		t.Fatalf("unexpected partition %+v total %v", w.ByTier, *w.Cost)
	}
	w = &workRow{Cost: v, ByTier: map[string]*big.Int{}, tierMicros: map[string]*big.Int{"flash": big.NewInt(1000), "heavy": big.NewInt(1000), "pro": big.NewInt(1000)}}
	finishCosts(w)
	if w.Cost.Int64() != 1 || w.ByTier["flash"].Int64() != 1 || w.ByTier["heavy"].Int64() != 0 || w.ByTier["pro"].Int64() != 0 {
		t.Fatalf("tie partition %+v total %v", w.ByTier, *w.Cost)
	}
}

func TestExactStreamTotalSeparateFromDisplay(t *testing.T) {
	s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{"a": {ID: "a", Kind: "work", Stream: "s", Tier: "flash", Cost: 1000, Priced: true}, "b": {ID: "b", Kind: "work", Stream: "s", Tier: "pro", Cost: 1000, Priced: true}, "c": {ID: "c", Kind: "work", Stream: "s", Tier: "heavy", Cost: 1000, Priced: true}}}
	d := mapSnapshot(s, time.Time{})
	w := d.Tables["work"].(map[string]*workRow)["s"]
	if d.StreamCosts["s"].amounts["total_cost"].Int64() != 3000 || w.Cost == nil || w.Cost.Int64() != 1 {
		t.Fatalf("raw=%v display=%v", d.StreamCosts["s"].amounts["total_cost"], w.Cost)
	}
}
func TestMoneyCellGrouping(t *testing.T) {
	v := big.NewInt(123456)
	if got := moneyCell(v); got != "$1,234.56" {
		t.Fatalf("money=%q", got)
	}
}

func TestStreamCostWireExact(t *testing.T) {
	cases := []struct {
		name   string
		values []int64
		want   string
	}{
		{"zero", []int64{0}, "$0.00"},
		{"subcent", []int64{1}, "$0.01"},
		{"boundary", []int64{290000}, "$0.29"},
		{"above boundary", []int64{290001}, "$0.30"},
		{"grouped sum", []int64{1234560000, 10000}, "$1,234.57"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &streamCost{amounts: map[string]*big.Int{}, unpriced: 2}
			for _, n := range tc.values {
				s.add("total_cost", n)
			}
			b, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err = json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}
			if got["total_cost"] != tc.want || got["unpriced_runs"] != float64(2) {
				t.Fatalf("wire = %s", b)
			}
		})
	}
	empty, err := json.Marshal(&streamCost{amounts: map[string]*big.Int{}, unpriced: 1})
	if err != nil || string(empty) != `{"unpriced_runs":1}` {
		t.Fatalf("unpriced only = %s, %v", empty, err)
	}
}
