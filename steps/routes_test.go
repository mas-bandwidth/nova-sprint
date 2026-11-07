package steps

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
)

// TestTickRoutesDealOnlyToServingRows: a card whose brief declares
// NEEDS-ROUTE is dealt only to a row that serves the route (roster.Worker
// Routes, from nova-config's route:<name> roles); with none it waits on the
// unserved judgement naming the route, and a row that serves it is dealt
// to.
func TestTickRoutesDealOnlyToServingRows(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.must("add", store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "tier: pro\nNEEDS-ROUTE: ipad\n\ndo p1"})
	s.run(3)
	s.requireState("p1", "ready")
	assert.Equal(t, []string{"unserved.pro+ipad.1"}, s.openJudgements("unserved"), "a tier served without the route does not clear it")
	assert.Contains(t, s.snap().Judgements["unserved.pro+ipad.1"].Question, "with route(s) ipad")
	s.roster.Workers["r2"] = roster.Worker{Name: "r2", Width: 1, Tiers: []string{"pro"}, Routes: []string{"ipad"}}
	s.must("row", store.PutRow{Name: "r2", Session: "hook-r2", Role: "worker"})
	s.must("seen", store.SetRowState{Row: "r2", State: "up"})
	s.run(2)
	s.requireState("p1", "working")
	assert.Empty(t, s.openJudgements("unserved"), "the cause cleared")
	for _, c := range s.snap().Cards {
		if c.Of == "p1" && c.State != "done" {
			assert.Equal(t, "r2", c.Holder, "dealt to the row that serves the route")
		}
	}
}
