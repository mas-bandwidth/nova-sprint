package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// where answers "why is the fleet idle" in its first line (docs/SPEC-SPRINT.md section 14):
// when cards are waiting, the top three blockers are printed as the second line under
// the progress summary, and where --json carries blockers and idle.
func TestWhereCarriesIdleBlockers(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1:8")
	ta.ok("add --stream s1 --count 2 --brief-file " + proBriefFile(t))
	ta.ok("add --one --stream s2 --count 1 --brief-file " + proBriefFile(t) + " --needs s1-1")

	// before start / tick: no blockers running yet, but cards wait
	ta.ok("tick")
	var v whereView
	ta.json("where", &v)
	require.NotEmpty(t, v.Blockers, "where --json carries blockers")
	require.NotEmpty(t, v.Idle, "where --json carries idle line")
	assert.Equal(t, "idle: "+strings.Join(v.Blockers, "; "), v.Idle)

	text := ta.ok("where")
	lines := strings.Split(text, "\n")
	var idleLine string
	for _, l := range lines {
		if strings.HasPrefix(l, "idle: ") {
			idleLine = l
			break
		}
	}
	require.NotEmpty(t, idleLine, "where output prints idle line: %s", text)
	assert.Equal(t, v.Idle, idleLine)

	// Now when all cards land and no cards wait: idle line is absent
	ta.ok("start")
	ta.landStream("s1", []string{"", ""}, []string{"", ""}, []string{"", ""})
	ta.ok("tick")
	ta.landStream("s2", []string{""}, []string{""}, []string{""})
	ta.ok("tick")

	var vDone whereView
	ta.json("where", &vDone)
	assert.Empty(t, vDone.Blockers, "no blockers when all cards landed")
	assert.Empty(t, vDone.Idle, "no idle line when all cards landed")

	doneText := ta.ok("where")
	assert.NotContains(t, doneText, "idle: ")
}
