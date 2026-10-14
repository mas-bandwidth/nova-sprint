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

// An accepted brief edit keeps what a verb set since admission: the relinked
// need, the recorded tier (unless --tier names one), and the bound count of a
// routing-only edit; on the ordinary path and on the depends-only path (the
// text differs only in its DEPENDS-ON: line) alike.
func TestABriefEditKeepsWhatAVerbSet(t *testing.T) {
	t.Parallel()
	withC := func(t *testing.T) *world {
		w := relinkWorld(t)
		w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"c"}, Brief: "old c", Who: "coordinator"}))
		return w
	}
	// the coordinator pinned the tier with rework --tier or an earlier brief
	tierWorld := func(t *testing.T) *world {
		w := newWorld(t, "reader-a", "reader-b")
		w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "tier: pro\n\nthe task", Who: "coordinator"}))
		w.s.Work.Placed("a-1").Fields[FieldTier] = "frontier"
		return w
	}
	withTier := func(r BriefReq, tier string) BriefReq { r.Tier = tier; return r }
	for _, c := range []struct {
		name             string
		world            func(*testing.T) *world
		req              BriefReq
		field, want, msg string
		said             []string
	}{
		{"BriefKeepsANeedRelinkedToItsTwin", relinkWorld, briefOf("b", "tier: flash\nDEPENDS-ON: a-1\n\nthe task, reworded", "a-1"),
			"needs", "a-2", "the relink record wins over the text's stale DEPENDS-ON",
			[]string{"needs kept: a-2 (relinked from a-1 2030-01-02T03:04:05Z)"}},
		// the text names a new dependency c and still names the stale a-1
		{"BriefAddsANewNeedAndKeepsADroppedOne", withC, briefOf("b", "tier: flash\nDEPENDS-ON: a-1,c\n\nthe task, reworded", "a-1,c"),
			"needs", "a-2,c", "the relinked need stays a-2 and the new c is added", nil},
		// the body is the same, so Brief takes the depends-only path; the relink
		// record must still win over the text's stale DEPENDS-ON
		{"ABriefChangingOnlyDependsKeepsARelinkedNeed", withC, briefOf("b", "tier: flash\nDEPENDS-ON: a-1,c\n\nthe task", "a-1,c"),
			"needs", "a-2,c", "the relink record wins over the text's stale DEPENDS-ON on the depends-only path",
			[]string{"needs kept: a-2 (relinked from a-1 2030-01-02T03:04:05Z)"}},
		// only DEPENDS-ON changes (x -> x,y) and the recorded tier still disagrees
		// with line 1: the depends-only path says both, as the ordinary path does
		{"ADependsOnlyBriefReportsTheBoundAndKeepsTheTier", dependsOnlyBoundWorld, briefOf("a-1", "tier: pro\nDEPENDS-ON: x,y\n\nthe task", "x,y"),
			"", "", "", []string{"bound: same task (1 attempts stand)", "tier kept: frontier (the brief's line 1 says pro)"}},
		// --tier on the depends-only path writes over the recorded tier and line 1
		// (docs/SPEC-SPRINT.md, the tier)
		{"ADependsOnlyBriefTierFlagOverridesTheText", dependsOnlyBoundWorld, withTier(briefOf("a-1", "tier: pro\nDEPENDS-ON: x,y\n\nthe task", "x,y"), "heavy"),
			FieldTier, "heavy", "--tier names the tier on the depends-only path", nil},
		{"BriefKeepsARecordedTierAgainstTheText", tierWorld, briefOf("a-1", "tier: heavy\n\nthe task, reworded", ""),
			FieldTier, "frontier", "the recorded tier wins over the text's line 1", []string{"tier kept: frontier"}},
		{"BriefTierFlagOverridesTheText", tierWorld, withTier(briefOf("a-1", "tier: heavy\n\nthe task", ""), "heavy"),
			FieldTier, "heavy", "--tier names the tier over the recorded one and the text", nil},
		{"AMetadataOnlyBriefKeepsTheBoundCount", boundWorld, briefOf("a-1", "tier: heavy\nPATHS: internal/x.go\n\nthe task", ""),
			FieldBriefAttempt, "1", "a routing-only edit keeps the bound count", []string{"bound: same task (1 attempts stand)"}},
		{"ANewTaskBriefResetsTheBoundCount", boundWorld, briefOf("a-1", "tier: pro\nPATHS: internal/x.go\n\na different task", ""),
			FieldBriefAttempt, "2", "a new task resets the count", []string{"bound: new task (count reset)"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := c.world(t)
			p := Brief(w.s, c.req)
			require.Empty(t, p.Refused)
			w.must(p)
			if c.field != "" {
				assert.Equal(t, c.want, w.s.Work.Placed(c.req.Cards[0].ID).F(c.field), c.msg)
			}
			for _, said := range c.said {
				assert.Contains(t, strings.Join(p.Said, "\n"), said)
			}
		})
	}
}

func TestBriefTierRefusesATLAPathBelowHeavy(t *testing.T) {
	t.Parallel()
	// a card whose PATHS name TLA+ model work is the heavy tier's or above: a
	// re-tier to a tier below heavy is refused, as the lint's
	// tla-is-heavy-or-above rule refuses it at add (nova-sprint tier <card> --set <t> --reason).
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "a TLA model tier: frontier\nPATHS: tla/Foo.tla\n\nthe model", Who: "coordinator"}))
	p := Brief(w.s, BriefReq{ID: "a-1", Tier: "pro", Who: "coordinator"})
	require.Len(t, p.Refused, 1)
	assert.Empty(t, p.Units)
	assert.Contains(t, p.Refused[0].Why, "a tier below heavy is refused")

	// heavy and frontier are accepted
	for _, tier := range []string{"heavy", "frontier"} {
		p = Brief(w.s, BriefReq{ID: "a-1", Tier: tier, Who: "coordinator"})
		require.Empty(t, p.Refused, tier)
		w.must(p)
		assert.Equal(t, tier, w.s.Work.Placed("a-1").F(FieldTier), tier)
	}
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
	assert.Contains(t, says, "needs kept: a-1 not added (kept, ")
}

// --tier with a brief is held to the lint's tla-is-heavy-or-above rule as tier
// --set is: a brief whose PATHS covers tla/ is refused a tier below heavy, on
// the ordinary path and on the DEPENDS-ON-only path alike, and heavy passes.
func TestABriefTierFlagRefusesATLAPathBelowHeavy(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"x"}, Brief: "x", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"y"}, Brief: "y", Who: "coordinator"}))
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"m"}, Needs: []string{"x"}, Brief: "a model tier: frontier\nPATHS: tla/Foo.tla\nDEPENDS-ON: x\n\nthe model", Who: "coordinator"}))
	for _, c := range []struct{ name, text, needs string }{
		{"ordinary", "a model tier: frontier\nPATHS: tla/Foo.tla\nDEPENDS-ON: x\n\nthe model, reworded", "x"},
		{"depends-only", "a model tier: frontier\nPATHS: tla/Foo.tla\nDEPENDS-ON: x,y\n\nthe model", "x,y"},
	} {
		p := Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "m", Brief: c.text, Needs: Split(c.needs)}}, Tier: "pro", Who: "coordinator"})
		require.Len(t, p.Refused, 1, c.name)
		assert.Contains(t, p.Refused[0].Why, "below heavy", c.name)
		assert.Empty(t, p.Units, c.name)
	}

	// heavy itself is accepted
	p := Brief(w.s, BriefReq{Cards: []BriefCard{{ID: "m", Brief: "a model tier: frontier\nPATHS: tla/Foo.tla\nDEPENDS-ON: x\n\nthe model, reworded again", Needs: Split("x")}}, Tier: "heavy", Who: "coordinator"})
	require.Empty(t, p.Refused)
	w.must(p)
	assert.Equal(t, "heavy", w.s.Work.Placed("m").F(FieldTier))
}

// continue --tier is held to the lint's tla-is-heavy-or-above rule as a brief
// edit's --tier is: a tla/ card is refused a tier below heavy, and heavy passes.
func TestContinueTierFlagRefusesATLAPathBelowHeavy(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.must(Add(w.s, AddReq{Stream: "a", IDs: []string{"a-1"}, Brief: "a TLA model tier: frontier\nPATHS: tla/Foo.tla\n\nthe model", Who: "coordinator"}))
	const head = "1111111111111111111111111111111111111111"
	p := Continue(w.s, ContinueReq{ID: "a-1", Tier: "pro", Carry: head, Note: "x", Who: "coordinator"})
	require.Len(t, p.Refused, 1)
	assert.Empty(t, p.Units)
	assert.Contains(t, p.Refused[0].Why, "below heavy")

	p = Continue(w.s, ContinueReq{ID: "a-1", Tier: "heavy", Carry: head, Note: "x", Who: "coordinator"})
	require.Empty(t, p.Refused)
	w.must(p)
	assert.Equal(t, "heavy", w.s.Work.Placed("a-1").F(FieldTier))
}
