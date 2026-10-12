package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tierLineRules is the built-in rules paragraph every card brief quotes.
const tierLineRules = "RULES.\nWork only in the job directory this card names.\nNever force-push or rebase a shared branch.\n" +
	"Never kill a process you did not start.\nNever start a server on this machine.\n" +
	"No `rm -rf` outside the job directory.\nReport what was not done.\n"

// tierBrief writes a brief whose line 1 is line and whose next lines are more, and returns
// its path.
func tierBrief(t *testing.T, name, line, more string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name+".md")
	require.NoError(t, os.WriteFile(p, []byte(line+"\n"+more+"\nThe task.\n\n"+tierLineRules), 0o600))
	return p
}

// add prints the tier each card was stored with, the tier its first deal draws: line 1's in
// either form, flash for a free task that names none (the seat, 2026-10-12: six cards whose
// RESULT line said tier: heavy were stored with no tier and dealt on flash, and the admission
// said nothing). A card brief (PATHS:) whose line 1 names no tier is refused by the card
// checks (tier-line) unless its stream names a default tier, which it is then stored with.
func TestAddPrintsTheTierEachCardIsStoredWith(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	out := ta.ok("add --stream s1 --one s1-1 --brief-file " + tierBrief(t, "heavy", "RESULT: s1-1 sha=000000000000 tier: heavy", ""))
	assert.Contains(t, out, "s1-1 -> ready", out)
	assert.Contains(t, out, "tier=heavy", "add says the tier it stored: %s", out)
	assert.Contains(t, ta.ok("add --stream s1 --one s1-2 --brief-file "+tierBrief(t, "pro", "c: the work (s1) tier: pro", "")), "tier=pro")
	assert.Contains(t, ta.ok("add --stream s1 --one s1-5 --brief-file "+tierBrief(t, "free", "c: the work (s1)", "")), "tier=flash", "a free task that names no tier is flash")

	card := "PATHS: internal/x/*.go\n"
	code, out, errs := ta.do("add --stream s1 --one s1-3 --brief-file " + tierBrief(t, "none", "RESULT: s1-3 sha=000000000000", card))
	assert.Equal(t, 2, code, "%s%s", out, errs)
	assert.Contains(t, errs, "LINT DRIFT card=s1-3 check=tier-line line=1", "a card brief whose line 1 names no tier is refused")

	ta.ok("stream set s1 --default-tier pro")
	out = ta.ok("add --stream s1 --one s1-4 --brief-file " + tierBrief(t, "none2", "RESULT: s1-4 sha=000000000000", card))
	assert.Contains(t, out, "tier=pro", "stored with the stream's default: %s", out)
	code, _, errs = ta.do("stream set s1 --default-tier frontier")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, errs, "--default-tier wants flash, pro or heavy")
}

// brief prints the tier the re-briefed card is dealt on (<id> tier=<t>), the tier its new
// line 1 names.
func TestBriefPrintsTheTierTheCardIsDealtOn(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --one s1-1 --brief-file " + tierBrief(t, "flash", "c: the work (s1) tier: flash", ""))
	out := ta.ok("brief s1-1 --brief-file " + tierBrief(t, "heavy", "c: the work (s1) tier: heavy", ""))
	assert.Contains(t, out, "s1-1 tier=heavy", "brief says the tier: %s", out)
}
