package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// H11: a brief edit records the needs its DEPENDS-ON: line names. A ready
// card whose replaced brief names a need that has not landed is recorded with
// that need and moves waiting in the same unit, whatever else the brief
// changed (docs/SPEC-SPRINT.md, needs and the deal). Before the fix the
// ordinary path wrote the brief and dropped the parsed needs, so the card
// stayed ready and was dealt before its need landed.
func TestABriefEditRecordsItsNeeds(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dep"}, Brief: proBrief, Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"c"}, Brief: proBrief, Who: "coordinator"}))
	require.Equal(t, Ready, w.state("dep"))
	require.Equal(t, Ready, w.state("c"))
	// the new brief names dep on its DEPENDS-ON: line and changes another line,
	// so it is not the dependsOnly: path: the ordinary path must still record it
	newBrief := "tier: pro\nDEPENDS-ON: dep\n\nthe task, changed"
	p := Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "c", Brief: newBrief, Needs: []string{"dep"}}}, Who: "coordinator"})
	require.Empty(t, p.Refused, "refused: %+v", p.Refused)
	w.must(p)
	require.Equal(t, "dep", w.s.Work.Placed("c").F("needs"), "needs not recorded: %v", w.s.Work.Placed("c").Fields)
	require.Equal(t, Waiting, w.state("c"), "a ready card with an unlanded need stays ready and is dealt early")
}

// H11: a card in waiting or ready whose brief edit names needs all landed stays
// or becomes ready with the needs recorded.
func TestABriefEditWithLandedNeedsStaysReady(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dep"}, Brief: proBrief, Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"c"}, Brief: proBrief, Who: "coordinator"}))
	w.place(w.s.Work, "dep", "s1", Landed)
	newBrief := "tier: pro\nDEPENDS-ON: dep\n\nthe task, changed"
	p := Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "c", Brief: newBrief, Needs: []string{"dep"}}}, Who: "coordinator"})
	require.Empty(t, p.Refused, "refused: %+v", p.Refused)
	w.must(p)
	require.Equal(t, "dep", w.s.Work.Placed("c").F("needs"), "needs not recorded")
	require.Equal(t, Ready, w.state("c"), "all needs landed: it stays ready")
}

// H11: the same edit with no change to the DEPENDS-ON: line leaves the card's
// needs as they were.
func TestABriefEditLeavesUnchangedNeeds(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dep"}, Brief: proBrief, Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"c"}, Brief: proBrief, Needs: []string{"dep"}, Who: "coordinator"}))
	w.place(w.s.Work, "dep", "s1", Landed)
	require.Equal(t, "dep", w.s.Work.Placed("c").F("needs"))
	p := Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "c", Brief: "tier: pro\nDEPENDS-ON: dep\n\nthe task, changed", Needs: []string{"dep"}}}, Who: "coordinator"})
	require.Empty(t, p.Refused, "refused: %+v", p.Refused)
	w.must(p)
	require.Equal(t, "dep", w.s.Work.Placed("c").F("needs"), "the DEPENDS-ON: line did not change, so needs do not")
}

// H11: a ready card with a need not landed is dealt nothing, and the tick
// raises one judgment naming the card and its unlanded needs. The row is
// built in the store the way a corrupt row would be; no verb makes it.
func TestAReadyCardWithAnUnlandedNeedIsNotDealt(t *testing.T) {
	t.Parallel()
	w := setup(t, 0)
	w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"dep"}, Brief: proBrief, Who: "coordinator"}))
	w.s.Work.Put(&Card{ID: "bad", Row: "s1", Col: Ready, Score: 1, Rev: 1,
		Fields: map[string]string{"kind": "primary", "attempt": "0", "stream": "s1", "brief": proBrief, "needs": "dep"}})
	p := Deal(w.s, DealReq{Sel: Sel{Only: []string{"bad"}}})
	require.Empty(t, p.Units, "the deal dealt a ready card with an unlanded need: %+v", p.Units)
	// the tick raises one judgment naming the card and its unlanded need
	w.part(TickDeal, TickReq{})
	open := w.openOn("bad")
	require.Len(t, open, 1, "the tick raised %d judgments for bad, want one: %v", len(open), open)
	require.Contains(t, open[0].Note.What, "bad", "the judgment does not name the card: %q", open[0].Note.What)
	require.Contains(t, open[0].Note.What, "dep", "the judgment does not name the unlanded need: %q", open[0].Note.What)
	require.True(t, strings.Contains(w.state("bad"), "ready"), "bad is %s", w.state("bad"))
}
