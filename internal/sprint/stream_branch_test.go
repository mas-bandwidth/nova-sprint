package sprint

import (
	"strings"
	"testing"
)

// The stream branch (docs/SPEC-SPRINT.md section 7, the sprint branch): the first add of a
// stream writes its branch and trunk on its control card, a card cut on dev or main is
// re-cut on the branch, and a cleared sprint builds the same on its first add, with no
// setting written.

// TestFirstAddWritesTheStreamBranchAndTrunk pins that the first add of a card to a new stream
// writes stream_branch=sprint/s and trunk=<the card's BASE> on the control card, and no setting
// (the fields are the control card's, not a property).
func TestFirstAddWritesTheStreamBranchAndTrunk(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, base string }{
		{"FirstAddWritesTheStreamBranchAndTrunk", "dev"},  // BASE: dev takes trunk=dev
		{"AMainOnlyRepositoryTakesMainAsTrunk", "main"},   // BASE: main takes trunk=main
		{"AClearedSprintMakesTheSameStreamBranch", "dev"}, // a cleared sprint builds the same (docs/TESTS.md names it)
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, "reader-a", "reader-b")
			p := w.must(Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "c1", Brief: "title\ntier: flash\nBASE: " + tc.base, Base: tc.base}}, Who: "coordinator"}))
			if len(p.Props) != 0 {
				t.Errorf("no setting is written: %v", p.Props)
			}
			ctl := w.s.StreamCtl("s")
			if got := ctl.F(FieldStreamBranch); got != "sprint/s" {
				t.Errorf("stream_branch=%q, want sprint/s", got)
			}
			if got := ctl.F(FieldTrunk); got != tc.base {
				t.Errorf("trunk=%q, want %s", got, tc.base)
			}
		})
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

// TestLandingFlowIsOneSentence pins sprint.LandingFlow to THE FLOW sentence, byte for byte.
func TestLandingFlowIsOneSentence(t *testing.T) {
	t.Parallel()
	want := "The landing flow: card heads land in a batch on their stream's branch sprint/<stream>, oldest first, one writer per stream; stream branches merge one at a time into dev (main where a repository has no dev); a card never lands on dev or main."
	if LandingFlow != want {
		t.Errorf("LandingFlow=%q, want %q", LandingFlow, want)
	}
}
