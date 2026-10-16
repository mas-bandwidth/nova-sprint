package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A card is dealt on its tier (route.go, tierLadder; the owner, 2026-10-16: a card is
// never worked or read below its tier): the tier a brief's line 1 names is its first deal's
// and its ceiling, and at its bound the judgment is the coordinator's. Flash first (the
// owner, 2026-10-02) and the escalation below a card's tier that carried it are gone. On
// the mem twin, injected clock, no socket.

// setPrimary writes fields onto a primary as a step that wrote them would leave it, on
// the store's own backend (the mem twin, or Redis in the functional tier). It guards on
// the work table as stored (setPrimaryOnTable), not on h.snap(): while the queue holds a
// change, the snapshot's WithQueue overlay reads the work table one revision ahead and the
// queued cards' revisions bumped, so a write guarded on it is refused ("mem: table revision
// mismatch").
func (h *harness) setPrimary(id string, set map[string]string) {
	h.t.Helper()
	h.setPrimaryOnTable(id, set)
}

// boundOut ends every take of the primary's live work card without a finish, as a
// provider failing it does, until the deal would pass the redeal bound: the attempt
// reaches its bound, and the machine's next ticks act on it.
func (h *harness) boundOut(id string) {
	h.t.Helper()
	for i := 0; i <= sprint.MaxRedeals; i++ {
		wc := h.snap().Work.Card(id).F("work")
		h.failTake(wc, providerLine)
		h.machine()
	}
	h.machine()
}

// flashAndPro is a store with two flash routes and two pro routes.
func flashAndPro(t *testing.T) *harness {
	return routeHarness(t, route("flash-a", "flash"), route("flash-b", "flash"), route("pro-a", "pro"), route("pro-b", "pro"))
}

// tierOfRoute is the tier of a route of flashAndPro, by its name.
func tierOfRoute(name string) string {
	switch name {
	case "flash-a", "flash-b":
		return "flash"
	case "pro-a", "pro-b":
		return "pro"
	}
	return ""
}

// A brief's tier on line 1 is the card's starting tier, not only its ceiling (the owner,
// 2026-10-03: seven of seven first flash attempts of pro cards died with no result, one
// dead attempt per card): a brief that says pro starts on a pro route and is never dealt
// below it; flash first applies to a brief that says flash or says no tier. The work card
// records the tier it was drawn from, its packet hands it to the child, and `card` shows
// the tier now and the ceiling.
func TestABriefThatSaysProStartsOnPro(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, brief, tier, ceiling string }{
		{"says pro", briefOf("pro", ""), "pro", "pro"},
		{"says flash", briefOf("flash", ""), "flash", "flash"},
		{"says no tier", "c: the work (s1)\n\nThe task.\n", "flash", "flash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := flashAndPro(t)
			h.addReady("s1", 1, tc.brief)
			h.must(DealStep(sprint.DealReq{}))
			wc := h.workCards()["s1-1.w1"]
			require.NotNil(t, wc)
			assert.Equal(t, tc.tier, tierOfRoute(wc.F(sprint.FieldRoute)), "dealt on %s", wc.F(sprint.FieldRoute))
			assert.Equal(t, tc.tier, wc.F(sprint.FieldTier), "the work card records the tier it was drawn from")
			assert.Equal(t, tc.tier, h.packetOf(wc.ID).Tier, "the packet hands the child its tier")
			pr := h.snap().Work.Card("s1-1")
			assert.Equal(t, tc.tier, pr.F(sprint.FieldTierNow), "the deal on a route writes the tier the card is on")
			now, ceiling := sprint.CardTiers(pr)
			assert.Equal(t, []string{tc.tier, tc.ceiling}, []string{now, ceiling})
			assert.Contains(t, sprint.AttemptLine(wc), " tier="+tc.tier+" ")
			h.clean("dealt on " + tc.tier)
		})
	}
}

// onFlashBelowPro admits a pro card that carries a flash tier on its primary, as a card
// dealt before line 1 became the starting tier did: it is on pro all the same (cardTier).
func (h *harness) onFlashBelowPro(stream string) {
	h.t.Helper()
	h.addReady(stream, 1, briefOf("pro", ""))
	h.setPrimary(stream+"-1", map[string]string{sprint.FieldTierNow: "flash"})
}

// At its ceiling the bound is the coordinator's: a pro card that reaches its bound on
// pro, and a flash card on flash, raise the judgment "a card reached its bound" and are
// dealt no more. A card the coordinator pinned to flash (rework --tier) never climbs.
func TestAtTheProBoundTheJudgmentIsRaised(t *testing.T) {
	t.Parallel()
	t.Run("a pro card at the pro bound", func(t *testing.T) {
		t.Parallel()
		h := flashAndPro(t)
		h.addReady("s1", 1, briefOf("pro", ""))
		h.startMachine()
		h.machine()
		require.Equal(t, "pro", h.snap().Work.Card("s1-1").F(sprint.FieldTierNow), "a brief that says pro starts on pro")
		h.boundOut("s1-1")
		pr := h.snap().Work.Card("s1-1")
		assert.Equal(t, 1, pr.Int("attempt"), "no second attempt: pro is its ceiling")
		assert.Equal(t, sprint.Ready, pr.Col)
		open := h.openOf(sprint.NBound)
		require.Len(t, open, 1, "the bound's judgment")
		assert.Contains(t, open[0].Note.What, "s1-1.w1: attempt 1 was redealt 3 times")
		h.clean("at the pro bound")
	})
	t.Run("a flash card at the flash bound", func(t *testing.T) {
		t.Parallel()
		h := flashAndPro(t)
		h.addReady("s1", 1, briefOf("flash", ""))
		h.startMachine()
		h.machine()
		h.boundOut("s1-1")
		assert.Equal(t, 1, h.snap().Work.Card("s1-1").Int("attempt"), "flash is its ceiling")
		assert.Len(t, h.openOf(sprint.NBound), 1)
	})
	t.Run("a pro card pinned to flash", func(t *testing.T) {
		t.Parallel()
		h := flashAndPro(t)
		h.addReady("s1", 1, briefOf("pro", ""))
		h.setPrimary("s1-1", map[string]string{sprint.FieldTier: "flash"})
		h.startMachine()
		h.machine()
		h.boundOut("s1-1")
		pr := h.snap().Work.Card("s1-1")
		assert.Equal(t, 1, pr.Int("attempt"), "the coordinator's tier is its ceiling")
		assert.Empty(t, pr.F(sprint.FieldTierNow))
		assert.Len(t, h.openOf(sprint.NBound), 1)
	})
}

// A pro card is never worked or read below its tier (the owner, 2026-10-16): one that
// carries a flash tier on its primary from before line 1 became its start is dealt on pro
// and read twice on pro routes, never on flash.
func TestAProCardWithAStaleFlashTierIsWorkedAndReadOnPro(t *testing.T) {
	t.Parallel()
	h := flashAndPro(t)
	require.NoError(t, h.st.BeatReaders(h.ctx))
	h.onFlashBelowPro("s1")
	h.startMachine()
	h.machine()
	require.Equal(t, "pro", h.workCards()["s1-1.w1"].F(sprint.FieldTier), "dealt on pro, never flash")
	h.work("m1")
	h.work("m2")
	h.machine()
	reads := h.snap().Readers.Of("s1-1")
	require.Len(t, reads, 2, "a pro card is read twice")
	for _, rc := range reads {
		assert.Equal(t, "pro", tierOfRoute(rc.F(sprint.FieldRoute)), "%s read on %s", rc.ID, rc.F(sprint.FieldRoute))
		assert.Equal(t, "pro", rc.F(sprint.FieldTier))
	}
	h.clean("a pro card read on pro")
}

// A store with no route escalates nothing (NextTier): its member runs its own model
// whatever the tier, so a pro card at its bound on a twin is the bound's judgment, never
// a silent second attempt.
func TestAStoreWithNoRouteEscalatesNothing(t *testing.T) {
	t.Parallel()
	h := routeHarness(t)
	h.addReady("s1", 1, briefOf("pro", ""))
	h.startMachine()
	h.machine()
	h.boundOut("s1-1")
	pr := h.snap().Work.Card("s1-1")
	assert.Equal(t, 1, pr.Int("attempt"), "no second attempt")
	assert.Empty(t, pr.F(sprint.FieldTierNow))
	assert.Len(t, h.openOf(sprint.NBound), 1, "the bound's judgment")
}
