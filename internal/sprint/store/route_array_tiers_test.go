package store

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// The route load reads the array of every tier a card is dealt on, frontier too
// (a-frontier-card-is-dealt-from-its-routes-like-every-tier): a frontier array the load
// never read left the deal walking the frontier routes in name order, the array ignored.
func TestTheRouteLoadReadsTheFrontierArray(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{cardhdr.RouteFlash, cardhdr.RoutePro, cardhdr.RouteHeavy, cardhdr.RouteFrontier}, routeArrayTiers)
}
