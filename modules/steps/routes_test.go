package steps

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
)

// TestTickResourcesDealOnlyToServingRows: a card whose brief declares
// RESOURCES tags/amounts is dealt only to a bench that fits them; with none it
// waits on the unserved judgement naming the resource, and clears once served.
func TestTickResourcesDealOnlyToServingRows(t *testing.T) {
	t.Parallel()
	s := newSim(t)
	s.stream("s1")
	s.row("r1", 2, "pro")
	s.must("add", store.PutCard{ID: "p1", Kind: "producer", Stream: "s1", Tier: "pro", Brief: "tier: pro\nRESOURCES: ipad\n\ndo p1"})
	s.run(3)
	s.requireState("p1", "ready")
	assert.Equal(t, []string{"unserved.pro+ipad.1"}, s.openJudgements("unserved"), "a tier served without the tag does not clear it")
	assert.Contains(t, s.snap().Judgements["unserved.pro+ipad.1"].Question, "with resource(s) ipad")
	s.roster.Workers["r2"] = roster.Worker{Name: "r2", Tiers: []string{"pro"}, Tags: []string{"ipad"}, Resources: map[string]int{"cpu": 1, "memory": 1}}
	s.must("row", store.PutRow{Name: "r2", Session: "hook-r2", Role: "worker"})
	s.must("seen", store.SetRowState{Row: "r2", State: "up"})
	s.run(2)
	s.requireState("p1", "working")
	assert.Empty(t, s.openJudgements("unserved"), "the cause cleared")
	for _, c := range s.snap().Cards {
		if c.Of == "p1" && c.State != "done" {
			assert.Equal(t, "r2", c.Holder, "dealt to the row that fits the tag")
		}
	}
}
