package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoadmapAddMissingOriginExample: card work-roadmap-add-origin-examples.
// roadmap add for a fix without --origin refuses at exit 2 and the refusal
// line itself shows valid origin formats.
func TestRoadmapAddMissingOriginExample(t *testing.T) {
	t.Parallel()
	dir := workRecord(t)
	res := workMain(unreachable(t)).Run("roadmap", "add", "--repo", dir, "--id", "f9", "--release", "v1.0.1", "--title", "F9")
	require.Equal(t, 2, res.Code, "%s%s", res.Stdout, res.Stderr)
	out := res.Stdout + res.Stderr
	assert.Contains(t, out, "PR #123")
	assert.Contains(t, out, "40")
	assert.Contains(t, out, "SHA")
}
