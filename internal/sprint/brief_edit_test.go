package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The brief verb replaces a card's whole text, and the server re-derives the
// card's needs from the new text's DEPENDS-ON line — which, in a text dumped from
// the card, is the admission-time list, not the relinked one. A text edit that
// does not mean to move an edge must not: the relink record wins over the text.

// relinkWorld is stream a with a (the old card), a-2 (its twin) and b-1 waiting
// on a; a -> a-2 is then relinked, so b-1 needs a-2 and records it.
func relinkWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-a")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a"}, Brief: "tier: pro\n\nthe first card", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-2"}, Brief: "tier: pro\n\nthe twin", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"b-1"}, Brief: "tier: pro\nDEPENDS-ON: a\n\nthe dependent", Needs: []string{"a"}, Who: "coordinator"}))
	w.must(Relink(w.s, RelinkReq{Old: []string{"a"}, New: "a-2", Reason: "re-cut as its twin", Who: "coordinator"}))
	require.Equal(t, "a-2", w.s.Work.Placed("b-1").F("needs"))
	return w
}

// A text dumped from the card still says DEPENDS-ON: a; the brief edit keeps
// a-2, the relink record's name, and says so in a NOTE line.
func TestBriefKeepsARelinkedNeedAgainstTheText(t *testing.T) {
	w := relinkWorld(t)
	text := "tier: pro\nDEPENDS-ON: a\n\nthe dependent"
	p := w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "b-1", Brief: text, Needs: []string{"a"}}}, Who: "coordinator"}))
	assert.Equal(t, "a-2", w.s.Work.Placed("b-1").F("needs"))
	require.Len(t, p.Said, 1)
	assert.Equal(t, "needs kept: a-2 (relinked from a 2030-01-02T03:04:05Z)", p.Said[0])
}

// A text that names a new dependency adds it, beside the kept relinked one.
func TestBriefAddsANewDependencyFromTheText(t *testing.T) {
	w := relinkWorld(t)
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"c-1"}, Brief: "tier: pro\n\nthe other", Who: "coordinator"}))
	text := "tier: pro\nDEPENDS-ON: a,c-1\n\nthe dependent"
	w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "b-1", Brief: text, Needs: []string{"a", "c-1"}}}, Who: "coordinator"}))
	assert.Equal(t, "a-2,c-1", w.s.Work.Placed("b-1").F("needs"))
}

// A text that drops a need keeps it, unless --needs-from-brief takes the needs
// wholly from the text.
func TestBriefKeepsADroppedNeedUnlessTakenFromTheText(t *testing.T) {
	w := relinkWorld(t)
	text := "tier: pro\n\nthe dependent"
	p := w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "b-1", Brief: text}}, Who: "coordinator"}))
	assert.Equal(t, "a-2", w.s.Work.Placed("b-1").F("needs"))
	require.NotEmpty(t, p.Said)

	text2 := "tier: pro\n\nthe dependent, again"
	w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "b-1", Brief: text2, NeedsFromBrief: true}}, Who: "coordinator"}))
	assert.Equal(t, "", w.s.Work.Placed("b-1").F("needs"))
}
