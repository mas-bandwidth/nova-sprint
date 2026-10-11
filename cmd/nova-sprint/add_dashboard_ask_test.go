package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// add and brief refuse a card that changes the dashboard page unless its SOURCE: line quotes
// the owner asking for that visible change (swarm.LintDashboardAsk; the owner, 2026-10-11:
// "NEVER REMOVE THINGS FROM THE DASHBOARD UNLESS I ASK"): known no, refused with the reason and
// nothing written; known yes, admitted.
func TestAddAndBriefRefuseADashboardCardWithoutTheOwnersAsk(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead")
	write := func(name, text string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), name+".md")
		require.NoError(t, os.WriteFile(path, []byte(passingBrief(text)), 0o600))
		return path
	}
	header := "RESULT: c sha=0123456789ab tier: pro\nPATHS: internal/sprintdash/page/app.js, internal/sprintdash/*_test.go\nTEST: internal/sprintdash TestY\n"
	unasked := header + "\nTHE TASK. Remove the release picker."
	asked := "RESULT: c sha=0123456789ab tier: pro\nPATHS: internal/sprintdash/page/app.js, internal/sprintdash/*_test.go\nTEST: internal/sprintdash TestY\nSOURCE: the owner, 2026-10-11: \"remove the release picker\"\n\nTHE TASK. Remove the release picker."

	code, out, stderr := ta.do("add --stream s1 --one --actor lead --brief-file " + write("unasked", unasked))
	assert.Equal(t, 2, code, out+stderr)
	assert.Contains(t, stderr, "dashboard-asked-for")
	assert.Contains(t, stderr, "NEVER REMOVE THINGS FROM THE DASHBOARD UNLESS I ASK", "the refusal says why")
	assert.Contains(t, stderr, "nova-sprint add REFUSED")
	assert.False(t, ta.placed("unasked"), "nothing written")

	out = ta.ok("add --stream s1 --one --actor lead --brief-file " + write("asked", asked))
	assert.Contains(t, out, "MOVED asked -> ready")

	// brief: the admitted card's new brief without the ask is refused, and its brief stands
	code, out, stderr = ta.do("brief asked --actor lead --brief-file " + write("asked", unasked))
	assert.Equal(t, 2, code, out+stderr)
	assert.Contains(t, stderr, "dashboard-asked-for")
	assert.Contains(t, stderr, "nova-sprint brief REFUSED")
}
