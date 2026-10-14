package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The stream branch (docs/SPEC-SPRINT.md section 7, the sprint branch): the first add of a
// stream writes its branch and trunk on its control card, a card cut on dev or main is
// re-cut on the branch, and a cleared sprint builds the same on its first add, with no
// setting written.

// TestAddWritesTheStreamBranchAndTrunk is table-driven over the bases a card cuts on:
// a dev base writes the branch and trunk, a main-only repository takes main as trunk, and
// a cleared sprint writes the same fields with no setting.
func TestAddWritesTheStreamBranchAndTrunk(t *testing.T) {
	cases := []struct {
		name   string
		base   string
		trunk  string
		cleared bool
	}{
		{name: "a dev base writes its branch and trunk", base: "dev", trunk: "dev"},
		{name: "a main only repository takes main as trunk", base: "main", trunk: "main"},
		{name: "a cleared sprint writes the same branch and no setting", base: "dev", trunk: "dev", cleared: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, "reader-a", "reader-b")
			p := w.must(Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "c1", Brief: "title\ntier: flash\nBASE: " + c.base, Base: c.base}}, Who: "coordinator"}))
			if c.cleared {
				assert.Empty(t, p.Props, "no setting is written")
			}
			ctl := w.s.StreamCtl("s")
			assert.Equal(t, "sprint/s", ctl.F(FieldStreamBranch))
			assert.Equal(t, c.trunk, ctl.F(FieldTrunk))
		})
	}
}

// TestAddRewritesADevBaseToTheStreamBranch pins that add re-cuts a card's BASE: dev onto the
// stream's branch sprint/s, and says so on one NOTE line quoting sprint.LandingFlow.
func TestAddRewritesADevBaseToTheStreamBranch(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	p := w.must(Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "c1", Brief: "title\ntier: flash\nBASE: dev", Base: "dev"}}, Who: "coordinator"}))
	assert.Equal(t, "sprint/s", baseOfBrief(w.s.Work.Card("c1").F("brief")))
	found := false
	for _, said := range p.Said {
		if strings.Contains(said, LandingFlow) {
			found = true
		}
	}
	assert.True(t, found, "no NOTE line quotes sprint.LandingFlow: %v", p.Said)
}

// TestLandingFlowIsOneSentence pins sprint.LandingFlow to THE FLOW sentence, byte for byte.
func TestLandingFlowIsOneSentence(t *testing.T) {
	t.Parallel()
	want := "The landing flow: card heads land in a batch on their stream's branch sprint/<stream>, oldest first, one writer per stream; stream branches merge one at a time into dev (main where a repository has no dev); a card never lands on dev or main."
	assert.Equal(t, want, LandingFlow)
}
