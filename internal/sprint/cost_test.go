package sprint

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardcost"
)

// The producer card carries what it cost (cost.go): one record per consumer key,
// set once, the total kept exactly with it, and past MaxCostRecords the total still
// exact and the list cut.

// book adds the consumers to the primary as steps would, one step each.
func book(pr *Card, cons ...Consumer) {
	for _, c := range cons {
		set := map[string]string{}
		addConsumer(pr, set, c)
		for k, v := range set {
			pr.Fields[k] = v
		}
	}
}

func consumerOf(key, at, usage string) Consumer {
	return Consumer{Kind: "read", Card: "s1-1.r1.reader-a", Attempt: 1, Who: "reader-a", End: "ok", At: at, Key: key, Usage: cardcost.ParseUsage(usage)}
}

func TestTheProducerKeepsOneRecordPerConsumerAndAnExactTotal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		cons    []Consumer
		records int
		input   int64
		charged string
	}{
		{name: "two consumers", cons: []Consumer{consumerOf("a#v", "2026-10-01T12:00:01Z", "input=10 actual_usd=0.1"), consumerOf("b#v", "2026-10-01T12:00:00Z", "input=20 predicted_usd=0.2")},
			records: 2, input: 30, charged: "0.3"},
		{name: "a consumer added twice counts once", cons: []Consumer{consumerOf("a#v", "2026-10-01T12:00:00Z", "input=10 actual_usd=0.1"), consumerOf("a#v", "2026-10-01T12:00:00Z", "input=10 actual_usd=0.1")},
			records: 1, input: 10, charged: "0.1"},
		{name: "a consumer with no cost is in the history and not in charged", cons: []Consumer{consumerOf("a#v", "2026-10-01T12:00:00Z", "input=5")},
			records: 1, input: 5, charged: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pr := &Card{ID: "s1-1", Fields: map[string]string{}}
			book(pr, tc.cons...)
			v := CardCostOf(pr)
			assert.Len(t, v.Consumers, tc.records)
			assert.Equal(t, tc.records, v.Total.Records)
			assert.Equal(t, tc.input, v.Total.Tokens.Input)
			assert.Equal(t, tc.charged, v.Total.Charged)
			for i := 1; i < len(v.Consumers); i++ {
				assert.LessOrEqual(t, v.Consumers[i-1].At, v.Consumers[i].At, "the history is in the order the consumers ended")
			}
		})
	}
}

func TestPastTheBoundTheTotalStaysExactAndTheListIsCut(t *testing.T) {
	t.Parallel()
	pr := &Card{ID: "s1-1", Fields: map[string]string{}}
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < MaxCostRecords+3; i++ {
		book(pr, consumerOf(fmt.Sprintf("c%03d#v", i), t0.Add(time.Duration(i)*time.Second).Format(time.RFC3339), "input=1 actual_usd=0.001"))
	}
	v := CardCostOf(pr)
	require.Len(t, v.Consumers, MaxCostRecords)
	assert.Equal(t, 3, v.Cut)
	assert.Equal(t, MaxCostRecords+3, v.Total.Records, "the total holds every consumer")
	assert.Equal(t, "0.067", v.Total.Charged)
	assert.Contains(t, v.CostLines()[len(v.CostLines())-1], " cut=3", "the total line says the list was cut")
}

// A zero harness cost on a route with no price sheet is not $0.00: a record whose
// run reported tokens, whose route has no sheet and whose harness actual is exactly
// zero keeps its tokens and its route, says unpriced=no-price-sheet and carries no
// figure, so it is one of the stream's unpriced runs and adds nothing to charged. A
// priced route still prices the record, and a non-zero harness actual on the unpriced
// route is still its charged figure.
func TestAZeroHarnessCostOnAnUnpricedRouteIsCountedUnpriced(t *testing.T) {
	t.Parallel()
	unpriced := Route{Name: "pro-mercury3", Tier: "pro", Provider: "mercury", Model: "mercury-3", Enabled: true}
	priced := Route{Name: "pro-a", Tier: "pro", Provider: "openrouter", Model: "vendor/m", Enabled: true,
		Prices: cardcost.Prices{Input: "1", Output: "10", ReasoningAsOutput: true}}
	noSheet := Route{Name: "pro-mercury3-pro", Tier: "pro", Provider: "mercury", Model: "mercury-3-pro", Enabled: true}
	s := &Snapshot{Now: t0, Fleet: NewTable(Fleet), Work: NewTable(Work), Routes: []Route{unpriced, noSheet, priced}}
	s.Work.SetRows([]string{"s1"})

	// take books one work take as the finish step does: its route priced by costRecord.
	take := func(id, route, usage string) *Card {
		c := &Card{ID: id, Row: "s1", Col: Working, Fields: map[string]string{}}
		u := cardcost.ParseUsage(costRecord(s, usage, route, "", false, "", ""))
		book(c, Consumer{Kind: "work", Card: id, Key: id + "#g0", End: "ok", At: stamp(t0), Usage: u})
		s.Work.Put(c)
		return c
	}

	// the unpriced route with a zero harness actual: unpriced, tokens and route kept,
	// no dollar figure, not charged.
	zero := take("s1-zero", "pro-mercury3", "input=1000 output=100 actual_usd=0 actual_by=harness")
	zu := CardCostOf(zero).Consumers[0].Usage
	assert.Equal(t, cardcost.WhyNoSheet, zu.Unpriced)
	assert.Equal(t, "pro-mercury3", zu.Route)
	assert.Empty(t, zu.Actual)
	assert.Equal(t, cardcost.CostNone, zu.Present())
	assert.Equal(t, int64(1000), zu.Tokens.Input, "the tokens are kept")
	zt := CardCostOf(zero).Total
	assert.Equal(t, 1, zt.Records)
	assert.Empty(t, zt.Charged, "the zero actual is not charged")
	assert.Equal(t, 0, zt.ChargedOf)

	// the same take on a priced route is charged its prediction.
	p := take("s1-priced", "pro-a", "input=1000000 output=100000")
	pt := CardCostOf(p).Total
	assert.Equal(t, "2", pt.Charged)
	assert.Equal(t, 1, pt.ChargedOf)

	// a non-zero harness actual on the unpriced route is still charged.
	nz := take("s1-nonzero", "pro-mercury3-pro", "input=1000 output=100 actual_usd=0.5 actual_by=harness")
	nzu := CardCostOf(nz).Consumers[0].Usage
	assert.Equal(t, "0.5", nzu.Actual)
	assert.Equal(t, cardcost.CostActual, nzu.Present())
	nzt := CardCostOf(nz).Total
	assert.Equal(t, "0.5", nzt.Charged)
	assert.Equal(t, 1, nzt.ChargedOf)

	// the stream counts the zero-cost unpriced take among its unpriced runs, and its
	// spend is only the priced take's and the non-zero actual's.
	tc := StreamTierCosts(s)["s1"]
	assert.Equal(t, 1, tc.UnpricedRuns, "the zero harness cost on the unpriced route is an unpriced run")
	assert.Equal(t, "$2.50", tc.TotalCost)
}

func TestARecordReadsBackAsTheConsumer(t *testing.T) {
	t.Parallel()
	c := Consumer{Kind: "work", Card: "s1-1.w2", Attempt: 2, Take: 1, Gen: 3, Who: "m1", Route: "flash-a", Model: "opencode/m", End: "provider failure",
		At: "2026-10-01T12:00:00Z", Key: "s1-1.w2#g3", Usage: cardcost.ParseUsage("input=10 actual_usd=0.1 actual_by=harness wait=2s run=9s")}
	got := parseConsumer(c.Key, c.line())
	assert.Equal(t, c, got)
}
