package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// brief check promises it answers as add does, so it runs add's probable-duplicate
// check over the briefs it holds against the open cards of the one read-only
// snapshot it already loaded: an open card and a near-identical brief is one
// `duplicate` finding naming the card add would hold it against, with add's words,
// and exit 1; an unrelated brief (another PATHS file) names none. brief check
// takes no --not-a-duplicate-of, and its remedy says that add does.
func TestBriefCheckNamesAProbableDuplicate(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	repo := "mas-bandwidth/nova-sprint"
	open := writeBriefFile(t, dir, "dup-open", passingBrief(
		"RESULT: dup-open sha=0123456789ab tier: pro\nKIND: fix\nREPO: "+repo+
			"\nPATHS: internal/dupdemo.go\nTHE TASK. Fix the duplicate demo case in the parser."))
	ta.ok("add --stream s1 --one --brief-file " + open)

	near := writeBriefFile(t, dir, "dup-new", passingBrief(
		"RESULT: dup-new sha=0123456789ab tier: pro\nKIND: fix\nREPO: "+repo+
			"\nPATHS: internal/dupdemo.go\nTHE TASK. Fix the duplicate demo case in the parser."))
	other := writeBriefFile(t, dir, "other-card", passingBrief(
		"RESULT: other-card sha=0123456789ab tier: pro\nKIND: fix\nREPO: "+repo+
			"\nPATHS: internal/other.go\nTHE TASK. Chart the unrelated roadmap totals."))

	code, out, errs := ta.do("brief check --stream s1 " + near + " " + other)
	require.Equal(t, 1, code, "exit %d\n%s%s", code, out, errs)
	require.Equal(t, 1, strings.Count(out, " duplicate:"), "one finding, the near-identical brief:\n%s", out)
	require.Contains(t, out, "CHECK "+near+" duplicate: probable duplicate of dup-open", out)
	require.Contains(t, out, "--not-a-duplicate-of", out)
	require.NotContains(t, out, "CHECK "+other+" ", out)
}
