package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestCompactSayPrintsACriticalJudgment pins that a judgment whose cards wait on
// it is printed, not dropped: its group line is prefixed `CRITICAL <n> behind:`
// (reads.go, groupLine, when g.Behind reaches sprint.CriticalBehind), so the
// compact filter decides "is this a judgment" by its HAPPENED prefix alone, never
// by a literal leading JUDGMENT. A dropped critical judgment is the one the seat
// most needs. (docs/CLI.md, `nova-sprint hook --compact`; docs/SPEC-SPRINT.md,
// "The hook".) The clock is the test's own (t0), never the wall's.
func TestCompactSayPrintsACriticalJudgment(t *testing.T) {
	t.Parallel()
	now := t0
	g := sprint.Group{Kind: sprint.Judgment, ID: "j1", Type: sprint.NWorkFailed, Alias: "j1",
		Stream: "s1", Size: 1, Primaries: []string{"s1-1"}, Behind: sprint.CriticalBehind + 130,
		Oldest: now.Add(-10 * time.Second), What: "the first detail line"}
	var b bytes.Buffer
	compactSay(&b, "seat", "addr", "env", hookMsg{T: "push", ID: 7, Text: groupText(g, now, false)})
	out := b.String()
	require.Contains(t, out, "PUSH 7 CRITICAL 140 behind: JUDGMENT j1", "the critical judgment is printed, its group line intact")
	require.Contains(t, out, "  the first detail line\n", "the critical judgment's first detail line is printed")
}
