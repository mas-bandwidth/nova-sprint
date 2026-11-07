package dash

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// The cost tile is the API spend alone (the owner, 2026-11-07: "api rate work
// should be priced *exactly*. friend sub rate should be free."): a kin's run
// on a subscription seat adds its tokens and never a dollar; an API run with
// no price is counted unpriced, split no-tokens / no-price with the models
// prices() leaves out named; every holder's tokens reach its fleet or
// friends row.
func TestCostSummaryAPIOnlyWithSeatTokens(t *testing.T) {
	cards := map[string]store.Card{
		"p1": {ID: "p1", Kind: "producer", Stream: "s", State: "landed", Tier: "flash"},
		"p2": {ID: "p2", Kind: "producer", Stream: "s", State: "landed", Tier: "pro"},
		// a bench's API run, priced exactly from its tokens
		"p1.w1": {ID: "p1.w1", Kind: "work", Of: "p1", Stream: "s", State: "done", Result: "ok", Holder: "amd-1", Tier: "flash", Cost: 8658, Priced: true,
			Usage: "input=19541 cache_read=36336 output=692 reasoning=70 model=deepseek/deepseek-flash billing=api charged=0.015"},
		// a kin's read on a subscription seat: free, its tokens kept
		"p1.r1": {ID: "p1.r1", Kind: "read", Of: "p1", Stream: "s", State: "done", Result: "ok", Holder: "stella", Tier: "flash", Priced: true,
			Usage: "input=1200000 output=48000 model=anthropic/opus billing=subscription charged=0.9"},
		// the kin's whole card: p2 is landed with no API spend, out of the per-card divisor
		"p2.w1": {ID: "p2.w1", Kind: "work", Of: "p2", Stream: "s", State: "done", Result: "ok", Holder: "alex", Tier: "pro", Priced: true,
			Usage: "billing=subscription charged=-"},
		// API runs with no price: one with no tokens, two of models prices() leaves out
		"p2.r1": {ID: "p2.r1", Kind: "read", Of: "p2", Stream: "s", State: "done", Holder: "r", Tier: "pro", Usage: "billing=api charged=- unpriced=no-tokens"},
		"p2.r2": {ID: "p2.r2", Kind: "read", Of: "p2", Stream: "s", State: "done", Holder: "r", Tier: "pro", Usage: "input=10 output=5 model=inception/mercury-3 billing=api charged=- unpriced=no-price"},
		"p2.r3": {ID: "p2.r3", Kind: "read", Of: "p2", Stream: "s", State: "fail", Holder: "w", Tier: "pro", Usage: "input=3 model=openai/sol billing=api charged=- unpriced=no-price"},
		// a record from before round 25: priced as it was recorded
		"p2.r4": {ID: "p2.r4", Kind: "read", Of: "p2", Stream: "s", State: "done", Holder: "w", Tier: "pro", Cost: 1342, Priced: true},
	}
	s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: cards,
		Rows: map[string]store.Row{"amd-1": {Name: "amd-1", State: "up"}, "stella": {Name: "stella", State: "up"}, "alex": {Name: "alex", State: "up"}, "r": {Name: "r", State: "up"}, "w": {Name: "w", State: "up"}}}
	env := mapSnapshot(s, testRoster, time.Time{})
	c := env.Cost
	want := costSummary{Total: "$0.01", PerCard: "$0.01", ByTier: map[string]string{"flash": "$0.01", "pro": "$0.00"}, Priced: 2, Unpriced: 3, NoTokens: 1, NoPrice: 2,
		Missing: []string{"inception/mercury-3", "openai/sol"}, Landed: 2, PricedCards: 2, APITokens: "56.6K", SeatRuns: 2, SeatTokens: "1.2M"}
	b, _ := json.Marshal(c)
	w, _ := json.Marshal(want)
	if string(b) != string(w) {
		t.Fatalf("cost\n got %s\nwant %s", b, w)
	}
	fleet := env.Tables["fleet"].(map[string]*fleetRow)
	friends := env.Tables["friends"].(map[string]*fleetRow)
	if fleet["amd-1"].Tokens != 56639 || friends["stella"].Tokens != 1248000 || friends["alex"].Tokens != 0 || fleet["r"].Tokens != 15 || fleet["w"].Tokens != 3 {
		t.Fatalf("tokens by holder: amd-1 %d stella %d alex %d r %d w %d", fleet["amd-1"].Tokens, friends["stella"].Tokens, friends["alex"].Tokens, fleet["r"].Tokens, fleet["w"].Tokens)
	}
	row, _ := json.Marshal(friends["stella"])
	var cells map[string]string
	if err := json.Unmarshal(row, &cells); err != nil || cells["tokens"] != "1.2M" || cells["tokens_n"] != "1248000" {
		t.Fatalf("friends row %s", row)
	}
}

// Nothing priced on the API is "-", never a false $0, even beside seat runs.
func TestCostSummarySeatsOnlyIsNoDollar(t *testing.T) {
	s := store.Snapshot{Streams: map[string]store.Stream{"s": {Name: "s"}}, Cards: map[string]store.Card{
		"p1":    {ID: "p1", Kind: "producer", Stream: "s", State: "working", Tier: "pro"},
		"p1.w1": {ID: "p1.w1", Kind: "work", Of: "p1", Stream: "s", State: "done", Holder: "alex", Tier: "pro", Priced: true, Usage: "input=900 output=100 billing=subscription charged=-"},
	}}
	c := mapSnapshot(s, testRoster, time.Time{}).Cost
	if c.Total != "-" || c.PerCard != "-" || c.Priced != 0 || c.SeatRuns != 1 || c.SeatTokens != "1K" || c.Unpriced != 0 {
		t.Fatalf("seats only: %+v", c)
	}
}
