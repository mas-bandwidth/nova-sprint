package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 2026-10-18: brief check reads the file alone, and a card's soft dependencies live in the
// store (needs set --land-after, add's record of the LAND-AFTER: line), so the file a
// coordinator checks before `brief <id> --brief-file` could say LAND-AFTER: a while the card
// holds b, and the check came back clean: the two drifted apart unseen. Given a file whose
// id is a card on the table, brief check reads the card's edges from the store and reports
// a file that would change them as `land-after`, naming both lists; a file that says what the
// store holds, and a file whose id is no card, report nothing.
func TestBriefCheckSeesTheLandAfterTheStoreHolds(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	repo := "mas-bandwidth/nova-sprint"
	a := writeSoftHeaderBrief(t, dir, "a", "-", "", repo, "", "internal/a.go")
	b := writeSoftHeaderBrief(t, dir, "b", "-", "", repo, "", "internal/b.go")
	c := writeSoftHeaderBrief(t, dir, "c", "-", "a", repo, "", "internal/c.go")
	ta.ok("add --stream s1 --brief-file " + a + " --brief-file " + b + " --brief-file " + c)
	require.Equal(t, "a", ta.primary("c").F("land_after"))

	// the card says LAND-AFTER: a, the file says the same: no finding about it
	_, out, _ := ta.do("brief check --stream s1 " + c)
	require.NotContains(t, out, " land-after:", out)

	// needs set moves the store's edge to b; the file still says a
	ta.ok("needs set c --land-after b")
	require.Equal(t, "b", ta.primary("c").F("land_after"))
	_, out, _ = ta.do("brief check --stream s1 " + c)
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "CHECK "+c+" land-after:") {
			line = l
		}
	}
	require.NotEmpty(t, line, "the file names a, the store holds b, and brief check says so:\n%s", out)
	require.Contains(t, line, "LAND-AFTER", line)
	require.Contains(t, line, "a", line)
	require.Contains(t, line, "b", line)

	// a file whose id is no card on the table has no edges to differ from
	fresh := writeSoftHeaderBrief(t, dir, "fresh", "-", "a", repo, "", "internal/f.go")
	_, out, _ = ta.do("brief check --stream s1 " + fresh)
	require.NotContains(t, out, " land-after:", out)
}
