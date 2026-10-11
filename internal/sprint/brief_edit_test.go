package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A brief edit is the whole text, and the server re-derives the card's needs from that
// text's DEPENDS-ON: line -- which, in a text dumped from the card, is the admission-time
// list, not the relinked one. So a relink was undone by the next brief edit, a re-tier
// written on the card was read back from line 1, and the attempt bound counted the text,
// not the task. These pin the interim rule that the card's own fields win over the text
// (docs/SPEC-SPRINT.md, the brief verb; the owner, 2026-10-11: "I'm feeling like cards
// should probably be more structured").

// relinkedWorld is stream a with a-2 waiting on a-1, a-1 relinked to its twin a-3: a-2
// needs a-3 and holds the relink record (FieldRelinked).
func relinkedWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-a")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "one", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-2"}, Needs: []string{"a-1"}, Brief: "DEPENDS-ON: a-1\ntwo", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-3"}, Brief: "the twin", Who: "coordinator"}))
	w.must(Relink(w.s, RelinkReq{Old: []string{"a-1"}, New: "a-3", Who: "coordinator"}))
	require.Equal(t, "a-3", w.s.Work.Placed("a-2").F("needs"), "the relink re-pointed a-2 to a-3")
	require.NotEmpty(t, w.s.Work.Placed("a-2").F(FieldRelinked), "a-2 holds the relink record")
	return w
}

// A brief --brief-file of a text still naming the dropped id keeps the relinked name, and
// the brief verb says so in a NOTE line.
func TestBriefEditKeepsARelinkedNeed(t *testing.T) {
	t.Parallel()
	w := relinkedWorld(t)
	p := w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-2", Brief: "DEPENDS-ON: a-1\ntwo", Needs: []string{"a-1"}}}, Who: "coordinator"}))
	assert.Equal(t, "a-3", w.s.Work.Placed("a-2").F("needs"), "the relink record wins over the text")
	assert.Contains(t, strings.Join(p.Said, "\n"), "needs kept: a-3 (relinked from a-1",
		"the brief verb NOTEs the need it kept against the text")
}

// A text that names a new dependency adds it; the needs the card holds are kept.
func TestBriefEditAddsANewNeedAndKeepsTheRest(t *testing.T) {
	t.Parallel()
	w := relinkedWorld(t)
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-4"}, Brief: "new", Who: "coordinator"}))
	w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-2", Brief: "DEPENDS-ON: a-4\ntwo", Needs: []string{"a-4"}}}, Who: "coordinator"}))
	needs := Split(w.s.Work.Placed("a-2").F("needs"))
	assert.Contains(t, needs, "a-4", "a text naming a new dependency adds it")
	assert.Contains(t, needs, "a-3", "a need the card holds is kept")
}

// A text that drops a need keeps it unless --needs-from-brief is given.
func TestBriefEditDropsANeedOnlyWithNeedsFromBrief(t *testing.T) {
	t.Parallel()
	w := relinkedWorld(t)
	w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-2", Brief: "two only"}}, Who: "coordinator"}))
	assert.Equal(t, "a-3", w.s.Work.Placed("a-2").F("needs"), "the need is kept without --needs-from-brief")
	w = relinkedWorld(t)
	w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-2", Brief: "two only"}}, Who: "coordinator", NeedsFromBrief: true}))
	assert.Empty(t, w.s.Work.Placed("a-2").F("needs"), "--needs-from-brief takes the text as the whole truth")
}

// The tier is the card's own field: a text whose line 1 names another keeps the recorded
// tier and NOTEs it, unless --tier re-tiers the card instead (briefTier).
func TestBriefEditKeepsTheRecordedTier(t *testing.T) {
	t.Parallel()
	w := editWorld(t)
	w.s.Work.Placed("a-1").Fields[FieldTier] = "heavy"
	p := w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-1", Brief: "tier: flash\nnew brief"}}, Who: "coordinator"}))
	assert.Equal(t, "heavy", w.s.Work.Placed("a-1").F(FieldTier), "the recorded tier is kept")
	assert.Contains(t, strings.Join(p.Said, "\n"), "tier kept: heavy", "the brief verb NOTEs the tier it kept")
}

// The attempt bound hashes the task, not the text: an edit of lines that are no part of the
// task does not reset the count, and brief says which it got.
func TestBriefEditBindsTheTaskNotTheText(t *testing.T) {
	t.Parallel()
	w := editWorld(t)
	c := w.s.Work.Placed("a-1")
	c.Fields["attempt"] = "1"
	c.Fields["brief"] = "tier: pro\nPATHS: x.go\nORDER: 1\nthe body"
	sameTaskBrief := "tier: flash\nPATHS: x.go\nORDER: 2\nthe body"
	p := w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-1", Brief: sameTaskBrief}}, Who: "coordinator"}))
	assert.Contains(t, strings.Join(p.Said, "\n"), "bound: same task (1 attempts stand)",
		"an edit of the tier and ORDER leaves the bound standing")

	w = editWorld(t)
	c = w.s.Work.Placed("a-1")
	c.Fields["attempt"] = "1"
	c.Fields["brief"] = "tier: pro\nPATHS: x.go\nthe body"
	newTaskBrief := "tier: pro\nPATHS: x.go\na different body"
	p = w.must(Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-1", Brief: newTaskBrief}}, Who: "coordinator"}))
	assert.Contains(t, strings.Join(p.Said, "\n"), "bound: new task (count reset)",
		"an edit of the body resets the bound")
}

// taskOf is the task: the body after the header, plus PATHS, NEW, SHARED, TEST and
// DONE-WHEN; the run lines (tier, DEPENDS-ON, ORDER, TLC-HOSTS, MERGE, TOOLCHAIN-HOSTS)
// are not the task.
func TestTaskOfLeavesOutTheRunLines(t *testing.T) {
	t.Parallel()
	a := "tier: pro\nPATHS: x.go\nORDER: 1\nDEPENDS-ON: a-1\nthe body"
	b := "tier: heavy\nPATHS: x.go\nORDER: 2\nDEPENDS-ON: a-2\nthe body"
	assert.True(t, sameTask(a, b), "the run lines are not the task")
	c := "tier: pro\nPATHS: y.go\nthe body"
	assert.False(t, sameTask(a, c), "PATHS is part of the task")
}
