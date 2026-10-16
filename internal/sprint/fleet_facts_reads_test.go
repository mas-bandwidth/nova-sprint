package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAGateNeedsJudgmentClosesOnceAMemberCarriesTheNeed: a deal that runs before any beat
// has carried facts (the first ticks after a restart, 2026-10-16 04:47Z) raises the
// gate-needs judgment naming every member with empty facts; once a beat's facts reach the
// control cards and the card is dealt, that judgment is closed, never left open naming
// "(facts: )" while the card works.
func TestAGateNeedsJudgmentClosesOnceAMemberCarriesTheNeed(t *testing.T) {
	w := fleetWorld(t, 1, 1, "m-darwin", "m-linux")
	w.s.Work.Card("s1-1").Fields["brief"] = "tier: pro\nGATE-NEEDS: linux\n\nThe task.\n"
	p, _ := TickDeal(w.s, TickReq{})
	w.must(p)
	require.NotEmpty(t, w.openOn("s1-1"), "no facts yet: the judgment is raised")

	w.part(TickPresence, TickReq{Beats: map[string]Beat{
		"m-darwin": {At: w.s.Now, Facts: []string{"darwin"}},
		"m-linux":  {At: w.s.Now, Facts: []string{"linux"}},
	}})
	p, _ = TickDeal(w.s, TickReq{})
	w.must(p)
	require.NotNil(t, w.s.Fleet.Card("s1-1.w1"), "the card is dealt once the linux member carries linux")
	for _, o := range w.openOn("s1-1") {
		assert.NotEqual(t, NGateNeeds, o.Note.Type, "the stale judgment is closed: %s", o.Note.What)
	}
}

// TestAFleetReaderIsNeverDealtAReadOnATierNoRouteServes: a fleet reader whose row names its
// tiers (reader set --tiers flash,pro,heavy,frontier) brings no model of its own, so a read
// whose tier no enabled route serves (heavy here: since v1.4.2 a card is read at its own tier,
// never handed down) is not dealt to it; dealt, it carried route "" model "" deadline 0 and
// every fleet reader refused it at launch (2026-10-16, "card <id>.r2.hetzner1 has no route").
// A pro read on the same readers still draws its route.
func TestAFleetReaderIsNeverDealtAReadOnATierNoRouteServes(t *testing.T) {
	w := readRouteRestWorld(t) // pro-a, pro-b; readers name flash,pro,heavy,frontier
	putReviewBy(w, "s1-2", "s1-2: work (s1) tier: heavy\n", "m1", 2)
	assert.False(t, w.s.readerServesTier("reader-m2", "heavy"), "no route serves heavy: a fleet reader does not serve it")
	assert.True(t, w.s.readerServesTier("reader-m2", "pro"), "a routed tier it names is served")

	dealReads(t, w, nil)
	assert.Empty(t, readCardsOf(w, "s1-2"), "no routeless heavy read is dealt to a fleet reader")
	pro := readCardsOf(w, "s1-1")
	require.NotEmpty(t, pro, "the pro primary is asked its reads")
	for _, c := range pro {
		assert.NotEmpty(t, c.F(FieldRoute), "a read dealt to a fleet reader carries its route")
		assert.NotEmpty(t, c.F(FieldModel))
		assert.NotEmpty(t, c.F(FieldDeadline))
	}
}
