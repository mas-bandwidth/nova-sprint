package usage

import (
	"math"
	"math/big"
	"strings"
)

// Rate is one model's price: micro-dollars per million tokens of each class
// (the policy's prices(): in, cache_read, cache_write, out). Reasoning is
// billed as output.
type Rate struct{ In, CacheRead, CacheWrite, Out int64 }

// Sheet is the policy's price sheet, by model.
type Sheet map[string]Rate

// Lookup is the rate of model, a record's provider/model: the sheet's entry
// for the whole name, else for the model alone (the part after the slash, as
// the catalog names it).
func (s Sheet) Lookup(model string) (Rate, bool) {
	if model == "" {
		return Rate{}, false
	}
	if r, ok := s[model]; ok {
		return r, true
	}
	if _, name, ok := strings.Cut(model, "/"); ok {
		r, ok := s[name]
		return r, ok
	}
	return Rate{}, false
}

// Billing is how a run on a row of harness is billed (the owner,
// 2026-11-07: "api rate work should be priced *exactly*. friend sub rate
// should be free."): a bench, harness nova-worker, runs on API keys; every
// other harness is a kin's subscription seat.
func Billing(harness string) string {
	if harness == "nova-worker" {
		return BillingAPI
	}
	return BillingSubscription
}

// Priced is a run's record as the engine stores it, its cost in
// micro-dollars, and whether it has a price.
type Priced struct {
	Record Record
	Cost   int64
	Priced bool
}

// Price prices a run's reported words (Parse) billed as billing, with
// charged (the --cost the row passed, an exact decimal of dollars; "" for
// none) kept as evidence and never as the price:
//
//   - subscription: cost 0, priced, the tokens kept;
//   - api with no token reported: unpriced=no-tokens;
//   - api whose model has no rate in sheet: unpriced=no-price, never a
//     silent $0;
//   - api: each class's tokens times its rate per million, reasoning at the
//     output rate, summed exactly and rounded half up to a micro-dollar.
//
// The words' own billing word is replaced: the row decides, not the run.
func Price(words, billing, charged string, sheet Sheet) (Priced, error) {
	r, err := Parse(words)
	if err != nil {
		return Priced{}, err
	}
	if r.Charged == "" && charged != "" {
		if r.Charged, err = Decimal(charged); err != nil {
			return Priced{}, err
		}
	}
	r.Billing, r.Unpriced = billing, ""
	if billing == BillingSubscription {
		return Priced{Record: r, Priced: true}, nil
	}
	r.Billing = BillingAPI
	if !r.Tokens.Reported() {
		r.Unpriced = WhyNoTokens
		return Priced{Record: r}, nil
	}
	rate, ok := sheet.Lookup(r.Model)
	if !ok {
		r.Unpriced = WhyNoPrice
		return Priced{Record: r}, nil
	}
	return Priced{Record: r, Cost: Cost(r.Tokens, rate), Priced: true}, nil
}

// Cost is t at rate, in micro-dollars: exact, rounded half up, at most
// MaxInt64.
func Cost(t Tokens, rate Rate) int64 {
	sum := new(big.Int)
	out := max(t.Output, 0) + max(t.Reasoning, 0)
	for _, c := range [][2]int64{{t.Input, rate.In}, {t.CacheRead, rate.CacheRead}, {t.CacheWrite, rate.CacheWrite}, {out, rate.Out}} {
		if c[0] > 0 && c[1] > 0 {
			sum.Add(sum, new(big.Int).Mul(big.NewInt(c[0]), big.NewInt(c[1])))
		}
	}
	sum.Add(sum, big.NewInt(500_000))
	sum.Quo(sum, big.NewInt(1_000_000))
	if !sum.IsInt64() {
		return math.MaxInt64
	}
	return sum.Int64()
}
