package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// writeHeaderBrief writes a passing brief whose header block carries DEPENDS-ON: and
// PATHS: lines, as a card the coordinator writes does, and returns its path.
func writeHeaderBrief(t *testing.T, dir, id, depends, paths string) string {
	t.Helper()
	lead := "RESULT: " + id + " sha=000000000000 tier: flash\nKIND: fix\nDEPENDS-ON: " + depends + "\nPATHS: " + paths + "\n\nFix " + id + "."
	path := filepath.Join(dir, id+".md")
	require.NoError(t, os.WriteFile(path, []byte(passingBrief(lead)), 0o600))
	return path
}

// add reads a brief's DEPENDS-ON: header line as its needs when --needs is not given
// (nova-tools#5096 item 17, the wave-2 card builder: "add neither reads DEPENDS-ON from
// the brief nor sees two open cards naming the same file; the chains were computed
// outside"): `-` and an owner/repo#n reference are no need, --needs given wins, and the
// many-brief form reads it for each card.
func TestAddReadsDependsOnFromTheBrief(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	c0 := writeHeaderBrief(t, dir, "c0", "mas-bandwidth/nova-tools#5096", "internal/c0.go")
	c1 := writeHeaderBrief(t, dir, "c1", "c0", "internal/c1.go")
	assert.Contains(t, ta.ok("add --stream s c0 --one --brief-file "+c0), "MOVED c0 -> plan")
	assert.Contains(t, ta.ok("add --stream s c1 --one --brief-file "+c1), "MOVED c1 -> plan")
	assert.Equal(t, "c0", ta.primary("c1").F("needs"), "DEPENDS-ON: c0 is c1's need")
	ta.ok("add --stream s c2 --one --brief-file " + c1 + " --needs c1")
	assert.Equal(t, "c1", ta.primary("c2").F("needs"), "--needs given wins over the brief's line")
	many := t.TempDir()
	writeHeaderBrief(t, many, "p1", "-", "internal/p1.go")
	writeHeaderBrief(t, many, "p2", "p1", "internal/p2.go")
	ta.ok("add --stream t --brief-dir " + many)
	assert.Equal(t, "p1", ta.primary("p2").F("needs"), "the many-brief form reads DEPENDS-ON: per card")
	assert.Equal(t, string(sprint.Planned), ta.primary("p1").Col)
}

// Two cards of one add that name the same file in PATHS, no lander-owned ledger
// covers it, and neither needs the other, would edit it at once: add refuses
// them, exit 2, nothing written, naming the file and the cards. There is no
// --allow-shared-paths that admits them; a ledger the lander merges, or a needs
// edge, does (docs/SPEC-CARD-CONTRACT.md, the shared-path rule).
func TestAddRefusesTwoCardsNamingOneFileInPaths(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	writeHeaderBrief(t, dir, "q1", "-", "internal/x.go, internal/q1.go")
	writeHeaderBrief(t, dir, "q2", "-", "internal/q2.go internal/x.go")
	code, out, errs := ta.do("add --stream s --brief-dir " + dir)
	require.Equal(t, 2, code, "shared PATHS: %s%s", out, errs)
	assert.Contains(t, errs, "internal/x.go is named in PATHS by q1 and q2, and neither needs the other")
	assert.NotContains(t, out, "MOVED", "nothing written")
	chained := t.TempDir()
	writeHeaderBrief(t, chained, "r1", "-", "internal/y.go")
	writeHeaderBrief(t, chained, "r2", "r1", "internal/y.go")
	assert.Contains(t, ta.ok("add --stream u --brief-dir "+chained), "ADD OK stream=u cards=2 before=- moved=2", "a chain shares its file in turn")
	ledger := t.TempDir()
	writeHeaderBrief(t, ledger, "f1", "-", "FIXES.md internal/f1.go")
	writeHeaderBrief(t, ledger, "f2", "-", "FIXES.md internal/f2.go")
	assert.Contains(t, ta.ok("add --stream v --brief-dir "+ledger), "ADD OK stream=v cards=2 before=- moved=2", "a ledger the lander merges is shared freely")
}

// A SHARED: header line does not admit a non-ledger path: two cards that declare
// the same non-ledger file on a SHARED: line (and on PATHS) are refused the same
// way as if the line were absent, naming the file and the cards (docs/SPEC-CARD-
// CONTRACT.md, the shared-path rule). Only a ledger the lander merges, or a needs
// edge, admits a shared path.
func TestAddRefusesASharedPathNonLedgerBothBriefsDeclare(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	write := func(dir, id, paths, shared string) {
		t.Helper()
		lead := "RESULT: " + id + " sha=000000000000 tier: flash\nKIND: fix\nDEPENDS-ON: -\nPATHS: " + paths + "\nSHARED: " + shared + "\n\nFix " + id + "."
		require.NoError(t, os.WriteFile(filepath.Join(dir, id+".md"), []byte(passingBrief(lead)), 0o600))
	}
	dir := t.TempDir()
	write(dir, "l1", "docs/ledger.md internal/l1.go", "docs/ledger.md")
	write(dir, "l2", "docs/ledger.md, internal/l2.go", "docs/ledger.md")
	code, _, errs := ta.do("add --stream s --brief-dir " + dir)
	require.Equal(t, 2, code, "a SHARED: line does not admit a non-ledger path")
	assert.Contains(t, errs, "docs/ledger.md is named in PATHS by l1 and l2, and neither needs the other")

	half := t.TempDir()
	write(half, "h1", "docs/ledger.md internal/x.go", "docs/ledger.md")
	write(half, "h2", "docs/ledger.md internal/x.go", "-")
	code, _, errs = ta.do("add --stream t --brief-dir " + half)
	require.Equal(t, 2, code)
	assert.Contains(t, errs, "docs/ledger.md is named in PATHS by h1 and h2, and neither needs the other")
}
