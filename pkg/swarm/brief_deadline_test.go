package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// BriefDeadline reads the brief's DEADLINE: line, or the template's Deadline: line up to its
// semicolon, as the deadline-p95 check reads a value; the template's placeholder is none.
func TestBriefDeadlineReadsTheBriefsOwnDeadline(t *testing.T) {
	t.Parallel()
	for brief, want := range map[string]int{
		"tier: pro\nDEADLINE: 240m\n\nTHE TASK.":           14400,
		"tier: pro\nDEADLINE: 2700\n":                      2700,
		"tier: pro\nDEADLINE: finish within 180 minutes\n": 10800,
		"tier: pro\nDeadline: finish within 240 minutes; the judgment of a card that runs past it is the coordinator's\n": 14400,
	} {
		got, ok := BriefDeadline(brief)
		assert.True(t, ok, brief)
		assert.Equal(t, want, got, brief)
	}
	for _, brief := range []string{
		"tier: pro\n\nTHE TASK.",
		"tier: pro\nDeadline: finish within <n> minutes; the judgment of a card that runs past it is the coordinator's\n",
		"tier: pro\nDEADLINE: soon\n",
	} {
		_, ok := BriefDeadline(brief)
		assert.False(t, ok, brief)
	}
}
