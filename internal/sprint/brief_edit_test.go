package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A brief edit replaces a card's whole text, but the text dumped from the card
// is the admission-time text: its DEPENDS-ON and line 1 tier are the ones the
// card was admitted with, not the ones a verb set since. Three rules keep what
// the verb set (docs/SPEC-SPRINT.md, the brief, the needs and the bound): the
// needs a relink re-pointed, the tier a retier pinned, and the bound count a
// routing-only edit must not reset.

// relinkWorld is the twin store: b waits on a-1, which a-2 replaced.
func relinkWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "old one", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-2"}, Brief: "old two", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"b"}, Needs: []string{"a-1"}, Brief: "tier: flash\nDEPENDS-ON: a-1\n\nthe task", Who: "coordinator"}))
	w.must(Relink(w.s, RelinkReq{Old: []string{"a-1"}, New: "a-2", Who: "coordinator"}))
	require.Equal(t, "a-2", w.s.Work.Placed("b").F("needs"))
	require.NotEmpty(t, w.s.Work.Placed("b").F(FieldRelinked))
	return w
}

// briefOf is a brief request replacing one card's brief, as the verb passes it:
// its needs are the text's DEPENDS-ON list.
func briefOf(id, brief, depends string) BriefReq {
	return BriefReq{Cards: []BriefCard{{ID: id, Brief: brief, Needs: Split(depends)}}, Who: "coordinator"}
}

func TestBriefKeepsANeedRelinkedToItsTwin(t *testing.T) {
	t.Parallel()
	w := relinkWorld(t)
	p := Brief(w.s, briefOf("b", "tier: flash\nDEPENDS-ON: a-1\n\nthe task, reworded", "a-1"))
	require.Empty(t, p.Refused)
	w.must(p)
	assert.Equal(t, "a-2", w.s.Work.Placed("b").F("needs"), "the relink record wins over the text's stale DEPENDS-ON")
	assert.Contains(t, strings.Join(p.Said, "\n"), "needs kept: a-2 (relinked from a-1 2030-01-02T03:04:05Z)")
}

func TestBriefAddsANewNeedAndKeepsADroppedOne(t *testing.T) {
	t.Parallel()
	w := relinkWorld(t)
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"c"}, Brief: "old c", Who: "coordinator"}))
	// the text names a new dependency c and still names the stale a-1
	p := Brief(w.s, briefOf("b", "tier: flash\nDEPENDS-ON: a-1,c\n\nthe task, reworded", "a-1,c"))
	require.Empty(t, p.Refused)
	w.must(p)
	assert.Equal(t, "a-2,c", w.s.Work.Placed("b").F("needs"), "the relinked need stays a-2 and the new c is added")
}

func TestABriefDroppingANeedKeepsItUnlessNeedsFromBrief(t *testing.T) {
	t.Parallel()
	// a text that drops a need keeps it by default
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "one", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-2"}, Brief: "two", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"b"}, Needs: []string{"a-1", "a-2"}, Brief: "b needs both", Who: "coordinator"}))
	kept := w.must(Brief(w.s, briefOf("b", "b reworded\nDEPENDS-ON: a-1", "a-1")))
	assert.Equal(t, "a-1,a-2", w.s.Work.Placed("b").F("needs"), "a text that drops a need keeps it")
	assert.Contains(t, strings.Join(kept.Said, "\n"), "needs kept: a-2 (the text's DEPENDS-ON drops it", "the NOTE names every property kept against the text")

	// --needs-from-brief takes the text whole, the old behaviour
	w = relinkWorld(t)
	p := Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "b", Brief: "tier: flash\nDEPENDS-ON: a-1\n\nthe task", Needs: []string{"a-1"}}}, NeedsFromBrief: true, Who: "coordinator"})
	require.Empty(t, p.Refused)
	w.must(p)
	assert.Equal(t, "a-1", w.s.Work.Placed("b").F("needs"), "--needs-from-brief takes the text's stale list")
}

func TestABriefChangingOnlyDependsKeepsARelinkedNeed(t *testing.T) {
	t.Parallel()
	w := relinkWorld(t)
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"c"}, Brief: "old c", Who: "coordinator"}))
	// the brief differs from the card's only in its DEPENDS-ON: line (the body is
	// the same), so Brief takes the depends-only path; the relink record must still
	// win over the text's stale DEPENDS-ON, as it does on the ordinary path.
	p := Brief(w.s, briefOf("b", "tier: flash\nDEPENDS-ON: a-1,c\n\nthe task", "a-1,c"))
	require.Empty(t, p.Refused)
	w.must(p)
	assert.Equal(t, "a-2,c", w.s.Work.Placed("b").F("needs"), "the relink record wins over the text's stale DEPENDS-ON on the depends-only path")
	assert.Contains(t, strings.Join(p.Said, "\n"), "needs kept: a-2 (relinked from a-1 2030-01-02T03:04:05Z)")
}

// dependsOnlyBoundWorld is a card dealt at attempt 2 whose brief last changed at
// attempt 1, with a recorded tier and a DEPENDS-ON line: a DEPENDS-ON-only edit
// is the depends-only path, which must report the bound and the kept tier as the
// ordinary path does (docs/SPEC-SPRINT.md, the brief).
func dependsOnlyBoundWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"x"}, Brief: "x", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"y"}, Brief: "y", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Needs: []string{"x"}, Brief: "tier: pro\nDEPENDS-ON: x\n\nthe task", Who: "coordinator"}))
	w.place(w.s.Work, "a-1", "a", Working)
	w.s.Work.Placed("a-1").Fields["attempt"] = "2"
	w.s.Work.Placed("a-1").Fields[FieldBriefAttempt] = "1"
	w.s.Work.Placed("a-1").Fields[FieldTier] = "frontier"
	return w
}

func TestADependsOnlyBriefReportsTheBoundAndKeepsTheTier(t *testing.T) {
	t.Parallel()
	w := dependsOnlyBoundWorld(t)
	// only the DEPENDS-ON: line changes (x -> x,y), so the task is the same and
	// the recorded tier still disagrees with the text's line 1: the depends-only
	// path must say both, as the ordinary path does.
	p := Brief(w.s, briefOf("a-1", "tier: pro\nDEPENDS-ON: x,y\n\nthe task", "x,y"))
	require.Empty(t, p.Refused)
	w.must(p)
	says := strings.Join(p.Said, "\n")
	assert.Contains(t, says, "bound: same task (1 attempts stand)")
	assert.Contains(t, says, "tier kept: frontier (the brief's line 1 says pro)")
}

func TestADependsOnlyBriefTierFlagOverridesTheText(t *testing.T) {
	t.Parallel()
	w := dependsOnlyBoundWorld(t)
	// only the DEPENDS-ON: line changes (x -> x,y), so the depends-only path is
	// taken, but --tier names a new tier: the depends-only path must write it over
	// the recorded tier and the text's line 1, as the ordinary path does
	// (docs/SPEC-SPRINT.md, the tier).
	p := Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-1", Brief: "tier: pro\nDEPENDS-ON: x,y\n\nthe task", Needs: []string{"x", "y"}}}, Tier: "heavy", Who: "coordinator"})
	require.Empty(t, p.Refused)
	w.must(p)
	assert.Equal(t, "heavy", w.s.Work.Placed("a-1").F(FieldTier), "--tier names the tier on the depends-only path")
}

func TestBriefKeepsARecordedTierAgainstTheText(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "tier: pro\n\nthe task", Who: "coordinator"}))
	// the coordinator pinned the tier with rework --tier or an earlier brief
	w.s.Work.Placed("a-1").Fields[FieldTier] = "frontier"
	p := Brief(w.s, briefOf("a-1", "tier: heavy\n\nthe task, reworded", ""))
	require.Empty(t, p.Refused)
	w.must(p)
	assert.Equal(t, "frontier", w.s.Work.Placed("a-1").F(FieldTier), "the recorded tier wins over the text's line 1")
	assert.Contains(t, strings.Join(p.Said, "\n"), "tier kept: frontier")
}

func TestBriefTierFlagOverridesTheText(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "tier: pro\n\nthe task", Who: "coordinator"}))
	w.s.Work.Placed("a-1").Fields[FieldTier] = "frontier"
	p := Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "a-1", Brief: "tier: heavy\n\nthe task", Needs: nil}}, Tier: "heavy", Who: "coordinator"})
	require.Empty(t, p.Refused)
	w.must(p)
	assert.Equal(t, "heavy", w.s.Work.Placed("a-1").F(FieldTier), "--tier names the tier over the recorded one and the text")
}

// boundWorld is a card dealt at attempt 2 whose brief last changed at attempt 1.
func boundWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "tier: pro\nPATHS: internal/x.go\n\nthe task", Who: "coordinator"}))
	w.place(w.s.Work, "a-1", "a", Review)
	w.s.Work.Placed("a-1").Fields["attempt"] = "2"
	w.s.Work.Placed("a-1").Fields[FieldBriefAttempt] = "1"
	return w
}

func TestAMetadataOnlyBriefKeepsTheBoundCount(t *testing.T) {
	t.Parallel()
	w := boundWorld(t)
	p := Brief(w.s, briefOf("a-1", "tier: heavy\nPATHS: internal/x.go\n\nthe task", ""))
	require.Empty(t, p.Refused)
	w.must(p)
	c := w.s.Work.Placed("a-1")
	assert.Equal(t, "1", c.F(FieldBriefAttempt), "a routing-only edit keeps the bound count")
	assert.Contains(t, strings.Join(p.Said, "\n"), "bound: same task (1 attempts stand)")
}

func TestBriefTierRefusesATLAPathBelowFrontier(t *testing.T) {
	t.Parallel()
	// a card whose PATHS name TLA+ model work is the frontier tier's: a re-tier
	// to a tier below frontier is refused, as the lint's tla-is-frontier rule
	// refuses it at add (nova-sprint tier <card> --set <t> --reason).
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "a TLA model tier: frontier\nPATHS: tla/Foo.tla\n\nthe model", Who: "coordinator"}))
	p := Brief(w.s, BriefReq{ID: "a-1", Tier: "heavy", Who: "coordinator"})
	require.Len(t, p.Refused, 1)
	assert.Empty(t, p.Units)
	assert.Contains(t, p.Refused[0].Why, "a tier below frontier is refused")

	// frontier itself is accepted
	p = Brief(w.s, BriefReq{ID: "a-1", Tier: "frontier", Who: "coordinator"})
	require.Empty(t, p.Refused)
	w.must(p)
	assert.Equal(t, "frontier", w.s.Work.Placed("a-1").F(FieldTier))
}

func TestANewTaskBriefResetsTheBoundCount(t *testing.T) {
	t.Parallel()
	w := boundWorld(t)
	p := Brief(w.s, briefOf("a-1", "tier: pro\nPATHS: internal/x.go\n\na different task", ""))
	require.Empty(t, p.Refused)
	w.must(p)
	c := w.s.Work.Placed("a-1")
	assert.Equal(t, "2", c.F(FieldBriefAttempt), "a new task resets the count")
	assert.Contains(t, strings.Join(p.Said, "\n"), "bound: new task (count reset)")
}

// The needs a batch of edits writes are the ones held to the cycle check, never
// the texts' lists: b's text still names a-1 (stale), which the relink record
// keeps as a-2, and a second edit in the same batch makes a-2 need b. The text's
// list (b -> a-1) closes no cycle; the needs the edit writes (b -> a-2) do, so
// the batch is refused (docs/SPEC-SPRINT.md, the needs; needsCycleWith).
func TestABriefBatchHoldsTheKeptNeedsToTheCycleCheck(t *testing.T) {
	t.Parallel()
	w := relinkWorld(t)
	p := Brief(w.s, BriefReq{Cards: []BriefCard{
		{ID: "b", Brief: "tier: flash\nDEPENDS-ON: a-1\n\nthe task, reworded", Needs: []string{"a-1"}},
		{ID: "a-2", Brief: "old two, reworded\nDEPENDS-ON: b", Needs: []string{"b"}},
	}, Who: "coordinator"})
	require.NotEmpty(t, p.Refused, "b -> a-2 -> b is a cycle: the kept twin is held to the check")
	var whys []string
	for _, r := range p.Refused {
		whys = append(whys, r.Why)
	}
	assert.Contains(t, strings.Join(whys, "\n"), "cycle")
}

// The relink record keeps only the last relink, so a card relinked twice has
// lost the record of the first: a text still naming that first old id, a card
// since dropped, must not add it back (it would block the card on something
// dropped, the incident of 2026-10-11), and the brief says it left it out.
func TestABriefLeavesOutADroppedNeedAnEarlierRelinkReplaced(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	for _, id := range []string{"a-1", "a-2", "x-1", "x-2"} {
		w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{id}, Brief: "card " + id, Who: "coordinator"}))
	}
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"b"}, Needs: []string{"a-1", "x-1"}, Brief: "tier: flash\nDEPENDS-ON: a-1,x-1\n\nthe task", Who: "coordinator"}))
	w.must(Relink(w.s, RelinkReq{Old: []string{"a-1"}, New: "a-2", Who: "coordinator"}))
	w.must(Relink(w.s, RelinkReq{Old: []string{"x-1"}, New: "x-2", Who: "coordinator"}))
	w.must(Drop(w.s, DropReq{Sel: Sel{IDs: []string{"a-1", "x-1"}}, Reason: "replaced", Who: "coordinator"}))
	require.Equal(t, "a-2,x-2", w.s.Work.Placed("b").F("needs"))

	p := Brief(w.s, briefOf("b", "tier: flash\nDEPENDS-ON: a-1,x-1\n\nthe task, reworded", "a-1,x-1"))
	require.Empty(t, p.Refused)
	w.must(p)
	assert.Equal(t, "a-2,x-2", w.s.Work.Placed("b").F("needs"), "the dropped a-1 an earlier relink replaced is not added back")
	says := strings.Join(p.Said, "\n")
	assert.Contains(t, says, "needs kept: x-2 (relinked from x-1 ")
	assert.Contains(t, says, "need a-1 is now a-2 (relinked ", "the alias a-1 => a-2 outlives the last relink record: the text's old id resolves to its successor, not left as a dropped need")
}

// --tier with a brief is held to the lint's tla-is-frontier rule as tier --set
// is: a brief whose PATHS covers tla/ is refused a tier below frontier, on the
// ordinary path and on the DEPENDS-ON-only path alike.
func TestABriefTierFlagRefusesATLAPathBelowFrontier(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"x"}, Brief: "x", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"y"}, Brief: "y", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"m"}, Needs: []string{"x"}, Brief: "a model tier: frontier\nPATHS: tla/Foo.tla\nDEPENDS-ON: x\n\nthe model", Who: "coordinator"}))
	for _, c := range []struct{ name, text, needs string }{
		{"ordinary", "a model tier: frontier\nPATHS: tla/Foo.tla\nDEPENDS-ON: x\n\nthe model, reworded", "x"},
		{"depends-only", "a model tier: frontier\nPATHS: tla/Foo.tla\nDEPENDS-ON: x,y\n\nthe model", "x,y"},
	} {
		p := Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "m", Brief: c.text, Needs: Split(c.needs)}}, Tier: "heavy", Who: "coordinator"})
		require.Len(t, p.Refused, 1, c.name)
		assert.Contains(t, p.Refused[0].Why, "below frontier", c.name)
		assert.Empty(t, p.Units, c.name)
	}
}
