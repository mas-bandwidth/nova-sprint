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

// partitionCents assigns the rounded total by largest fractional cent, with
// alphabetical ties. Inputs are nonnegative micro-dollar amounts.
func partitionCents(micros map[string]*big.Int) (*big.Int, map[string]*big.Int) {
	parts := make([]tierPart, 0, len(micros))
	total := new(big.Int)
	whole := new(big.Int)
	unit := big.NewInt(10000)
	for name, micro := range micros {
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
	result := make(map[string]*big.Int, len(parts))
	for i, p := range parts {
		if int64(i) < left {
			p.cents.Add(p.cents, big.NewInt(1))
		}
		result[p.name] = p.cents
	}
	return rounded, result
}

// finishCosts partitions the rounded stream total among its tiers.
func finishCosts(w *workRow) {
	if w.Cost == nil {
		return
	}
	w.Cost, w.ByTier = partitionCents(w.tierMicros)
}
