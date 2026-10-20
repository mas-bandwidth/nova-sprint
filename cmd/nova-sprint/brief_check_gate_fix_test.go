package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// brief check names the gate-fix cycle over a directory holding both briefs:
// G's DONE-WHEN and F's DONE-WHEN run the same command, and F needs G. The
// finding is gate-fix-cycle, names both ids, and the call exits as brief check
// does for a finding (docs/SPEC-SPRINT.md section 2; tla/WaitFor.tla, the
// gate-artifact edge and NoWaitCycle).
func TestBriefCheckGateFix(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	g := writeBriefFile(t, dir, "g", passingBrief(
		"RESULT: g sha=0123456789ab tier: pro\nDONE-WHEN: `make tables-x-fuzz-all`\nTHE TASK. Gate the tables."))
	f := writeBriefFile(t, dir, "f", passingBrief(
		"RESULT: f sha=0123456789ab tier: pro\nDEPENDS-ON: g\nDONE-WHEN: `ELIXIR=elixir make tables-x-fuzz-all`\nTHE TASK. Fix the tables."))
	before := ta.applies()
	code, out, errs := ta.do("brief check --stream s1 " + dir)
	require.Equal(t, 1, code, "exit %d\n%s%s", code, out, errs)
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, " gate-fix-cycle:") {
			line = l
		}
	}
	require.NotEmpty(t, line, "brief check names the gate-fix cycle:\n%s", out)
	require.Contains(t, line, "g")
	require.Contains(t, line, "f")
	require.Contains(t, line, "tables-x-fuzz-all")
	require.Contains(t, line, f, "the finding is filed on the dependent brief")
	require.NotContains(t, out, "CHECK "+g+" gate-fix-cycle:", "the finding is filed on the dependent brief")
	require.NotContains(t, out, " plan: gate-fix", "the finding is gate-fix-cycle, not a duplicate plan refusal")
	require.Equal(t, before, ta.applies(), "brief check wrote")
}
