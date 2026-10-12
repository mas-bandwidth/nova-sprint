package sprint

import (
	"strings"
	"testing"
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
	if got := ctl.F(FieldStreamBranch); got != "sprint/s" {
		t.Errorf("stream_branch=%q, want sprint/s", got)
	}
	if got := ctl.F(FieldTrunk); got != "dev" {
		t.Errorf("trunk=%q, want dev", got)
	}
}

// TestAddRewritesADevBaseToTheStreamBranch pins that add re-cuts a card's BASE: dev onto the
// stream's branch sprint/s, and says so on one NOTE line quoting sprint.LandingFlow.
func TestAddRewritesADevBaseToTheStreamBranch(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	p := w.must(Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "c1", Brief: "title\ntier: flash\nBASE: dev", Base: "dev"}}, Who: "coordinator"}))
	if got := baseOfBrief(w.s.Work.Card("c1").F("brief")); got != "sprint/s" {
		t.Errorf("stored brief BASE=%q, want sprint/s", got)
	}
	found := false
	for _, said := range p.Said {
		if strings.Contains(said, LandingFlow) {
			found = true
		}
	}
	if !found {
		t.Errorf("no NOTE line quotes sprint.LandingFlow: %v", p.Said)
	}
}

// TestAMainOnlyRepositoryTakesMainAsTrunk pins that a card with BASE: main takes trunk=main
// and stream_branch=sprint/s.
func TestAMainOnlyRepositoryTakesMainAsTrunk(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "c1", Brief: "title\ntier: flash\nBASE: main", Base: "main"}}, Who: "coordinator"}))
	ctl := w.s.StreamCtl("s")
	if got := ctl.F(FieldStreamBranch); got != "sprint/s" {
		t.Errorf("stream_branch=%q, want sprint/s", got)
	}
	if got := ctl.F(FieldTrunk); got != "main" {
		t.Errorf("trunk=%q, want main", got)
	}
}

// TestAClearedSprintMakesTheSameStreamBranch pins that a cleared sprint (an epoch bump)
// writes the same fields on its first add, and no setting is written (the fields are the
// control card's, not a property).
func TestAClearedSprintMakesTheSameStreamBranch(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	p := w.must(Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "c1", Brief: "title\ntier: flash\nBASE: dev", Base: "dev"}}, Who: "coordinator"}))
	if len(p.Props) != 0 {
		t.Errorf("no setting is written: %v", p.Props)
	}
	ctl := w.s.StreamCtl("s")
	if ctl.F(FieldStreamBranch) != "sprint/s" || ctl.F(FieldTrunk) != "dev" {
		t.Errorf("stream_branch=%q trunk=%q, want sprint/s dev", ctl.F(FieldStreamBranch), ctl.F(FieldTrunk))
	}
}

// TestLandingFlowIsOneSentence pins sprint.LandingFlow to THE FLOW sentence, byte for byte.
func TestLandingFlowIsOneSentence(t *testing.T) {
	t.Parallel()
	want := "The landing flow: card heads land in a batch on their stream's branch sprint/<stream>, oldest first, one writer per stream; stream branches merge one at a time into dev (main where a repository has no dev); a card never lands on dev or main."
	if LandingFlow != want {
		t.Errorf("LandingFlow=%q, want %q", LandingFlow, want)
	}
}
