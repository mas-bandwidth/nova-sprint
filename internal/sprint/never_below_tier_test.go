package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A card is never worked or read below its tier (the owner, 2026-10-16: "It is completely
// undefined that a card should run at the wrong tier. There is no aspect of the design of
// nova sprint that allows this."). Its tier is its brief's line 1; a route or a friend row
// at that tier or above works it, a reader at its read tier or above reads it, and when
// none is up it waits, never handed down. On 2026-10-15 heavy cards were dealt to fleet
// machines on a pro route while the heavy tier had no route, and heavy and frontier cards
// were read on pro routes. Time is the world's clock (t0), the stores are in memory.

// TestAHeavyCardIsNeverDealtOnAProRoute: a heavy card with only flash and pro routes up is
// not dealt, whatever tier its primary records below its own (a gate's wall wrote pro on
// it at add, or a deal of an older binary did), and never takes the pro route.
func TestAHeavyCardIsNeverDealtOnAProRoute(t *testing.T) {
	t.Parallel()
	for _, recorded := range []string{"", cardhdr.RouteFlash, cardhdr.RoutePro} {
		t.Run("recorded="+recorded, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 2}))
			w.s.Routes = []Route{
				{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true, Deadline: 600},
				{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "q", Enabled: true, Deadline: 600},
			}
			w.must(Add(w.s, AddReq{Stream: "s1", Cards: []CardAdd{{ID: "s1-1", Brief: "RESULT: s1-1 sha=000000000000 tier: heavy\n\nThe task."}}}))
			if recorded != "" {
				w.s.Work.Card("s1-1").Fields[FieldTierNow] = recorded
			}
			pr := w.s.Primary("s1-1")
			require.Equal(t, cardhdr.RouteHeavy, w.s.DealTier(pr), "the deal draws heavy, never the recorded "+recorded)
			now, _ := CardTiers(pr)
			require.Equal(t, cardhdr.RouteHeavy, now, "the card is on heavy")
			w.part(tickDealFixture, TickReq{})
			assert.Nil(t, w.s.Fleet.Card(WorkCardID("s1-1", 1)), "no heavy route is up: the heavy card waits, never dealt on pro")
			assert.Equal(t, Ready, w.s.Primary("s1-1").Col, "it waits ready")
		})
	}
}

// TestACriticalHeavyCardStaysHeavy: a critical card is raised to pro when it is below it,
// and never lowered: a critical heavy card is on heavy.
func TestACriticalHeavyCardStaysHeavy(t *testing.T) {
	t.Parallel()
	c := &Card{ID: "s1-1", Fields: map[string]string{"brief": "s1-1: work tier: heavy\n", FieldBehind: itoa(CriticalBehind)}}
	require.True(t, IsCritical(c))
	now, own := CardTiers(c)
	assert.Equal(t, cardhdr.RouteHeavy, own, "a critical heavy card's tier is heavy")
	assert.Equal(t, cardhdr.RouteHeavy, now, "and it is dealt on heavy")
	f := &Card{ID: "s1-2", Fields: map[string]string{"brief": "s1-2: work tier: flash\n", FieldBehind: itoa(CriticalBehind)}}
	_, own = CardTiers(f)
	assert.Equal(t, cardhdr.RoutePro, own, "a critical flash card is raised to pro")
}

// TestAGatesWallNeverLowersAHeavyCard: a slow gate admits a flash card on pro and writes
// nothing on a heavy card.
func TestAGatesWallNeverLowersAHeavyCard(t *testing.T) {
	t.Parallel()
	walls := map[string][]float64{"./internal/slow": {1500, 1500, 1500}}
	set, _ := gateTier(walls, gateBrief("heavy", "./internal/slow"))
	assert.Empty(t, set, "a heavy card is above pro: the gate's wall writes no tier on it")
	set, _ = gateTier(walls, gateBrief("flash", "./internal/slow"))
	assert.Equal(t, cardhdr.RoutePro, set[FieldTierNow], "a flash card whose gate flash cannot run is admitted pro")
}

// TestAHeavyCardIsNeverReadOnAProRoute: a heavy card in review with only pro routes up has
// its read tier heavy, and the fleet reader with no heavy route is asked nothing; a heavy
// or a frontier friend reads it; a pro friend never does.
func TestAHeavyCardIsNeverReadOnAProRoute(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) *world {
		w := readCardsWorld(t, 4, "m1", "m2")
		w.s.Routes = []Route{{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "q", Enabled: true, Deadline: 600}}
		putReviewBy(w, "s1-1", "s1-1: work (s1) tier: heavy\n", "zoe", 1)
		w.s.Work.Card("s1-1").Fields[FieldTierNow] = cardhdr.RouteHeavy
		require.Equal(t, cardhdr.RouteHeavy, w.s.readTierOf(w.s.Work.Card("s1-1")), "a heavy card is read at heavy")
		return w
	}
	t.Run("only pro routes and fleet readers", func(t *testing.T) {
		t.Parallel()
		w := setup(t)
		dealReads(t, w, nil)
		assert.Empty(t, readCardsOf(w, "s1-1"), "no fleet reader reads a heavy card on a pro route")
	})
	t.Run("a pro friend", func(t *testing.T) {
		t.Parallel()
		w := setup(t)
		dealReads(t, w, workerSeatFixtures([]FriendSeat{readerSeat("pat", 4, []string{"pro"}, []string{"reader"})}))
		assert.Empty(t, readCardsOf(w, "s1-1"), "a pro friend never reads a heavy card")
	})
	t.Run("a heavy and a frontier friend", func(t *testing.T) {
		t.Parallel()
		w := setup(t)
		dealReads(t, w, workerSeatFixtures([]FriendSeat{
			readerSeat("jon", 4, []string{"heavy"}, []string{"reader"}),
			readerSeat("ada", 4, []string{"frontier"}, []string{"reader"}),
			readerSeat("pat", 4, []string{"pro"}, []string{"reader"}),
		}))
		assert.ElementsMatch(t, []string{FriendRow("jon"), FriendRow("ada")}, eReadRows(w, "s1-1"), "the heavy and the frontier friend read it, never the pro friend or a pro route")
		for _, rc := range readCardsOf(w, "s1-1") {
			assert.Equal(t, cardhdr.RouteHeavy, rc.F(FieldTier), "read at heavy")
		}
	})
	t.Run("a heavy route", func(t *testing.T) {
		t.Parallel()
		w := setup(t)
		w.s.Routes = append(w.s.Routes, Route{Name: "heavy-a", Tier: cardhdr.RouteHeavy, Provider: "p", Model: "h", Enabled: true, Deadline: 600})
		dealReads(t, w, nil)
		reads := readCardsOf(w, "s1-1")
		require.Len(t, reads, 2, "a heavy card's two reads go to the fleet readers on the heavy route")
		for _, rc := range reads {
			assert.Equal(t, cardhdr.RouteHeavy, rc.F(FieldTier), "read at heavy")
			assert.Equal(t, "heavy-a", rc.F(FieldRoute), "on the heavy route, never pro")
		}
	})
}

// TestAFrontierCardIsReadOnlyByAFrontierFriend: a frontier card's read tier is frontier;
// no route serves frontier, so the fleet readers (with heavy and pro routes up) are asked
// nothing, a heavy friend is asked nothing, and a frontier friend reads it.
func TestAFrontierCardIsReadOnlyByAFrontierFriend(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) *world {
		w := readCardsWorld(t, 4, "m1", "m2")
		w.s.Routes = []Route{
			{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "q", Enabled: true, Deadline: 600},
			{Name: "heavy-a", Tier: cardhdr.RouteHeavy, Provider: "p", Model: "h", Enabled: true, Deadline: 600},
		}
		putReviewBy(w, "s1-1", "s1-1: work (s1) tier: frontier\n", "zoe", 1)
		require.Equal(t, cardhdr.RouteFrontier, w.s.readTierOf(w.s.Work.Card("s1-1")), "a frontier card is read at frontier")
		return w
	}
	t.Run("fleet readers and a heavy friend", func(t *testing.T) {
		t.Parallel()
		w := setup(t)
		dealReads(t, w, workerSeatFixtures([]FriendSeat{readerSeat("jon", 4, []string{"heavy"}, []string{"reader"})}))
		assert.Empty(t, readCardsOf(w, "s1-1"), "neither a heavy route nor a heavy friend reads a frontier card")
	})
	t.Run("a frontier friend", func(t *testing.T) {
		t.Parallel()
		w := setup(t)
		dealReads(t, w, workerSeatFixtures([]FriendSeat{
			readerSeat("jon", 4, []string{"heavy"}, []string{"reader"}),
			readerSeat("ada", 4, []string{"frontier"}, []string{"reader"}),
		}))
		reads := readCardsOf(w, "s1-1")
		require.Len(t, reads, 1, "the one frontier friend reads it, and no one else")
		assert.Equal(t, FriendRow("ada"), reads[0].Row)
		assert.Equal(t, cardhdr.RouteFrontier, reads[0].F(FieldTier), "read at frontier")
	})
}

// TestAProCardMayBeReadAbove: a reader above a card's read tier may read it: a pro card
// with no pro reader up is read by a heavy friend; and with a pro reader up the pro reader
// is first (the cheaper).
func TestAProCardMayBeReadAbove(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 4)
	putReviewBy(w, "s1-1", "s1-1: work (s1) tier: pro\n", "zoe", 1)
	dealReads(t, w, workerSeatFixtures([]FriendSeat{
		readerSeat("jon", 4, []string{"heavy"}, []string{"reader"}),
		readerSeat("fred", 4, []string{"flash"}, []string{"reader"}),
	}))
	assert.Equal(t, []string{FriendRow("jon")}, eReadRows(w, "s1-1"), "the heavy friend reads the pro card; the flash friend never does")
}
