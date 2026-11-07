package card

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// LandFix (INTENT rule 16): a merging producer at its head goes back to
// ready with the finding and one bounce; refused off merging, at another
// head, or with no finding.
func TestLandFix(t *testing.T) {
	plan, r := Move(pr(Merging), nil, rows(), Stream{Name: "s1"}, nil, LandFix{Head: h1, Finding: "CI red: --- FAIL: TestBudgets"})
	if r != nil {
		t.Fatalf("refused: %s", r.Reason)
	}
	var state, finding string
	bounces := -1
	for _, w := range plan.Writes {
		switch x := w.(type) {
		case store.SetCardState:
			state = x.State
		case store.SetCardFinding:
			finding = x.Finding
		case store.SetCardBounces:
			bounces = x.Bounces
		}
	}
	if state != Ready || finding != "CI red: --- FAIL: TestBudgets" || bounces != 1 {
		t.Fatalf("state %q finding %q bounces %d: %+v", state, finding, bounces, plan.Writes)
	}
	held, r := Move(pr(Merging), nil, rows(), Stream{Name: "s1", Held: true}, nil, LandFix{Head: h1, Finding: "x"})
	if r != nil {
		t.Fatalf("held refused: %s", r.Reason)
	}
	for _, w := range held.Writes {
		if x, ok := w.(store.SetCardState); ok && x.State != Waiting {
			t.Fatalf("a held stream's fix waits, got %s", x.State)
		}
	}
	for _, c := range []struct {
		p Card
		e LandFix
	}{{pr(Review), LandFix{Head: h1, Finding: "x"}}, {pr(Merging), LandFix{Head: h2, Finding: "x"}}, {pr(Merging), LandFix{Head: h1}}} {
		if _, r := Move(c.p, nil, rows(), Stream{Name: "s1"}, nil, c.e); r == nil {
			t.Fatalf("LandFix %+v from %s accepted", c.e, c.p.State)
		}
	}
}
