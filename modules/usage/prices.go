package usage

// Table is the built-in price sheet: the rate of each model the fleet's
// benches run on API keys, in micro-dollars per million tokens, from the
// provider's own price page on the date given. The policy's prices() is
// consulted first; this table prices what it leaves unlisted, so a sprint
// whose policy keeps prices() at {} still prices every bench run (Glenn,
// 2026-11-09: "API stuff costs $$$ ... it's all zero cost so far, i don't
// believe that for a second"). A model in neither is unpriced=no-price,
// never a silent $0. Keyed by provider/model as a run reports it (Lookup
// also tries the model alone).
//
// Each entry cites its source. Change a price here only with the page it
// came from and the date read.
var Table = Sheet{
	// DeepSeek, https://api-docs.deepseek.com/quick_start/pricing, read
	// 2026-11-09: deepseek-flash (DeepSeek-V4.1-Flash-1017) input cache
	// miss $0.30, cache hit $0.006, output $1.20 per million at the standard
	// (peak) rate. Off-peak (all but 01:00-04:00 and 06:00-10:00 UTC on
	// weekdays) is half; a record says when it was reported, not when each
	// call was made, so the standard rate is taken and an off-peak run is
	// counted at up to twice its charge.
	// DeepSeek has no separate cache-write price: a write is a cache miss.
	"deepseek/deepseek-flash": {In: 300_000, CacheRead: 6_000, CacheWrite: 300_000, Out: 1_200_000},
	// The same page: deepseek-v4-pro (DeepSeek-V4-Pro-0813) input cache
	// miss $1.32, cache hit $0.044, output $3.96 per million, standard rate.
	"deepseek/deepseek-v4-pro": {In: 1_320_000, CacheRead: 44_000, CacheWrite: 1_320_000, Out: 3_960_000},
	// Inception, https://www.inceptionlabs.ai/ (the Mercury 3 card), read
	// 2026-11-09: input $0.10, output $0.35 per million, marked 50% off (the
	// list rate is $0.20 and $0.70; the discount's end is unannounced). The
	// cached-input price Inception's page does not give is the OpenCode
	// model catalog's, https://opencode.ai/data/inception/mercury-3.md:
	// $0.01 per million; no cache-write price is listed, so a write is
	// taken as input.
	"inception/mercury-3": {In: 100_000, CacheRead: 10_000, CacheWrite: 100_000, Out: 350_000},
}

// Repriced is a stored record's cost as the board and the audit views count
// it: the cost the engine stored when it priced the run; else, for an API
// run stored unpriced=no-price whose model Table now prices, its tokens at
// Table's rate (the run was recorded when no sheet named the model; its
// tokens and model are in the words). Anything else stays unpriced. The
// store's record is not changed: the words still say unpriced=no-price,
// and check counts these runs apart.
func Repriced(words string, cost int64, priced bool) (int64, bool) {
	if priced {
		return cost, true
	}
	r := MustParse(words)
	if r.Billing != BillingAPI || r.Unpriced != WhyNoPrice || !r.Tokens.Reported() {
		return 0, false
	}
	rate, ok := Table.Lookup(r.Model)
	if !ok {
		return 0, false
	}
	return Cost(r.Tokens, rate), true
}
