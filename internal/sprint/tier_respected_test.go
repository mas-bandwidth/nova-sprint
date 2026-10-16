package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tier is respected (the owner, 2026-10-16: "Tier must be respected. If no configured tier
// exists then that is a configuration issue, not something to work around."). A read or a
// card is never dealt on a tier no enabled route serves: no routeless packet, no member
// override, no lower tier, and no fleet reader bringing its own model. The deal raises the
// tier's one configuration judgment (NNoRoute, filed under tier:<tier>) naming the reads and
// the cards that wait, and it closes by itself once a route of the tier is enabled. The
// model is tla/ReaderTiers.tla (NoReadOnUnroutedTier, NoCardOnUnroutedTier,
// UnroutedIsJudged, JudgedOnlyWhileUnrouted).

const frontierTestBrief = "s1-2: frontier work (s1) tier: frontier\n\nThe task.\n"

// tierRespectWorld is the live shape of 2026-10-16: three members up with a reader row each
// that names every tier (reader set --tiers flash,pro,heavy,frontier), routes of flash and
// pro only, a frontier primary in review worked by m1 (s1-1), a frontier primary ready
// (s1-2), and a pro primary in review worked by m1 (s1-3), whose tier a route serves.
func tierRespectWorld(t *testing.T, readCards bool) *world {
	t.Helper()
	w := readCardsWorld(t, 4, "m1", "m2", "m3")
	if !readCards {
		w.readersTable()
		w.s.ReaderStates = map[string]string{"reader-m1": ReaderUp, "reader-m2": ReaderUp, "reader-m3": ReaderUp}
	}
	all := "flash,pro,heavy,frontier"
	w.s.Readers.Texts = map[string]map[string]string{
		"reader-m1": {ReaderTiers: all}, "reader-m2": {ReaderTiers: all}, "reader-m3": {ReaderTiers: all},
	}
	w.s.Routes = []Route{
		{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "flash-a", Tokens: 1000, Deadline: 600, Enabled: true},
		{Name: "pro-a", Tier: cardhdr.RoutePro, Provider: "p", Model: "pro-a", Tokens: 1000, Deadline: 600, Enabled: true},
	}
	putReviewBy(w, "s1-1", "s1-1: frontier work (s1) tier: frontier\n", "m1", 1)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"s1-2"}, Brief: frontierTestBrief}))
	putReviewBy(w, "s1-3", "s1-3: work (s1) tier: pro\n", "m1", 3)
	return w
}

// tierTick runs the tick's ask and deal parts once.
func tierTick(w *world) {
	w.t.Helper()
	w.part(friendAskPart(TickAsk), TickReq{})
	w.part(TickDeal, TickReq{})
}

// readsOf is every read of the primary at attempt 1 the deal placed, on either table.
func readsOf(w *world, primary string) []*Card {
	out := readCardsOf(w, primary)
	for _, c := range w.s.Readers.Cards() {
		if c.Placed() && c.F("kind") == "read" && c.F("primary") == primary {
			out = append(out, c)
		}
	}
	return out
}

// tierJudgments is the open judgments of the tier's no-route subject.
func tierJudgments(w *world, tier string) []Open {
	var out []Open
	for _, o := range w.s.Open {
		if o.Note.Type == NNoRoute && o.Note.Stream == TierSubject(tier) {
			out = append(out, o)
		}
	}
	return out
}

func openOfType(w *world, typ string) []Open {
	var out []Open
	for _, o := range w.s.Open {
		if o.Note.Type == typ {
			out = append(out, o)
		}
	}
	return out
}

func TestNoReadOrCardIsDealtOnATierNoRouteServes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		readCards bool
	}{{"read cards on", true}, {"the readers table", false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := tierRespectWorld(t, tc.readCards)
			for range 3 {
				tierTick(w)
				assert.Empty(t, readsOf(w, "s1-1"), "no frontier read is dealt: no route serves frontier")
				assert.Nil(t, w.s.Fleet.Card(WorkCardID("s1-2", 1)), "no frontier card is dealt: no route serves frontier")
				assert.Equal(t, Ready, w.state("s1-2"))
				js := tierJudgments(w, cardhdr.RouteFrontier)
				require.Len(t, js, 1, "the tier's one configuration judgment, raised once")
				assert.ElementsMatch(t, []string{"s1-1", "s1-2"}, js[0].Note.Primaries, "the read and the card that wait")
				assert.Contains(t, js[0].Note.What, "nova-config route add", "it names the configuration to set")
				for _, typ := range []string{NCannotAsk, NFewReaders, NNoFrontierRoom} {
					assert.Empty(t, openOfType(w, typ), "no second judgment of the same cause: %s", typ)
				}
			}
			// the routed tier is dealt as before: the pro primary's reads carry their route
			pro := readsOf(w, "s1-3")
			require.Len(t, pro, 2, "a pro primary is asked its two reads")
			for _, c := range pro {
				assert.Equal(t, "pro-a", c.F(FieldRoute))
				assert.Equal(t, "p/pro-a", c.F(FieldModel))
				assert.Equal(t, "600", c.F(FieldDeadline))
			}

			// a route of the tier is enabled: the read and the card are dealt on it, and the
			// judgment closes by itself
			w.s.Routes = append(w.s.Routes, Route{Name: "frontier-a", Tier: cardhdr.RouteFrontier, Provider: "p", Model: "frontier-a", Tokens: 1000, Deadline: 900, Enabled: true})
			tierTick(w)
			reads := readsOf(w, "s1-1")
			require.Len(t, reads, 2, "the frontier primary is asked its reads once a route serves frontier")
			for _, c := range reads {
				assert.Equal(t, "frontier-a", c.F(FieldRoute), "every read dealt carries its route")
				assert.Equal(t, "p/frontier-a", c.F(FieldModel))
				assert.Equal(t, "900", c.F(FieldDeadline))
			}
			wc := w.s.Fleet.Card(WorkCardID("s1-2", 1))
			require.NotNil(t, wc, "the frontier card is dealt once a route serves frontier")
			assert.Equal(t, "frontier-a", wc.F(FieldRoute))
			assert.Equal(t, "p/frontier-a", wc.F(FieldModel))
			assert.Empty(t, tierJudgments(w, cardhdr.RouteFrontier), "the judgment closes once a route of the tier is enabled")
		})
	}
}

// A friend's reader runs her own model and draws no fleet route, but only for the tiers her
// seat names (friendAtOrAbove): a flash friend's reader is never asked a frontier read, and
// a frontier friend's is, with no route on the card.
func TestAFriendReaderReadsOnlyTheTiersHerSeatNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tier  string
		reads bool
	}{{cardhdr.RouteFlash, false}, {cardhdr.RouteFrontier, true}} {
		t.Run(tc.tier, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, "reader-m1", "reader-amy", "reader-bob")
			w.readersTable()
			w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
			w.s.Friends = workerSeatFixtures([]FriendSeat{
				{Name: "amy", Width: 1, Status: Up, Tiers: []string{tc.tier}},
				{Name: "bob", Width: 1, Status: Up, Tiers: []string{tc.tier}},
			})
			w.s.Routes = []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Tokens: 1000, Deadline: 600, Enabled: true}}
			w.s.ReaderStates = map[string]string{"reader-m1": ReaderUp, "reader-amy": ReaderUp, "reader-bob": ReaderUp}
			putReview(w, "s1-1", "s1-1: frontier work tier: frontier\n\nThe task.", 1, 1, "0123456789abcdef0123456789abcdef01234567")
			w.part(TickAsk, TickReq{}) // the readers table's ask: the reader rows, readerServesTier
			reads := readsAt(w.s, w.s.Work.Card("s1-1"), 1)
			if !tc.reads {
				assert.Empty(t, reads, "a flash friend's reader is asked no frontier read, and no fleet reader is")
				return
			}
			require.Len(t, reads, 2)
			assert.ElementsMatch(t, []string{"reader-amy", "reader-bob"}, readerNames(reads))
			for _, rc := range reads {
				assert.Empty(t, rc.F(FieldRoute), "a friend runs her own model: no fleet route")
			}
		})
	}
}
