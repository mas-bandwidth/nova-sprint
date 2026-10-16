package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The store with no route at all is fenced off (the owner, 2026-10-16: "Fence it off."):
// only the in-memory twin (Snapshot.MemTwin, set by the store for its Mem backend) runs
// every tier on its member's own --model with no route (twinRunsOwnModel). A real store
// (Redis) with no route serves no tier: no work card and no fleet read is dealt on any
// tier, and the deal raises each tier's one configuration judgment (NNoRoute, filed under
// tier:<tier>), which closes by itself once a route of the tier is enabled.
// tla/ReaderTiers.tla NoDealOnRealStoreWithoutRoutes (reversed witness
// MCReaderTiersBrokenRealShortcut.cfg).

var fenceTiers = []string{cardhdr.RouteFlash, cardhdr.RoutePro, cardhdr.RouteHeavy, cardhdr.RouteFrontier}

// fenceStream is the tier's stream in the fence world.
func fenceStream(tier string) string { return "s" + tier }

// fenceReview is the primary in review of a tier in the fence world, worked by m1.
func fenceReview(tier string) string { return fenceStream(tier) + "-1" }

// fenceReady is the ready primary of a tier in the fence world, beside its review.
func fenceReady(tier string) string { return fenceStream(tier) + "-2" }

// noRoutesWorld is a store with no route at all: three members up whose reader rows name
// every tier, and for each tier one primary in review worked by m1 (its reads wait to be
// dealt) and one primary ready in the tier's own stream. memTwin says the store is the
// in-memory twin; false is a real store.
func noRoutesWorld(t *testing.T, readCards, memTwin bool) *world {
	t.Helper()
	w := readCardsWorld(t, 8, "m1", "m2", "m3")
	if !readCards {
		w.readersTable()
		w.s.ReaderStates = map[string]string{"reader-m1": ReaderUp, "reader-m2": ReaderUp, "reader-m3": ReaderUp}
	}
	all := "flash,pro,heavy,frontier"
	w.s.Readers.Texts = map[string]map[string]string{
		"reader-m1": {ReaderTiers: all}, "reader-m2": {ReaderTiers: all}, "reader-m3": {ReaderTiers: all},
	}
	w.s.MemTwin = memTwin
	w.s.Routes = nil
	for i, tier := range fenceTiers {
		review := fenceReview(tier)
		putReviewBy(w, review, review+": "+tier+" work (s1) tier: "+tier+"\n", "m1", float64(i+1))
	}
	for _, tier := range fenceTiers {
		st, ready := fenceStream(tier), fenceReady(tier)
		w.must(Add(w.s, AddReq{Stream: st, IDs: []string{ready}, Brief: ready + ": " + tier + " work (" + st + ") tier: " + tier + "\n\nThe task.\n"}))
	}
	return w
}

// fenceRoute is an enabled route of the tier.
func fenceRoute(tier string) Route {
	return Route{Name: tier + "-a", Tier: tier, Provider: "p", Model: tier + "-a", Tokens: 1000, Deadline: 900, Enabled: true}
}

func TestARealStoreWithNoRoutesDealsNothingAndJudgesEveryTier(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		readCards bool
	}{{"read cards on", true}, {"the readers table", false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := noRoutesWorld(t, tc.readCards, false)
			require.False(t, w.s.twinRunsOwnModel(), "a real store never takes the twin's shortcut")
			for range 3 {
				tierTick(w)
				for _, tier := range fenceTiers {
					assert.Nil(t, w.s.Fleet.Card(WorkCardID(fenceReady(tier), 1)), "no %s card is dealt: a real store with no route serves no tier", tier)
					assert.Equal(t, Ready, w.state(fenceReady(tier)), "the %s card waits ready", tier)
					assert.Empty(t, readsOf(w, fenceReview(tier)), "no %s read is dealt: no fleet reader serves a tier no route does", tier)
					js := tierJudgments(w, tier)
					require.Len(t, js, 1, "the %s tier's one configuration judgment, raised once", tier)
					assert.ElementsMatch(t, []string{fenceReady(tier), fenceReview(tier)}, js[0].Note.Primaries, "the %s card and read that wait", tier)
					assert.Contains(t, js[0].Note.What, "nova-config route add", "it names the configuration to set")
				}
				for _, typ := range []string{NCannotAsk, NFewReaders, NNoFrontierRoom} {
					assert.Empty(t, openOfType(w, typ), "no second judgment of the same cause: %s", typ)
				}
			}
		})
	}
}

func TestTheMemTwinWithNoRoutesStillRunsEveryTierOnTheMembersOwnModel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		readCards bool
	}{{"read cards on", true}, {"the readers table", false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := noRoutesWorld(t, tc.readCards, true)
			require.True(t, w.s.twinRunsOwnModel(), "the twin with no route takes the shortcut")
			tierTick(w)
			for _, tier := range fenceTiers {
				wc := w.s.Fleet.Card(WorkCardID(fenceReady(tier), 1))
				require.NotNil(t, wc, "the twin deals the %s card with no route", tier)
				assert.Empty(t, wc.F(FieldRoute), "the member runs its own --model: no route")
				assert.Empty(t, wc.F(FieldModel))
				reads := readsOf(w, fenceReview(tier))
				require.NotEmpty(t, reads, "the twin asks the %s primary's reads", tier)
				for _, c := range reads {
					assert.Empty(t, c.F(FieldRoute), "the reader runs its own --model: no route")
				}
				assert.Empty(t, tierJudgments(w, tier), "the twin judges no %s tier unrouted", tier)
			}
		})
	}
}

func TestEnablingARouteOnARealStoreDealsThatTierAndClosesItsJudgment(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		readCards bool
	}{{"read cards on", true}, {"the readers table", false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := noRoutesWorld(t, tc.readCards, false)
			tierTick(w)
			for _, tier := range fenceTiers {
				require.Len(t, tierJudgments(w, tier), 1, "%s is judged unrouted first", tier)
			}
			// one route is enabled: its tier is dealt on it and its judgment closes by
			// itself; every other tier still waits under its own
			w.s.Routes = []Route{fenceRoute(cardhdr.RouteFlash)}
			require.False(t, w.s.twinRunsOwnModel())
			tierTick(w)
			wc := w.s.Fleet.Card(WorkCardID(fenceReady(cardhdr.RouteFlash), 1))
			require.NotNil(t, wc, "the flash card is dealt once a route serves flash")
			assert.Equal(t, "flash-a", wc.F(FieldRoute))
			assert.Equal(t, "p/flash-a", wc.F(FieldModel))
			reads := readsOf(w, fenceReview(cardhdr.RouteFlash))
			require.NotEmpty(t, reads, "the flash primary is asked its read once a route serves flash")
			for _, c := range reads {
				assert.Equal(t, "flash-a", c.F(FieldRoute), "every read dealt carries its route")
				assert.Equal(t, "900", c.F(FieldDeadline))
			}
			assert.Empty(t, tierJudgments(w, cardhdr.RouteFlash), "the flash judgment closes once a route of it is enabled")
			for _, tier := range fenceTiers[1:] {
				assert.Nil(t, w.s.Fleet.Card(WorkCardID(fenceReady(tier), 1)), "no %s card: still no route of it", tier)
				assert.Empty(t, readsOf(w, fenceReview(tier)), "no %s read: still no route of it", tier)
				assert.Len(t, tierJudgments(w, tier), 1, "%s still waits under its one judgment", tier)
			}
		})
	}
}
