package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

func writeSoftHeaderBrief(t *testing.T, dir, id, depends, landAfter, repo, base, paths string) string {
	t.Helper()
	lead := "RESULT: " + id + " sha=000000000000 tier: flash\nKIND: fix\n"
	if repo != "" {
		lead += "REPO: " + repo + "\n"
	}
	if base != "" {
		lead += "BASE: " + base + "\n"
	}
	if depends != "" {
		lead += "DEPENDS-ON: " + depends + "\n"
	}
	if landAfter != "" {
		lead += "LAND-AFTER: " + landAfter + "\n"
	}
	lead += "PATHS: " + paths + "\n\nFix " + id + "."
	path := filepath.Join(dir, id+".md")
	require.NoError(t, os.WriteFile(path, []byte(passingBrief(lead)), 0o600))
	return path
}

// TestNeedsSetLandAfter pins `nova-sprint needs set <id> --land-after <a,b>`:
// sets the soft dependency without moving the card to waiting; `--land-after` with an empty value
// clears it; self, duplicate, off-table target, a target already in needs, a
// target in another repository or base, or a cycle through soft dependencies and
// needs together is refused with exit 2 and nothing written; `--land-after` with
// `--needs` in one call is refused.
func TestNeedsSetLandAfter(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	a := writeSoftHeaderBrief(t, dir, "a", "-", "", "mas-bandwidth/nova-sprint", "", "internal/a.go")
	b := writeSoftHeaderBrief(t, dir, "b", "-", "", "mas-bandwidth/nova-sprint", "", "internal/b.go")
	c := writeSoftHeaderBrief(t, dir, "c", "-", "", "mas-bandwidth/nova-sprint", "", "internal/c.go")
	n1 := writeSoftHeaderBrief(t, dir, "n1", "b", "", "mas-bandwidth/nova-sprint", "", "internal/n1.go")
	otherRepo := writeSoftHeaderBrief(t, dir, "other-repo", "-", "", "mas-bandwidth/nova-tools", "", "internal/o.go")
	otherBase := writeSoftHeaderBrief(t, dir, "other-base", "-", "", "", "dev", "internal/ob.go")
	dropped := writeSoftHeaderBrief(t, dir, "dropped", "-", "", "mas-bandwidth/nova-sprint", "", "internal/d.go")

	for _, f := range []string{a, b, c, n1, otherRepo, otherBase, dropped} {
		ta.ok("add --stream s1 --one --brief-file " + f)
	}
	ta.ok("drop dropped --reason obsolete")

	// Set soft dependency on b -> a,c; b stays ready and card b shows soft: a,c.
	out := ta.ok("needs set b --land-after a,c")
	assert.Contains(t, out, "b")
	assert.Equal(t, string(sprint.Ready), ta.primary("b").Col, "setting --land-after keeps b ready")
	assert.Equal(t, "a,c", ta.primary("b").F("land_after"))
	assert.Contains(t, ta.ok("card b"), "\nsoft: a,c\n")

	// Clear with --land-after ''.
	ta.ok("needs set b --land-after ''")
	assert.Equal(t, "", ta.primary("b").F("land_after"))
	assert.NotContains(t, ta.ok("card b"), "\nsoft:")

	// Put a back on b so we can verify refusals leave b's land_after unchanged.
	ta.ok("needs set b --land-after a")

	// --land-after with --needs or --clear in one call is refused (exit 2).
	code, _, errs := ta.do("needs set b --needs c --land-after a")
	assert.Equal(t, 2, code, errs)
	code, _, errs = ta.do("needs set b --clear --land-after a")
	assert.Equal(t, 2, code, errs)

	// Self, duplicate, absent, dropped, already in needs, another repo, another base, and cycles are refused with exit 2 and nothing written.
	ta.ok("needs set c --needs a")
	for _, tc := range []struct {
		name, cmd, want string
	}{
		{"self", "needs set b --land-after b", "itself"},
		{"duplicate", "needs set b --land-after a,a", "twice"},
		{"absent", "needs set b --land-after ghost", "not on the table"},
		{"dropped", "needs set b --land-after dropped", "not on the table"},
		{"already in needs", "needs set c --land-after a", "one relation, not two"},
		{"other repo", "needs set b --land-after other-repo", "the order would mean nothing"},
		{"other base", "needs set b --land-after other-base", "the order would mean nothing"},
		{"soft cycle", "needs set a --land-after b", "cycle"},
		{"mixed soft and need cycle", "needs set b --land-after n1", "cycle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := ta.applies()
			code, out, errs := ta.do(tc.cmd)
			require.Equal(t, 2, code, "%s: %s%s", tc.name, out, errs)
			assert.Contains(t, out+errs, tc.want, "%s: %s%s", tc.name, out, errs)
			assert.Equal(t, before, ta.applies(), "%s wrote to the store", tc.name)
		})
	}
	assert.Equal(t, "a", ta.primary("b").F("land_after"))
}

// TestAddReadsLandAfter pins that `nova-sprint add` of a brief carrying
// `LAND-AFTER: <a,b>` records the soft dependency without placing the card in
// waiting, `nova-sprint card <id>` shows `soft: <a,b>` on its own line, and a
// stranded `LAND-AFTER:` line in the body or an invalid `LAND-AFTER:` target is
// refused with exit 2 and nothing written.
func TestAddReadsLandAfter(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	a := writeSoftHeaderBrief(t, dir, "a", "-", "", "mas-bandwidth/nova-sprint", "", "internal/a.go")
	b := writeSoftHeaderBrief(t, dir, "b", "-", "", "mas-bandwidth/nova-sprint", "", "internal/b.go")
	c := writeSoftHeaderBrief(t, dir, "c", "-", "a, b", "mas-bandwidth/nova-sprint", "", "internal/c.go")

	ta.ok("add --stream s1 --brief-file " + a + " --brief-file " + b + " --brief-file " + c)
	assert.Equal(t, string(sprint.Ready), ta.primary("c").Col, "LAND-AFTER does not place c in waiting")
	assert.Equal(t, "a,b", ta.primary("c").F("land_after"))
	assert.Contains(t, ta.ok("card c"), "\nsoft: a,b\n")

	// A stranded LAND-AFTER: line in the body (outside the header block) is refused by the card lint (exit 2).
	stranded := filepath.Join(dir, "stranded.md")
	require.NoError(t, os.WriteFile(stranded, []byte(passingBrief("RESULT: stranded sha=000000000000 tier: flash\nKIND: fix\nPATHS: internal/s.go\n\nFix stranded.\nLAND-AFTER: a")), 0o600))
	code, out, errs := ta.do("add --stream s1 stranded --one --brief-file " + stranded)
	require.Equal(t, 2, code, "%s%s", out, errs)
	assert.Contains(t, out+errs, "LAND-AFTER: on line")
	assert.Contains(t, out+errs, "below the header block")
	assert.False(t, ta.placed("stranded"))

	// Self-edge and duplicate-of-hard-need in LAND-AFTER: are refused at add time with exit 2.
	selfBrief := writeSoftHeaderBrief(t, dir, "self-card", "-", "self-card", "mas-bandwidth/nova-sprint", "", "internal/sc.go")
	code, out, errs = ta.do("add --stream s1 self-card --one --brief-file " + selfBrief)
	require.Equal(t, 2, code, "%s%s", out, errs)
	assert.Contains(t, out+errs, "itself")
	assert.False(t, ta.placed("self-card"))
}

// TestAddAdmitsAnOverlapWithASoftDependency pins that two briefs in a plan that
// both name the same non-ledger file in PATHS are admitted when one names the
// other in `LAND-AFTER:`, and refused when neither has a soft dependency or a
// hard need on the other, with the refusal naming both options (`LAND-AFTER: <id>`
// and `DEPENDS-ON: <id>`).
func TestAddAdmitsAnOverlapWithASoftDependency(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")

	unlinked := t.TempDir()
	writeSoftHeaderBrief(t, unlinked, "p1", "-", "", "mas-bandwidth/nova-sprint", "", "internal/shared.go, internal/p1.go")
	writeSoftHeaderBrief(t, unlinked, "p2", "-", "", "mas-bandwidth/nova-sprint", "", "internal/shared.go, internal/p2.go")
	code, out, errs := ta.do("add --stream s1 --brief-dir " + unlinked)
	require.Equal(t, 2, code, "%s%s", out, errs)
	assert.Contains(t, errs, "internal/shared.go is named in PATHS by p1 and p2")
	assert.Contains(t, errs, "LAND-AFTER: <id>")
	assert.Contains(t, errs, "DEPENDS-ON: <id>")
	assert.False(t, ta.placed("p1") || ta.placed("p2"), "nothing written")

	softLinked := t.TempDir()
	writeSoftHeaderBrief(t, softLinked, "s1a", "-", "", "mas-bandwidth/nova-sprint", "", "internal/shared.go, internal/s1a.go")
	writeSoftHeaderBrief(t, softLinked, "s1b", "-", "s1a", "mas-bandwidth/nova-sprint", "", "internal/shared.go, internal/s1b.go")
	planOut := ta.ok("add --plan " + softLinked)
	assert.Contains(t, planOut, "internal/shared.go  s1a,s1b  not-ledger  soft\n")

	addOut := ta.ok("add --stream s1 --brief-dir " + softLinked)
	assert.Contains(t, addOut, "ADD OK stream=s1 cards=2")
	assert.Equal(t, string(sprint.Ready), ta.primary("s1a").Col)
	assert.Equal(t, string(sprint.Ready), ta.primary("s1b").Col)
	assert.Equal(t, "s1a", ta.primary("s1b").F("land_after"))
}
