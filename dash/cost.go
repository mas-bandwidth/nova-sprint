package dash

import (
	"math/big"
	"sort"
)

type tierPart struct {
	name      string
	cents     *big.Int
	remainder int64
}

// finishCosts partitions the rounded stream total by largest fractional cent,
// with alphabetical ties, matching v1's tierCostCents.
func finishCosts(w *workRow) {
	if w.Cost == nil {
		return
	}
	parts := make([]tierPart, 0, len(w.tierMicros))
	total := new(big.Int)
	whole := new(big.Int)
	unit := big.NewInt(10000)
	for name, micro := range w.tierMicros {
		q, r := new(big.Int), new(big.Int)
		q.QuoRem(micro, unit, r)
		parts = append(parts, tierPart{name, q, r.Int64()})
		total.Add(total, micro)
		whole.Add(whole, q)
	}
	rounded, rem := new(big.Int), new(big.Int)
	rounded.QuoRem(total, unit, rem)
	if rem.Sign() > 0 {
		rounded.Add(rounded, big.NewInt(1))
	}
	left := new(big.Int).Sub(rounded, whole).Int64()
	sort.Slice(parts, func(i, j int) bool {
		if parts[i].remainder == parts[j].remainder {
			return parts[i].name < parts[j].name
		}
		return parts[i].remainder > parts[j].remainder
	})
	for i, p := range parts {
		if int64(i) < left {
			p.cents.Add(p.cents, big.NewInt(1))
		}
		v, _ := new(big.Float).SetInt(p.cents).Float64()
		w.ByTier[p.name] = v / 100
	}
	v, _ := new(big.Float).SetInt(rounded).Float64()
	*w.Cost = v / 100
}
