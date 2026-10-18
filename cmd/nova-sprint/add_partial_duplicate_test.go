package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// partialDuplicateBrief is a passing brief of one repository and one path: the
// id alone differs, so two of them are one change carded twice.
func partialDuplicateBrief(id string) string {
	return passingBrief("RESULT: " + id + " sha=0123456789ab tier: pro\nREPO: mas-bandwidth/nova-sprint\nPATHS: internal/x/*.go\n\nTHE TASK. Fix the empty case in the table reader, naming the column it fails on.")
}

// add --partial runs the probable-duplicate check add runs: a brief that
// duplicates an open card is held back with its DUPLICATE finding, and every
// card that needs it is held with it, while the clean brief of the wave is
// admitted in the one write, exit 1 (docs/CLI.md; docs/SPEC-SPRINT.md section
// 7, the add).
func TestAddPartialHoldsAProbableDuplicate(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	first := t.TempDir()
	open := writeBriefFile(t, first, "open", partialDuplicateBrief("open"))
	ta.ok("add --stream s1 --one --brief-file " + open)

	dir := t.TempDir()
	writeBriefFile(t, dir, "dup", partialDuplicateBrief("dup"))
	writeNeedsBrief(t, dir, "needsdup", "Fix needsdup.", "dup")
	writeNeedsBrief(t, dir, "clean", "Fix clean.", "")
	code, out, errs := ta.do("add --stream s1 --brief-dir " + dir + " --partial")
	require.Equal(t, 1, code, "exit %d\n%s%s", code, out, errs)
	require.Contains(t, out, "HELD dup: ", out)
	require.Contains(t, out, "probable duplicate of open", out)
	require.Contains(t, out, "HELD needsdup: needs dup, held back", out)
	require.Contains(t, out, "ADD OK stream=s1 cards=1 ", out)
	var w whereView
	ta.json("where", &w)
	require.Equal(t, int64(2), w.All, "the table holds the open card and the clean one: %s", out)
	ta.clean()
}

// The override add takes admits the duplicate in a --partial wave too: with the
// reason for the open card all three are admitted, exit 0, and the duplicate
// carries the reason on its admission note (docs/CLI.md; docs/SPEC-SPRINT.md
// section 7, the add).
func TestAddPartialTakesADuplicateWithItsOverride(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	first := t.TempDir()
	open := writeBriefFile(t, first, "open", partialDuplicateBrief("open"))
	ta.ok("add --stream s1 --one --brief-file " + open)

	dir := t.TempDir()
	writeBriefFile(t, dir, "dup", partialDuplicateBrief("dup"))
	writeNeedsBrief(t, dir, "needsdup", "Fix needsdup.", "dup")
	writeNeedsBrief(t, dir, "clean", "Fix clean.", "")
	code, out, errs := ta.do("add --stream s1 --brief-dir " + dir + " --partial --not-a-duplicate-of 'open=the second change stands alone'")
	require.Equal(t, 0, code, "exit %d\n%s%s", code, out, errs)
	require.NotContains(t, out, "HELD ", out)
	require.Contains(t, out, "ADD OK stream=s1 cards=3 ", out)
	require.Contains(t, ta.ok("card --fields dup"), "duplicate:", "the override reason is on the card's note")
	var w whereView
	ta.json("where", &w)
	require.Equal(t, int64(4), w.All, "the open card and all three of the wave: %s", out)
	ta.clean()
}
