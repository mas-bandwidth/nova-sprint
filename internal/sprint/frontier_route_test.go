package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// frontierTwin is a snapshot whose routes are the given tier's names (tier-a, ...)
// beside a flash route, and a primary of that tier: a frontier card, or its heavy twin
// that the machine escalated to heavy (FieldTierNow), so both draw from their own tier.
// Time is s.Now; there is no store, clock, socket or network.
func frontierTwin(t *testing.T, tier string, names ...string) (*Snapshot, func(id string) *Card) {
	t.Helper()
	s := newWorld(t).s
	s.Routes = []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "f", Enabled: true}}
	for _, n := range names {
		s.Routes = append(s.Routes, Route{Name: tier + "-" + n, Tier: tier, Provider: "p", Model: tier + "-" + n, Enabled: true})
	}
	card := func(id string) *Card {
		f := map[string]string{"kind": "primary", "attempt": "0", "stream": "s1", "brief": "c: a card tier: " + tier + "\n"}
		if tier != cardhdr.RouteFrontier {
			f[FieldTierNow] = tier
		}
		return &Card{ID: id, Row: "s1", Fields: f}
	}
	return s, card
}

// swapped is a heavy twin's answer in the frontier card's words.
func swapped(s string) string {
	return strings.ReplaceAll(s, cardhdr.RouteHeavy, cardhdr.RouteFrontier)
}

// An unpinned frontier card goes down the same path as a heavy card: drawn from the
// frontier tier's array of enabled, non-resting routes (drawRoute), the index moved; an
// up friend whose row lists frontier is weighed by the deal's cost rule as for heavy; only
// with no route drawable and no friend up does it wait for the coordinator under the
// tier's no-route judgment. A pinned card runs on its pin (tla/RouteIndex.tla,
// EveryTierIsDealtFromItsRoutes, PinNeverAdvances).
func TestAFrontierCardIsDealtFromItsRoutesLikeEveryTier(t *testing.T) {
	t.Parallel()
	frontier, heavy := cardhdr.RouteFrontier, cardhdr.RouteHeavy

	t.Run("one enabled route, no friend: drawn like heavy", func(t *testing.T) {
		t.Parallel()
		for _, tier := range []string{heavy, frontier} {
			s, card := frontierTwin(t, tier, "a")
			c := card("s1-1")
			set, got, why, byFriend := s.routeOf(c, nil, nil, "")
			require.Empty(t, why, tier)
			assert.Equal(t, tier, got, tier)
			assert.False(t, byFriend, tier)
			assert.Equal(t, tier+"-a", set[FieldRoute], tier)
			assert.Equal(t, tier, set[FieldTier], tier)
			sTier, sWhy, sBy := s.routeServed(c, nil, "")
			assert.Equal(t, []any{tier, "", false}, []any{sTier, sWhy, sBy}, "routeServed agrees with routeOf, %s", tier)
			_, nr := s.noRoute(c)
			assert.Empty(t, nr, "no judgment for a tier a route serves, %s", tier)
		}
	})

	t.Run("a route and a friend up: the frontier card answers as the heavy twin", func(t *testing.T) {
		t.Parallel()
		answer := func(tier string) []any {
			s, card := frontierTwin(t, tier, "a")
			s.Friends = []FriendSeat{{Name: "fay", Width: 2, Status: Up, Tiers: []string{tier}}}
			c := card("s1-1")
			_, got, why, byFriend := s.routeOf(c, nil, nil, "")
			sTier, sWhy, sBy := s.routeServed(c, nil, "")
			return []any{got, swapped(why), byFriend, sTier, swapped(sWhy), sBy}
		}
		want := answer(heavy)
		want[0], want[3] = frontier, frontier
		assert.Equal(t, want, answer(frontier))
	})

	t.Run("two routes: the array is read and the index moves", func(t *testing.T) {
		t.Parallel()
		for _, tier := range []string{heavy, frontier} {
			s, card := frontierTwin(t, tier, "a", "b")
			s.Tiers = map[string][]string{tier: {tier + "-b", tier + "-a"}}
			ri := routeIndexesOf(s)
			var took []string
			for _, id := range []string{"s1-1", "s1-2"} {
				set, _, why, _ := s.routeOf(card(id), nil, ri, "")
				require.Empty(t, why, "%s %s", tier, id)
				took = append(took, set[FieldRoute])
			}
			assert.Equal(t, []string{tier + "-b", tier + "-a"}, took, tier)
			require.NotNil(t, ri[tier], tier)
			assert.Equal(t, uint64(2), ri[tier].r.count, "the index moved once a deal, %s", tier)
		}
	})

	t.Run("the route rests, no friend: the coordinator's judgment names the rest", func(t *testing.T) {
		t.Parallel()
		s, card := frontierTwin(t, frontier, "a")
		s.rests = map[string]RouteRest{"frontier-a": {Route: "frontier-a", Provider: "p", At: t0, Until: t0.Add(RouteRestFor), Cards: []string{"x"}, Cause: RestProvider}}
		c := card("s1-1")
		set, got, why, byFriend := s.routeOf(c, nil, nil, "")
		assert.Nil(t, set)
		assert.Equal(t, frontier, got)
		assert.False(t, byFriend)
		assert.Contains(t, why, "no route and no friend serves tier frontier")
		assert.Contains(t, why, "frontier-a until")
		_, nr := s.noRoute(c)
		assert.Equal(t, why, nr)
	})

	t.Run("no frontier route, no friend: the BASE sentence word for word", func(t *testing.T) {
		t.Parallel()
		s, card := frontierTwin(t, frontier)
		_, got, why, byFriend := s.routeOf(card("s1-1"), nil, nil, "")
		assert.Equal(t, frontier, got)
		assert.False(t, byFriend)
		assert.Equal(t, "no route and no friend serves tier frontier; a frontier card waits for the coordinator: run it, or pin it with a model: <provider>/<model> line ("+
			"no enabled route and no up friend serves tier frontier: enable a route (nova-config route add <name> --tier frontier ..., name it in nova-config tier set frontier --routes <name,...>, then nova-config apply) or bring up a friend whose row lists frontier; or pin the card with a model: <provider>/<model> line)", why)
	})

	t.Run("a pinned frontier card runs on its pin and moves no index", func(t *testing.T) {
		t.Parallel()
		s, _ := frontierTwin(t, frontier, "a")
		c := &Card{ID: "s1-1", Row: "s1", Fields: map[string]string{"kind": "primary", "attempt": "0", "stream": "s1",
			"brief": "c: a card tier: frontier\nmodel: p/m\ntokens: 1000\ndeadline: 600\n"}}
		ri := routeIndexesOf(s)
		set, _, why, _ := s.routeOf(c, nil, ri, "")
		require.Empty(t, why)
		assert.Equal(t, RoutePin, set[FieldRoute])
		for tier, idx := range ri {
			assert.Zero(t, idx.r.count, "the pin moves no index, %s", tier)
			assert.Empty(t, idx.moves, tier)
		}
	})

	t.Run("the fleet's tiers leave the tier out: not dealt to a machine, named as for heavy", func(t *testing.T) {
		t.Parallel()
		for _, tier := range []string{heavy, frontier} {
			s, card := frontierTwin(t, tier, "a")
			s.Work.SetProp(PropFleetTiers, cardhdr.RouteFlash)
			set, got, why, byFriend := s.routeOf(card("s1-1"), nil, nil, "")
			assert.Nil(t, set, tier)
			assert.Equal(t, tier, got, tier)
			assert.False(t, byFriend, tier)
			assert.Contains(t, why, "the fleet's tiers are flash: nova-sprint set --fleet-tiers all", tier)
		}
	})
}
