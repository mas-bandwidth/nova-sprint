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

// TestFirstAddWritesTheStreamBranchAndTrunk pins that an add of a card with BASE: dev to a
// new stream writes stream_branch=sprint/s and trunk=dev on the control card.
func TestFirstAddWritesTheStreamBranchAndTrunk(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "c1", Brief: "title\ntier: flash\nBASE: dev", Base: "dev"}}, Who: "coordinator"}))
	ctl := w.s.StreamCtl("s")
	assert.Equal(t, "sprint/s", ctl.F(FieldStreamBranch))
	assert.Equal(t, "dev", ctl.F(FieldTrunk))
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

// TestAMainOnlyRepositoryTakesMainAsTrunk pins that a card with BASE: main takes trunk=main
// and stream_branch=sprint/s.
func TestAMainOnlyRepositoryTakesMainAsTrunk(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "c1", Brief: "title\ntier: flash\nBASE: main", Base: "main"}}, Who: "coordinator"}))
	ctl := w.s.StreamCtl("s")
	assert.Equal(t, "sprint/s", ctl.F(FieldStreamBranch))
	assert.Equal(t, "main", ctl.F(FieldTrunk))
}

// TestAClearedSprintMakesTheSameStreamBranch pins that a cleared sprint (an epoch bump)
// writes the same fields on its first add, and no setting is written (the fields are the
// control card's, not a property).
func TestAClearedSprintMakesTheSameStreamBranch(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	p := w.must(Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "c1", Brief: "title\ntier: flash\nBASE: dev", Base: "dev"}}, Who: "coordinator"}))
	assert.Empty(t, p.Props, "no setting is written")
	ctl := w.s.StreamCtl("s")
	assert.Equal(t, "sprint/s", ctl.F(FieldStreamBranch))
	assert.Equal(t, "dev", ctl.F(FieldTrunk))
}

// TestLandingFlowIsOneSentence pins sprint.LandingFlow to THE FLOW sentence, byte for byte.
func TestLandingFlowIsOneSentence(t *testing.T) {
	t.Parallel()
	want := "The landing flow: card heads land in a batch on their stream's branch sprint/<stream>, oldest first, one writer per stream; stream branches merge one at a time into dev (main where a repository has no dev); a card never lands on dev or main."
	assert.Equal(t, want, LandingFlow)
}
