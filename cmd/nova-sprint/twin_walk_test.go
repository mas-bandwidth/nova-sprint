package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/onboarding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTwinWalkBriefsTheCardBeforeTheFirstTickAndSaysTheMemberIsUp pins the twin
// walkthrough's teaching: the transcript (the docs/TESTS.md first run and the
// flow twinWords() shows) adds the card with a brief before any tick, says in
// one sentence that a twin member is up at the next tick, and never speaks of a
// member down until it beats. Its commands run against a mem twin through the
// package's rig.
func TestTwinWalkBriefsTheCardBeforeTheFirstTickAndSaysTheMemberIsUp(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "TESTS.md"))
	require.NoError(t, err)
	lines, err := onboarding.FirstRun(string(raw), "nova-sprint")
	require.NoError(t, err)
	steps, err := onboarding.Steps("nova-sprint", lines)
	require.NoError(t, err)

	require.Len(t, steps, len(twinSteps)-2, "the first-run transcript holds the help's flow to the merge")
	for i, s := range steps {
		assert.Equal(t, "$ "+twinSteps[i], s.Line, "command %d of the transcript", i+1)
	}

	addLine := twinSteps[1]
	assert.Contains(t, addLine, "--brief-file", "the transcript adds the card with a brief: %q", addLine)

	help := twinWords()
	for _, line := range twinSteps {
		assert.Contains(t, help, "  "+line+"\n", "the twin help does not show %q", line)
	}
	assert.NotContains(t, help, "down until it beats", "the twin help says nothing of a member down until it beats")
	assert.Contains(t, help, "added is up from the next tick", "the twin help says a member is up at the next tick")

	file := filepath.Join(t.TempDir(), "sprint.twin")
	for _, s := range steps {
		code, out, errs := twinProcess(t, file, strings.TrimPrefix(s.Line, "$ "))
		require.Equal(t, 0, code, "the documented command\n  %s\nexited %d\n%s%s", s.Line, code, out, errs)
	}
}
