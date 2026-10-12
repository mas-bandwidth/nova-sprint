package sprint

import (
	"strings"
	"testing"
)

// The stream branch (docs/SPEC-SPRINT.md section 7, the sprint branch): the first add of a
// stream writes its branch and trunk on its control card, and a card cut on dev or main is
// refused in every stream, marked or not (ProtectedLandingWhy), never re-cut; a cleared
// sprint builds the same on its first add, with no setting written.

// TestFirstAddWritesTheStreamBranchAndTrunk pins that an add of a card naming no BASE to a
// new stream writes stream_branch=sprint/s and trunk=dev on the control card.
func TestFirstAddWritesTheStreamBranchAndTrunk(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "c1", Brief: "title\ntier: flash"}}, Who: "coordinator"}))
	ctl := w.s.StreamCtl("s")
	if got := ctl.F(FieldStreamBranch); got != "sprint/s" {
		t.Errorf("stream_branch=%q, want sprint/s", got)
	}
	if got := ctl.F(FieldTrunk); got != "dev" {
		t.Errorf("trunk=%q, want dev", got)
	}
}

// TestAddRefusesADevOrMainCard pins that add refuses a card whose BASE: names dev or main in
// every stream, naming the flow and the one command that re-points the stream's base.
func TestAddRefusesADevOrMainCard(t *testing.T) {
	t.Parallel()
	for _, base := range ProtectedBranches {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, "reader-a", "reader-b")
			p := Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "c1", Brief: "title\ntier: flash\nBASE: " + base, Base: base}}, Who: "coordinator"})
			if len(p.Refused) != 1 {
				t.Fatalf("refused=%v, want one refusal", p.Refused)
			}
			if !strings.Contains(p.Refused[0].Why, LandingFlow) {
				t.Errorf("refusal does not quote the flow: %q", p.Refused[0].Why)
			}
			if !strings.Contains(p.Refused[0].Why, "nova-sprint stream set s --base sprint/s") {
				t.Errorf("refusal does not name the fix: %q", p.Refused[0].Why)
			}
		})
	}
}

// TestAMainOnlyRepositoryTakesMainAsTrunk pins that a stream's trunk is main when its cards
// name main (branchAndTrunk), read back by Trunk.
func TestAMainOnlyRepositoryTakesMainAsTrunk(t *testing.T) {
	t.Parallel()
	branch, trunk := branchAndTrunk(nil, "s", []string{"main"})
	if branch != "sprint/s" || trunk != "main" {
		t.Errorf("branchAndTrunk=%q,%q, want sprint/s,main", branch, trunk)
	}
}

// TestAClearedSprintMakesTheSameStreamBranch pins that a cleared sprint (an epoch bump)
// writes the same fields on its first add, and no setting is written (the fields are the
// control card's, not a property).
func TestAClearedSprintMakesTheSameStreamBranch(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	p := w.must(Add(w.s, AddReq{Stream: "s", Cards: []CardAdd{{ID: "c1", Brief: "title\ntier: flash"}}, Who: "coordinator"}))
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
