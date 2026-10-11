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
	w.must(Brief(w.s, briefOf("b", "b reworded\nDEPENDS-ON: a-1", "a-1")))
	assert.Equal(t, "a-1,a-2", w.s.Work.Placed("b").F("needs"), "a text that drops a need keeps it")

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
