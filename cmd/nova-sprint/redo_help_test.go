package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRedoHelpSaysConflictsOnlyAndPointsToRework verifies that redo's help names its
// conflict-only scope and points to rework, that redo of a card in review is refused
// naming nova-sprint rework, and that rework prints an effect line
// (docs/SPEC-SPRINT.md, docs/CLI.md; seat friction 11, epoch 20).
func TestRedoHelpSaysConflictsOnlyAndPointsToRework(t *testing.T) {
	t.Parallel()

	a := newApp(func(string) string { return "" })

	// 1. help redo contains "merge conflicts only" and "rework".
	var redoOut, redoErr bytes.Buffer
	code := runInto(a, []string{"help", "redo"}, &redoOut, &redoErr)
	require.Equal(t, 0, code, "help redo: %s", redoErr.String())
	assert.Contains(t, redoOut.String(), "merge conflicts only")
	assert.Contains(t, redoOut.String(), "rework")

	// 2. help rework prints an effect line.
	var reworkOut, reworkErr bytes.Buffer
	code = runInto(a, []string{"help", "rework"}, &reworkOut, &reworkErr)
	require.Equal(t, 0, code, "help rework: %s", reworkErr.String())
	assert.Contains(t, reworkOut.String(), "effect: store write:")

	// 3. redo of a card in review is refused naming nova-sprint rework.
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 1 --one")
	ta.ok("start")
	ta.ok("tick")
	ta.ok("take --as m1 s1-1.w1@1")
	ta.ok("finish --as m1 s1-1.w1@1 --report done")

	code, out, errb := ta.do("redo s1-1")
	require.Equal(t, 1, code, "redo of card in review must be refused: %s", out)
	assert.Contains(t, errb, "not in a conflict")
	assert.Contains(t, errb, "nova-sprint rework")
}
