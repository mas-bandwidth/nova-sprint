package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A dropped card that has a successor is a permanent alias (docs/SPEC-SPRINT.md
// section 2; need_alias.go): drop --replaced-by records old => new on the old
// card's kept record, so a need on the old id resolves to the successor and no
// path can bring the old id back as a live need.
func TestADroppedCardWithASuccessorBecomesAnAlias(t *testing.T) {
	t.Parallel()
	w := setup(t, 1) // s1-1 ready
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"twin"}}))
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"b"}, Needs: []string{"s1-1"}}))
	require.Equal(t, Waiting, w.state("b"), "b waits on s1-1")

	w.must(Drop(w.s, DropReq{Sel: Sel{IDs: []string{"s1-1"}}, Reason: "recut", ReplacedBy: "twin", Who: "coordinator"}))

	require.Equal(t, "twin", AliasOf(w.s, "s1-1"), "the dropped card aliases its successor")
	require.Equal(t, []string{"twin"}, NamedWaits(w.s, w.s.Work.Card("b"), nil), "b's need resolves to the successor")
	needs, _ := NeedsOf(w.s, "b")
	require.Len(t, needs, 1)
	require.Equal(t, "twin", needs[0].ID, "the need is shown as the successor, not the dropped id")
	// the dropped id is no longer a dropped need: the alias target is what stands
	require.Empty(t, droppedNeeds(w.s, []string{"s1-1"}), "the old id is no longer a dropped need")
}

// A brief edit that names an old id stores the successor and NOTEs it (item 1):
// the text's DEPENDS-ON is the admission-time list, resolved through the alias.
func TestABriefEditNamingAnAliasStoresTheSuccessor(t *testing.T) {
	t.Parallel()
	w := setup(t, 1) // s1-1 ready
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"twin"}}))
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"b"}, Needs: []string{"s1-1"}, Brief: "b one\nDEPENDS-ON: s1-1"}))
	w.must(Drop(w.s, DropReq{Sel: Sel{IDs: []string{"s1-1"}}, Reason: "recut", ReplacedBy: "twin", Who: "coordinator"}))

	p := Brief(w.s, BriefReq{ID: "b", Brief: "b two\nDEPENDS-ON: s1-1", Needs: []string{"s1-1"}, Who: "coordinator"})
	require.NotEmpty(t, p.Units, "the edit writes: %+v", p.Refused)
	require.Contains(t, w.s.Work.Card("b").F("brief")+joined(p.Said), "need s1-1 is now twin", "the NOTE names the successor: %v", p.Said)
	w.do(p)
	require.Equal(t, "twin", w.s.Work.Card("b").F("needs"), "the stored need is the successor, never the old id")
}

// relink rewrites the need on EVERY card that names old, whatever its column
// (item 2): a working card that needs the old id is re-pointed too, keeps its
// column, and the old id becomes an alias.
func TestRelinkRewritesANeedInAnyColumn(t *testing.T) {
	t.Parallel()
	w := setup(t, 2) // s1-1, s1-2 ready
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"twin"}}))
	w.must(Deal(w.s, DealReq{Sel: Sel{Only: []string{"s1-1"}}}))
	w.must(NeedsSet(w.s, NeedsSetReq{ID: "s1-1", Needs: []string{"s1-2"}, Who: "coordinator"}))
	require.Equal(t, Working, w.state("s1-1"))
	require.Equal(t, Ready, w.state("s1-2"))

	w.must(Drop(w.s, DropReq{Sel: Sel{IDs: []string{"s1-2"}}, Reason: "recut", Who: "coordinator"}))
	w.must(Relink(w.s, RelinkReq{Old: []string{"s1-2"}, New: "twin", Who: "coordinator"}))

	require.Equal(t, "twin", w.s.Work.Card("s1-1").F("needs"), "the working card's need is re-pointed")
	require.Equal(t, Working, w.state("s1-1"), "the working card keeps its column")
}

func joined(ss []string) string {
	out := ""
	for _, s := range ss {
		out += "\n" + s
	}
	return out
}
