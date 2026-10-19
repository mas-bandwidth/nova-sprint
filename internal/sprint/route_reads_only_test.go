package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readsOnlySnap is a snapshot whose heavy tier has one route, heavy-um, reserved for
// reads (set --read-routes), beside a pro tier of one shared route; the clock is 1000 s.
func readsOnlySnap(t *testing.T) *Snapshot {
	t.Helper()
	s := &Snapshot{
		Now:   time.Unix(1000, 0).UTC(),
		Fleet: NewTable(Fleet),
		Work:  NewTable(Work),
		Routes: []Route{
			{Name: "pro-a", Tier: "pro", Provider: "p", Model: "a", Enabled: true},
			{Name: "heavy-um", Tier: "heavy", Provider: "u", Model: "um", Enabled: true},
		},
		Tiers:     map[string][]string{"pro": {"pro-a"}},
		Providers: map[string]Provider{"p": {Name: "p", Funded: true}, "u": {Name: "u", Funded: true}},
	}
	s.Work.SetProp(PropReadRoutes, "heavy-um")
	return s
}

// TestAReadsOnlyRouteIsPlacedForTheProviderGaps: a reads-only route is drawn by its tier's
// reads, so the gaps the check reports never name it as in no tier array.
func TestAReadsOnlyRouteIsPlacedForTheProviderGaps(t *testing.T) {
	t.Parallel()
	s := readsOnlySnap(t)
	routes, _ := s.ProviderGaps("")
	for _, g := range routes {
		assert.NotEqual(t, "heavy-um", g.Route, "the reads-only route is placed: its reads draw it")
	}
}

// TestAReadOfATierWhoseRoutesAreAllReadsOnlyKeepsItsRoute: with its one reads-only route
// avoided, a heavy read still runs on it (the exclusion lapses), never on no route.
func TestAReadOfATierWhoseRoutesAreAllReadsOnlyKeepsItsRoute(t *testing.T) {
	t.Parallel()
	s := readsOnlySnap(t)
	ri := routeIndexesOf(s)
	pr := &Card{ID: "h-1", Fields: map[string]string{"brief": "tier: heavy\n"}}
	set := s.readRouteOf(ri, pr, nil)
	require.Equal(t, "heavy-um", set[FieldRoute])
	set = s.readRouteOf(ri, pr, []string{"heavy-um"})
	assert.Equal(t, "heavy-um", set[FieldRoute], "the avoided reads-only route is taken again rather than none")
}
