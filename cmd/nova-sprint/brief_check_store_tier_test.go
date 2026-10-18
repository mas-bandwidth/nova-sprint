package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 2026-10-18: a card's tier is a field of the card (FieldTier), and a brief whose line 1
// names another tier keeps the recorded one: `brief` only prints "tier kept: pro (the brief's
// line 1 says flash)" after the edit is made. brief check, given a file whose id is a card on
// the table, says the mismatch before the edit, as a `tier` finding naming both tiers; a file
// whose line 1 names the recorded tier, and a file whose id is no card, report nothing.
func TestBriefCheckSeesATierThatDiffersFromTheStoredOne(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	repo := "mas-bandwidth/nova-sprint"
	c := writeSoftHeaderBrief(t, dir, "c", "-", "", repo, "", "internal/c.go") // line 1: tier: flash
	ta.ok("add --stream s1 --one --brief-file " + c)

	_, out, _ := ta.do("brief check --stream s1 " + c)
	require.NotContains(t, out, " tier:", "the file names the tier the card holds: "+out)

	ta.ok("brief c --tier pro --reason 'the work is bigger than flash'")
	require.Equal(t, "pro", ta.primary("c").F("tier"))
	_, out, _ = ta.do("brief check --stream s1 " + c)
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "CHECK "+c+" tier:") {
			line = l
		}
	}
	require.NotEmpty(t, line, "line 1 says flash, the card holds pro, and brief check says so:\n%s", out)
	require.Contains(t, line, "flash", line)
	require.Contains(t, line, "pro", line)

	fresh := writeSoftHeaderBrief(t, dir, "fresh", "-", "", repo, "", "internal/f.go")
	_, out, _ = ta.do("brief check --stream s1 " + fresh)
	require.NotContains(t, out, " tier:", out)
}
