package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHoldHelpHasAFamilyMap pins the one-screen map at the head of holdWords:
// the hold-family verb to reach for when, and the read verbs a new reader
// meets, so a stranger reading help hold never has to guess (docs/SPEC-SPRINT.md
// section 11, the hold family map).
func TestHoldHelpHasAFamilyMap(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	help := ta.ok("help hold")

	start := strings.Index(help, "the hold family, one screen")
	require.NotEqual(t, -1, start, "help hold carries the family map")
	end := strings.Index(help[start:], "Hold and unhold:")
	require.NotEqual(t, -1, end, "the map sits at the head of holdWords")
	block := strings.Trim(help[start:start+end], "\n")
	assert.LessOrEqual(t, len(strings.Split(block, "\n")), 24, "the map is at most 24 lines")

	for _, want := range []string{
		"hold <member|reader|friend|stream>",
		"fleet hold",
		"friend hold",
		"unhold <name>",
		"defer",
		"where",
		"dashboard",
		"view",
	} {
		assert.Contains(t, block, want, "the map names %q", want)
	}
}
