package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// add holds every brief to the card checks nova-card generate holds it to before it
// leaves (card.Checks): a card brief with no tier on line 1, a TEST outside its
// PATHS, the name of the sprint's coordinator or owner outside double-quoted words, or a
// card dropped off the table named, is refused with the finding and nothing is written;
// the clean brief, and the owner quoted by name, are admitted.
func TestAddHoldsABriefToTheCardChecks(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead --owner ada")
	ta.ok("add --stream s0 --count 2 --actor lead")
	ta.ok("drop s0-1 --reason obsolete --actor lead")
	header := "RESULT: c sha=0123456789ab tier: pro\nPATHS: internal/x/*.go, internal/x/*_test.go\nTEST: internal/x TestY\n"
	write := func(name, text string) string {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, name+".md")
		require.NoError(t, os.WriteFile(path, []byte(passingBrief(text)), 0o600))
		return path
	}
	for _, tc := range []struct{ name, brief, check string }{
		{"no-tier", "RESULT: c sha=0123456789ab\nPATHS: internal/x/*.go\nTEST: internal/x TestY\n\nTHE TASK. Fix x.", "check=tier-line line=1"},
		{"test-out", "RESULT: c sha=0123456789ab tier: pro\nPATHS: internal/x/*.go\nTEST: internal/other TestY\n\nTHE TASK. Fix x.", "check=test-outside-paths line=3"},
		{"named", header + "\nTHE TASK. Fix x as ada asked.", "check=personal-name line=5"},
		{"dropped", header + "\nTHE TASK. Finish what s0-1 began.", "check=dropped-card line=5"},
	} {
		code, out, stderr := ta.do("add --stream s1 --one --actor lead --brief-file " + write(tc.name, tc.brief))
		assert.Equal(t, 2, code, "%s: %s%s", tc.name, out, stderr)
		assert.Contains(t, stderr, "LINT DRIFT card="+tc.name+" "+tc.check, tc.name)
		assert.Contains(t, stderr, "nova-sprint add REFUSED: 1 brief finding(s)", tc.name)
		assert.False(t, ta.placed(tc.name), "%s: nothing written", tc.name)
	}
	out := ta.ok("add --stream s1 --one --actor lead --brief-file " + write("clean", header+"\nTHE TASK. The owner said \"ada wants x fixed\"; fix x."))
	assert.Contains(t, out, "MOVED clean -> ready")
	out = ta.ok("add --stream s1 --one --actor lead --brief-file " + write("path-exempt", header+"\nTHE TASK. See /home/ada/projects/.\n"))
	assert.Contains(t, out, "MOVED path-exempt -> ready")
}

// brief runs the same dropped-card lint as add (one lint for both, holdCardChecks):
// the same text that names a dropped card is refused by add and by brief, each with the
// same finding, and neither writes (docs/SPEC-SPRINT.md, the brief verb and the alias).
func TestBriefRunsTheSameDroppedCardLintAsAdd(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead --owner ada")
	ta.ok("add --stream s0 --count 2 --actor lead")
	ta.ok("add --stream s1 --one --actor lead --brief-file " + writeHeaderBrief(t, t.TempDir(), "tgt", "-", "internal/tgt.go"))
	ta.ok("drop s0-1 --reason obsolete --actor lead")
	header := "RESULT: c sha=0123456789ab tier: pro\nPATHS: internal/x/*.go, internal/x/*_test.go\nTEST: internal/x TestY\n"
	text := header + "\nTHE TASK. Finish what s0-1 began."
	write := func(name string) string {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, name+".md")
		require.NoError(t, os.WriteFile(path, []byte(passingBrief(text)), 0o600))
		return path
	}
	code, _, stderr := ta.do("add --stream s1 --one --actor lead --brief-file " + write("add"))
	assert.Equal(t, 2, code, "add refuses: %s", stderr)
	assert.Contains(t, stderr, "check=dropped-card line=5", "add: %s", stderr)
	code, out, stderr := ta.do("brief tgt --actor lead --brief-file " + write("brief"))
	assert.Equal(t, 2, code, "brief refuses: %s%s", out, stderr)
	assert.Contains(t, stderr, "check=dropped-card line=5", "brief: %s", stderr)
	assert.NotContains(t, out, "MOVED", "brief wrote nothing")
	assert.NotContains(t, ta.primary("tgt").F("brief"), "s0-1", "the brief was not replaced")
}

// A dropped card with a successor is an alias for the dropped-card lint too (item
// 1, item 2; docs/SPEC-SPRINT.md, the alias): an add or a brief whose text names
// the old hyphenated id is admitted, stores the successor, and NOTEs it. Only a
// dropped card with no successor is refused (TestBriefRunsTheSameDroppedCardLintAsAdd).
func TestAddAndBriefAdmitAnAliasedDroppedId(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead --owner ada")
	ta.ok("add --stream s0 --count 2 --actor lead") // s0-1, s0-2
	ta.ok("add --stream s1 --one s0-succ --actor lead --brief-file " + writeHeaderBrief(t, t.TempDir(), "s0-succ", "-", "internal/succ.go"))
	// drop s0-1 replaced by its successor: s0-1 becomes an alias of s0-succ
	ta.ok("drop s0-1 --replaced-by s0-succ --reason recut --actor lead")

	out := ta.ok("add --stream s1 --one --actor lead --brief-file " + writeHeaderBrief(t, t.TempDir(), "dep", "s0-1", "internal/dep.go"))
	assert.Contains(t, out, "need s0-1 is now s0-succ", "add NOTEs the alias: %s", out)
	assert.Equal(t, "s0-succ", ta.primary("dep").F("needs"), "add stores the successor, never the old id")

	out = ta.ok("brief s0-2 --actor lead --brief-file " + writeHeaderBrief(t, t.TempDir(), "s0-2", "s0-1", "internal/tgt.go"))
	assert.Contains(t, out, "need s0-1 is now s0-succ", "brief NOTEs the alias: %s", out)
	assert.Equal(t, "s0-succ", ta.primary("s0-2").F("needs"), "brief stores the successor, never the old id")
}
