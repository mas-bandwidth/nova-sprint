package sprint

import (
	"math/big"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardcost"
)

// The owner, 2026-10-11: "The cost per-work stream should include all costs, including
// reading, fix, whatever done to land the card, such that the total cost of all work streams
// = the total cost of the sprint so far", and "capture all cost per-card, on the card itself,
// and then these cards sum up to the stream cost".

// spend books one take of usd on the card (actual, so no route prices it).
func spend(c *Card, key, usd string) {
	book(c, Consumer{Kind: "work", Card: c.ID, Key: key, End: "ok", At: stamp(t0),
		Usage: cardcost.ParseUsage("input=1 actual_usd=" + usd + " actual_by=harness")})
}

// readSpend books one read of usd on the card.
func readSpend(c *Card, key, usd string) {
	book(c, Consumer{Kind: "read", Card: c.ID + ".r1", Key: key, End: "ok", At: stamp(t0),
		Usage: cardcost.ParseUsage("input=1 actual_usd=" + usd + " actual_by=harness")})
}

// sumTotals is the streams' exact sums added.
func sumTotals(t *testing.T, tcs map[string]TierCosts) string {
	t.Helper()
	sum := new(big.Rat)
	for _, tc := range tcs {
		if tc.TotalUSD == "" {
			continue
		}
		r, ok := new(big.Rat).SetString(tc.TotalUSD)
		require.True(t, ok, tc.TotalUSD)
		sum.Add(sum, r)
	}
	return cardcost.Text(sum)
}

// costWorld is two streams: s1 with a landed card, a card in review that took a fix and a
// read, and a card about to be dropped; s2 with one working card.
func costWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-a")
	w.admit(AddReq{Stream: "s1", IDs: []string{"a", "b", "c"}, Brief: proBrief, Who: "coordinator"})
	w.admit(AddReq{Stream: "s2", IDs: []string{"d"}, Brief: proBrief, Who: "coordinator"})
	w.place(w.s.Work, "a", "s1", Landed)
	w.place(w.s.Work, "b", "s1", Review)
	w.place(w.s.Work, "d", "s2", Working)
	a, b, c, d := w.s.Work.Card("a"), w.s.Work.Card("b"), w.s.Work.Card("c"), w.s.Work.Card("d")
	spend(a, "a#g0", "1.25")
	a.Fields[FieldCost] = CardCostOf(a).Total.Charged
	spend(b, "b#g0", "2")
	spend(b, "b#g1", "0.5") // the fix
	readSpend(b, "b.r1#v", "0.125")
	spend(c, "c#g0", "4")
	spend(d, "d#g0", "8")
	return w
}

// Every card's spend is its stream's: landed, open, the fix and the read, and a card dropped
// off the table, which keeps its cost and is still summed (read by id: the control card
// lists it). The streams' sums equal every card's own total exactly, and the check says so.
func TestStreamCostIsEveryCardOfItAndTheStreamsSumToTheTotal(t *testing.T) {
	t.Parallel()
	w := costWorld(t)
	tcs := StreamTierCosts(w.s)
	assert.Equal(t, "$7.88", tcs["s1"].TotalCost, "1.25 + 2 + 0.5 + 0.125 + 4: every card, every take, the read")
	assert.Equal(t, "7.875", tcs["s1"].TotalUSD)
	assert.Equal(t, "1.25", tcs["s1"].LandedUSD, "per landed reads the landed cards' own cost alone")
	assert.Equal(t, "$1.25", tcs["s1"].PerLanded)
	assert.Equal(t, "$8.00", tcs["s2"].TotalCost)
	assert.Equal(t, CardsSpend(w.s), sumTotals(t, tcs))

	w.must(Drop(w.s, DropReq{Sel: Sel{Only: []string{"c"}}, Reason: "out of scope", Who: "coordinator"}))
	c := w.s.Work.Card("c")
	require.NotNil(t, c)
	assert.False(t, c.Placed(), "dropped: off the table, its record kept")
	assert.Equal(t, "c", w.s.StreamCtl("s1").F(FieldCostOff), "the stream lists its card off the table")
	assert.Contains(t, CostOffIDs(withoutKept(w.s, "c")), "c", "a read that does not hold it is told to read it")

	tcs = StreamTierCosts(w.s)
	assert.Equal(t, "$7.88", tcs["s1"].TotalCost, "a dropped card keeps its cost and is still summed")
	assert.Equal(t, 1, tcs["s1"].DroppedCards)
	k := CostCheckOf(w.s, tcs)
	assert.True(t, k.OK, "%+v", k)
	assert.Equal(t, "15.875", k.Cards)
	assert.Equal(t, k.Cards, k.Streams)
	assert.Equal(t, "COST OK sum(streams)=total $15.88 (exact 15.875)", k.Line())
	assert.Equal(t, CardsSpend(w.s), sumTotals(t, tcs), "Σ streams = the total by construction")
}

// withoutKept is the snapshot with the cards named taken out of the work table, as a read
// of the placed cards alone holds it.
func withoutKept(s *Snapshot, ids ...string) *Snapshot {
	c := *s
	c.Work = s.Work.Frozen()
	for _, id := range ids {
		c.Work.Drop(id)
	}
	return &c
}

// A recut card's spend goes on with its twin, counted once: the twin carries it
// (FieldCostCarried), the old card is marked carried to the twin and left out of the sum, a
// twin recut again carries the whole line, and the streams still sum to every card's total.
func TestRecutCarriesTheOldCardsSpendOnceOnTheTwin(t *testing.T) {
	t.Parallel()
	w := recutWorld(t)
	old := w.s.Work.Card("old")
	spend(old, "old#g0", "3")
	readSpend(old, "old.r1#v", "0.25")
	spend(w.s.Work.Card("other"), "other#g0", "1")
	before := StreamTierCosts(w.s)["s1"].TotalUSD
	assert.Equal(t, "4.25", before)

	w.must(Recut(w.s, RecutReq{ID: "old", Tier: "heavy", Who: "coordinator"}))
	tw := w.s.Work.Placed("oldb")
	require.NotNil(t, tw)
	assert.Equal(t, "3.25", tw.F(FieldCostCarried), "the twin carries the old card's whole spend")
	assert.Equal(t, "oldb", w.s.Work.Card("old").F(FieldCostCarriedTo), "the old card is marked carried")
	assert.Contains(t, Split(w.s.StreamCtl("s1").F(FieldCostOff)), "old", "the replace's drop lists it off the table")
	tcs := StreamTierCosts(w.s)
	assert.Equal(t, before, tcs["s1"].TotalUSD, "counted once: on the twin, not again on the old card")
	assert.Equal(t, 1, tcs["s1"].CarriedCards)
	assert.Equal(t, "$4.00", tcs["s1"].WorkCost, "the old card's records are still the stream's by kind: 3 + other's 1")
	assert.Equal(t, "$0.25", tcs["s1"].ReadCost)
	k := CostCheckOf(w.s, tcs)
	assert.True(t, k.OK, "%+v", k)

	spend(tw, "oldb#g0", "2")
	w.must(Recut(w.s, RecutReq{ID: "oldb", Brief: "tier: flash\nthe smaller scope", Who: "coordinator"}))
	tc := w.s.Work.Placed("oldc")
	require.NotNil(t, tc)
	assert.Equal(t, "5.25", tc.F(FieldCostCarried), "the twin of a twin carries the line: 3.25 carried + 2 its own")
	tcs = StreamTierCosts(w.s)
	assert.Equal(t, "6.25", tcs["s1"].TotalUSD, "old 3.25 + oldb 2 + other 1, each once")
	assert.True(t, CostCheckOf(w.s, tcs).OK)
	lines := CardCostOf(tc).CostLines()
	assert.Contains(t, lines[len(lines)-1], "COST CARRIED from=oldb usd=5.25")
}

// The check fails, with the gap, when spend is not counted once: a record that reached a
// card after its spend was carried to its twin, and a card in no stream of the table.
func TestCostCheckFailsWithTheGap(t *testing.T) {
	t.Parallel()
	w := recutWorld(t)
	spend(w.s.Work.Card("old"), "old#g0", "3")
	w.must(Recut(w.s, RecutReq{ID: "old", Tier: "heavy", Who: "coordinator"}))
	spend(w.s.Work.Card("old"), "old#late", "0.5") // after the carry: on no stream's sum
	k := CostCheckOf(w.s, StreamTierCosts(w.s))
	assert.False(t, k.OK)
	assert.Equal(t, "0.5", k.Gap)
	assert.Equal(t, "COST FAIL sum(streams)=$3.00 total=$3.50 gap=$0.50 (exact 3.5 - 3)", k.Line())

	stray := &Card{ID: "stray", Row: "gone", Col: Working, Fields: map[string]string{"kind": "primary"}}
	spend(stray, "stray#g0", "1")
	w.s.Work.Put(stray)
	k = CostCheckOf(w.s, StreamTierCosts(w.s))
	assert.False(t, k.OK)
	assert.Equal(t, []string{"stray"}, k.Orphans)
	assert.Contains(t, k.Line(), "1 card(s) in no stream of the table: stray")
}

// A run that reported no token is recorded and counted unpriced, never priced at $0 and
// never invented: the stream's cost is the priced records', the count beside it.
func TestUnpricedRunsAreCountedBesideTheCostNeverAsZero(t *testing.T) {
	t.Parallel()
	w := costWorld(t)
	book(w.s.Work.Card("b"), Consumer{Kind: "work", Card: "b", Key: "b#g2", End: "no result", At: stamp(t0),
		Usage: cardcost.Usage{Tokens: cardcost.None(), Wait: cardcost.Unreported, Run: cardcost.Unreported, Unpriced: cardcost.WhyNoTokens}})
	tc := StreamTierCosts(w.s)["s1"]
	assert.Equal(t, 1, tc.UnpricedRuns)
	assert.Equal(t, "$7.88", tc.TotalCost, "the unpriced run adds no dollar")
	empty := newWorld(t)
	empty.must(Add(empty.s, AddReq{Stream: "s9", IDs: []string{"z"}, Brief: proBrief, Who: "coordinator"}))
	book(empty.s.Work.Card("z"), Consumer{Kind: "work", Card: "z", Key: "z#g0", End: "no result", At: stamp(t0),
		Usage: cardcost.Usage{Tokens: cardcost.None(), Wait: cardcost.Unreported, Run: cardcost.Unreported, Unpriced: cardcost.WhyNoTokens}})
	tz := StreamTierCosts(empty.s)["s9"]
	assert.Empty(t, tz.TotalCost, "nothing priced: no figure, never $0")
	assert.Equal(t, "-", CostCell(tz.TotalCost))
	assert.Equal(t, 1, tz.UnpricedRuns)
}

// A nova-decide call's spend is a decide record on the card it decided: a gate's call split
// over its batch so the parts sum to the whole, a score on its landed card (in its landed
// cost too), a brief on the card it is born with; priced by a route of the decide model
// when the store holds one, else kept with its tokens, counted apart and never priced.
func TestDecideCallsAreRecordedOnTheCardsTheyDecided(t *testing.T) {
	t.Parallel()
	parts := SplitDecideUsage(DecideUsage(1001, 10, ""), 3)
	require.Len(t, parts, 3)
	var in, out int64
	for _, p := range parts {
		u := cardcost.ParseUsage(p)
		in += u.Tokens.Input
		out += u.Tokens.Output
		assert.Equal(t, DecideModel, u.Model)
	}
	assert.Equal(t, int64(1001), in, "the parts sum to the call")
	assert.Equal(t, int64(10), out)
	est := cardcost.ParseUsage(DecideUsage(0, 0, "twelve bytes"))
	assert.Equal(t, int64(3), est.Tokens.Input, "no usage reported: the input estimated from the state")
	assert.True(t, slices.Contains(est.Extra, UsageEstimated), "and marked so: %v", est.Extra)

	w := costWorld(t)
	before := CostCheckOf(w.s, StreamTierCosts(w.s)).Cards
	gate := DecideCostReq{Stream: "s1", What: "gate", Op: "land/s1@abc@gate/pkg.TestX", Usage: map[string]string{}}
	for i, id := range []string{"a", "b"} {
		gate.Usage[id] = SplitDecideUsage(DecideUsage(2000, 20, ""), 2)[i]
	}
	w.must(RecordDecideCost(w.s, gate))
	again := RecordDecideCost(w.s, gate)
	assert.Empty(t, again.Units, "a replay records nothing twice")
	tc := StreamTierCosts(w.s)["s1"]
	assert.Equal(t, 2, tc.DecideUnpriced, "no route of the decide model: kept with its tokens, counted apart")
	assert.Equal(t, 0, tc.UnpricedRuns, "a decide call is no run")
	assert.Equal(t, before, CostCheckOf(w.s, StreamTierCosts(w.s)).Cards, "no price is made up")

	w.s.Routes = []Route{{Name: "decide-jev", Provider: "typesafe", Model: "jev-latest", Tier: "decide",
		Prices: cardcost.Prices{Input: "1", Output: "2", AsOf: "2026-10-11"}}}
	score := ScoreReq{Stream: "s1", Scores: []CardScore{{ID: "a", Op: "a@landed@abc", Class: "clean", P: 0.9, Usage: DecideUsage(1000000, 500000, "")}}}
	w.must(RecordScores(w.s, score))
	a := w.s.Work.Card("a")
	assert.Equal(t, "3.25", a.F(FieldCost), "the score is in the landed card's own cost: 1.25 + 1 + 1")
	tc = StreamTierCosts(w.s)["s1"]
	assert.Equal(t, "$2.00", tc.DecideCost)
	assert.Equal(t, "$2.00", tc.CostByTier[ConsumerDecide])
	assert.True(t, CostCheckOf(w.s, StreamTierCosts(w.s)).OK)

	add := AddReq{Stream: "s1", IDs: []string{"e"}, Brief: proBrief, Who: "coordinator", BriefOps: map[string]string{"e": "e@brief-1"},
		BriefRecord: "brief.jsonl", BriefUsage: map[string]string{"e": DecideUsage(2000000, 0, "")}}
	w.admit(add)
	e := CardCostOf(w.s.Work.Card("e"))
	require.Len(t, e.Consumers, 1, "the card is born with its brief decision's cost")
	assert.Equal(t, ConsumerDecide, e.Consumers[0].Kind)
	assert.Equal(t, "brief", e.Consumers[0].End)
	assert.Equal(t, "2", e.Total.Charged)
}
