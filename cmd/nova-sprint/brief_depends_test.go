package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A brief that differs in its DEPENDS-ON: line alone is taken on a RUNNING
// machine (applied by the next tick, as every work-table change is while it
// runs) and for a card dealt (the comfort list of 2026-10-03, item 2:
// re-pointing six dependants after a drop meant stop, six briefs, start); the
// needs follow the rule, not the line (docs/SPEC-SPRINT.md, the needs): a need
// the text drops stays and a new name is added, so a-2 re-pointed from a-1 to
// a-3 keeps a-1 and gains a-3; a waiting card's whole brief is replaced while
// the machine runs, and a dealt card's whole brief is still refused; a need that
// is no primary on the table, the card itself, or one not landed for a ready
// card is refused.
func TestBriefDependsOnOnlyChangeIsTakenInAnyState(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	ta.ok("add --one --stream a a-1 --brief-file " + writeHeaderBrief(t, dir, "a-1", "-", "internal/a1.go"))
	ta.ok("add --one --stream a a-2 --brief-file " + writeHeaderBrief(t, dir, "a-2", "a-1 (uses: internal/a1.go)", "internal/a2.go"))
	ta.ok("add --one --stream a a-3 --brief-file " + writeHeaderBrief(t, dir, "a-3", "-", "internal/a3.go"))
	require.Equal(t, "a-1", ta.primary("a-2").F("needs"))

	for id, line := range map[string]string{
		"a-3 ready": "brief a-3 --brief-file " + writeHeaderBrief(t, t.TempDir(), "a-3", "a-2 (uses: internal/a2.go)", "internal/a3.go"),
		"itself":    "brief a-2 --brief-file " + writeHeaderBrief(t, t.TempDir(), "a-2", "a-2 (uses: internal/a2.go)", "internal/a2.go"),
	} {
		code, _, errs := ta.do(line)
		assert.Equal(t, 1, code, id)
		assert.Contains(t, errs, "nothing was changed", id)
	}
	// a need that is no card is refused before the step, as a finding of the DEPENDS-ON grammar
	code, _, errs := ta.do("brief a-2 --brief-file " + writeHeaderBrief(t, t.TempDir(), "a-2", "zz-9 (uses: internal/z9.go)", "internal/a2.go"))
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "check=depends-unknown")
	assert.Equal(t, "a-1", ta.primary("a-2").F("needs"))
	assert.Equal(t, "", ta.primary("a-3").F("needs"))

	// the machine runs: the DEPENDS-ON change alone is taken, and the needs rule
	// holds on it: a-2 keeps a-1 the text dropped and adds a-3 it names
	ta.ok("start")
	out := ta.ok("brief a-2 --brief-file " + writeHeaderBrief(t, t.TempDir(), "a-2", "a-3 (uses: internal/a3.go)", "internal/a2.go"))
	assert.Contains(t, out, "MOVED a-2 DEPENDS-ON replaced: needs a-1,a-3 ")
	ta.ok("tick")
	assert.Equal(t, "a-1,a-3", ta.primary("a-2").F("needs"))
	assert.Contains(t, ta.primary("a-2").F("brief"), "DEPENDS-ON: a-3 (uses: internal/a3.go)\n")
	assert.Equal(t, "waiting", ta.primary("a-2").Col)

	// a waiting card takes a whole new brief while the machine runs; the needs stay
	out = ta.ok("brief a-2 --brief-file " + writeHeaderBrief(t, t.TempDir(), "a-2", "a-3 (uses: internal/a3.go)", "internal/other.go"))
	assert.Contains(t, out, "a-2 brief replaced")
	ta.ok("tick")
	assert.Contains(t, ta.primary("a-2").F("brief"), "PATHS: internal/other.go\n")
	assert.Equal(t, "a-1,a-3", ta.primary("a-2").F("needs"))
	assert.Equal(t, "waiting", ta.primary("a-2").Col)

	// a card dealt: the needs follow, and a dealt card whose needs have not landed goes back
	// to the pool (docs/SPEC-SPRINT.md, needs and the deal; needsWithdrawals): its primary
	// goes working -> waiting in that tick, the lifecycle's move for needs not landed
	require.Equal(t, "working", ta.primary("a-1").Col, "the tick dealt a-1")
	out = ta.ok("brief a-1 --replace-in-flight --brief-file " + writeHeaderBrief(t, t.TempDir(), "a-1", "a-3 (uses: internal/a3.go)", "internal/a1.go"))
	assert.Contains(t, out, "a-1 brief edited in place")
	ta.ok("tick")
	assert.Equal(t, "a-3", ta.primary("a-1").F("needs"))
	assert.Contains(t, ta.primary("a-1").F("brief"), "DEPENDS-ON: a-3 (uses: internal/a3.go)\n")
	assert.Equal(t, "waiting", ta.primary("a-1").Col, "withdrawn: it waits for a-3, which has not landed")
	out = ta.ok("brief a-1 --brief-file " + writeHeaderBrief(t, t.TempDir(), "a-1", "a-3 (uses: internal/a3.go)", "internal/other.go"))
	assert.Contains(t, out, "a-1 brief edited in place", "an attempt was dealt: its next attempt takes the brief")
}
