package sprint

import (
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/cardcost"
)

// The where record's recount, 2026-10-17 (a 12-tick idle profile of the server, the machine
// STOPPED): each card's cost records were parsed four times a recount, each card's brief
// header read twice and more, and the sprint's figures counted once a stream. These pin each
// card's records parsed once, its brief read once, and the figures the same as before.

// memoParses is the cost memo's parses so far.
func memoParses() int {
	costMemo.mu.Lock()
	defer costMemo.mu.Unlock()
	return costMemo.parses
}

// memoBriefReads is the tier memo's brief reads so far.
func memoBriefReads() int {
	tierMemo.mu.Lock()
	defer tierMemo.mu.Unlock()
	return tierMemo.reads
}

// A recount (the streams' figures and their check) parses each card's cost records once,
// and a second recount over the same cards parses none: a card replaced whole is parsed
// again, and so is one changed in place (a view is kept only while its card carries the
// fields it was parsed from). Not parallel: the counters are the package's.
func TestCardCostOfParsesEachCardOncePerRecount(t *testing.T) {
	w := costWorld(t)
	cards := 0
	for _, c := range w.s.Work.Cards() {
		// each card read anew, as the twin reads a record: none parsed yet
		cp := *c
		cp.Fields = maps.Clone(c.Fields)
		w.s.Work.Put(&cp)
		if !IsSentinel(c) {
			cards++
		}
	}
	require.Positive(t, cards)
	before := memoParses()
	first, check := StreamTierCostsChecked(w.s)
	assert.Equal(t, cards, memoParses()-before, "one parse a card, not four")
	assert.Equal(t, StreamTierCosts(w.s), first, "the figures as StreamTierCosts counts them")
	assert.Equal(t, CostCheckOf(w.s, first), check, "the check as CostCheckOf counts it")
	assert.Equal(t, cards, memoParses()-before, "the second recount parsed nothing")

	// replaced whole, as the twin replaces a card: parsed again, and the new figure counted
	b := w.s.Work.Card("b")
	nb := *b
	nb.Fields = map[string]string{}
	for k, v := range b.Fields {
		nb.Fields[k] = v
	}
	spend(&nb, "b#g2", "1")
	w.s.Work.Put(&nb)
	mark := memoParses()
	again := StreamTierCosts(w.s)
	assert.Equal(t, 1, memoParses()-mark, "the replaced card alone")
	assert.Equal(t, "8.875", again["s1"].TotalUSD)

	// changed in place (no step does; a test may): parsed again, never the kept view
	spend(&nb, "b#g3", "1")
	mark = memoParses()
	assert.Equal(t, "9.875", StreamTierCosts(w.s)["s1"].TotalUSD)
	assert.Equal(t, 1, memoParses()-mark)
	assert.Equal(t, cardCostOf(&nb), CardCostOf(&nb))

	// the caller's list is its own: a kept view is not changed through it
	v := CardCostOf(&nb)
	require.NotEmpty(t, v.Consumers)
	v.Consumers[0].Kind = "changed"
	assert.NotEqual(t, "changed", CardCostOf(&nb).Consumers[0].Kind)
}

// The total-only sites read the charged figure off the total alone (chargedOf): the figure
// CardCostOf's total carries, for every total a step writes.
func TestChargedOfIsTheTotalsChargedFigure(t *testing.T) {
	t.Parallel()
	c := &Card{ID: "x", Fields: map[string]string{}}
	assert.Equal(t, cardCostOf(c).Total.Charged, chargedOf(c.F(FieldCostTotal)), "no total")
	for i, usage := range []string{"input=1 actual_usd=0.5 actual_by=harness", "input=10 output=3", "input=1 predicted_usd=1.25", ""} {
		book(c, Consumer{Kind: "work", Card: c.ID, Key: fmt.Sprintf("x#g%d", i), End: "ok", At: stamp(t0), Usage: cardcost.ParseUsage(usage)})
		assert.Equal(t, cardCostOf(c).Total.Charged, chargedOf(c.F(FieldCostTotal)), "after %q: %s", usage, c.F(FieldCostTotal))
	}
	assert.NotEmpty(t, chargedOf(c.F(FieldCostTotal)))
}

// A card's brief tier is read once while its brief is the same, and attemptTier reads no
// brief when the card records its tier. Not parallel: the counter is the package's.
func TestTierWordReadsEachBriefOnce(t *testing.T) {
	c := &Card{ID: "t1", Fields: map[string]string{"brief": proBrief}}
	mark := memoBriefReads()
	assert.Equal(t, "pro", TierWord(c))
	assert.Equal(t, "pro", TierWord(c))
	assert.Equal(t, "pro", attemptTier(c))
	assert.Equal(t, 1, memoBriefReads()-mark, "one read of the brief")

	// a changed brief is read again
	c.Fields["brief"] = strings.Replace(proBrief, "pro", "flash", 1)
	assert.Equal(t, TierWord(&Card{Fields: map[string]string{"brief": c.Fields["brief"]}}), TierWord(c))
	assert.Equal(t, 3, memoBriefReads()-mark)

	// the tier the card records: no brief read
	d := &Card{ID: "t2", Fields: map[string]string{"brief": proBrief, FieldTierNow: "heavy"}}
	mark = memoBriefReads()
	assert.Equal(t, "heavy", attemptTier(d))
	d2 := &Card{ID: "t3", Fields: map[string]string{"brief": proBrief, FieldTier: "frontier"}}
	assert.Equal(t, "frontier", attemptTier(d2))
	assert.Zero(t, memoBriefReads()-mark)
	assert.Equal(t, "no tier", attemptTier(&Card{ID: "t4", Fields: map[string]string{}}))
}

// workCardBase is what the pattern \.w[0-9]+$ matched and took off, and the number after it.
func TestWorkCardBaseIsThePattern(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`\.w[0-9]+$`)
	for _, id := range []string{"s1-1.w1", "s1-1.w12", "s1-1", "s1-1.w", "s1-1.w1x", "a.w1.w2", "a.w.w3", "x.ww12", ".w7", "w7", "a.w0", "s1-1.w١", "s1-1.r1.reader-a", "s1-1.w1.g1", "a.w99999999999999999999"} {
		base, n, ok := workCardBase(id)
		assert.Equal(t, re.MatchString(id), ok, id)
		if ok {
			assert.Equal(t, re.ReplaceAllString(id, ""), base, id)
			want, _ := strconv.Atoi(id[strings.LastIndex(id, ".w")+2:])
			assert.Equal(t, want, n, id)
		}
	}
}

// A kept record put in, or dropped from, a table leaves its index (the cells hold the placed
// cards alone); a placed one builds it again. The cells read the same either way.
func TestAKeptRecordLeavesTheTablesIndex(t *testing.T) {
	t.Parallel()
	tb := NewTable(Work)
	tb.SetRows([]string{"s1"})
	tb.Put(&Card{ID: "a", Row: "s1", Col: Ready})
	require.Len(t, tb.Cell("s1", Ready), 1)
	require.NotNil(t, tb.cells)
	tb.Put(&Card{ID: "k", Fields: map[string]string{"stream": "s1"}}) // kept: no place
	assert.NotNil(t, tb.cells, "a kept record: the index stands")
	tb.Drop("k")
	assert.NotNil(t, tb.cells, "a kept record dropped: the index stands")
	assert.Len(t, tb.Cards(), 1)
	tb.Put(&Card{ID: "k", Fields: map[string]string{}})
	tb.Put(&Card{ID: "k", Row: "s1", Col: Ready}) // the kept record placed
	assert.Nil(t, tb.cells, "a placed record: the index is built again")
	assert.Len(t, tb.Cell("s1", Ready), 2)
	tb.Put(&Card{ID: "k", Fields: map[string]string{}}) // placed, now kept
	assert.Len(t, tb.Cell("s1", Ready), 1)
	tb.Drop("a")
	assert.Empty(t, tb.Cell("s1", Ready))
}

// KeepCardMemos keeps the views and tiers of the cards named and lets every other go: a
// copy a step planned on, or a card replaced since, is not held past the where record's
// count. Not parallel: the memos are the package's.
func TestKeepCardMemosLetsGoOfCardsNotNamed(t *testing.T) {
	live := &Card{ID: "live", Fields: map[string]string{"brief": proBrief}}
	dead := &Card{ID: "dead", Fields: map[string]string{"brief": proBrief}}
	for _, c := range []*Card{live, dead} {
		spend(c, c.ID+"#g0", "1")
		CardCostOf(c)
		TierWord(c)
	}
	KeepCardMemos([]*Card{live})
	costMemo.mu.Lock()
	_, liveCost := costMemo.cur[live]
	_, deadCost := costMemo.cur[dead]
	costMemo.mu.Unlock()
	tierMemo.mu.Lock()
	_, liveTier := tierMemo.cur[live]
	_, deadTier := tierMemo.cur[dead]
	tierMemo.mu.Unlock()
	assert.True(t, liveCost && liveTier, "the named card's view and tier kept")
	assert.False(t, deadCost || deadTier, "the other let go")
	assert.Nil(t, costMemo.old)
	mark := memoParses()
	CardCostOf(live)
	assert.Equal(t, mark, memoParses(), "kept: not parsed again")
	CardCostOf(dead)
	assert.Equal(t, mark+1, memoParses(), "let go: parsed again")
}
