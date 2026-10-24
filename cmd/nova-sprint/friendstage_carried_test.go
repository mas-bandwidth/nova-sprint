package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// JOB.md names the head a job starts from as CARRIED, with the attempt that pushed it: a
// friend's attempt redealt unfinished hands its pushed work to the next (sprint.PushedHead).
func TestFriendStageNamesTheCarriedHead(t *testing.T) {
	t.Parallel()
	const head = "89abcdef0123456789abcdef0123456789abcdef"
	assert.Equal(t, "CARRIED: attempt 1 head "+head+" (its pushed work: continue it, never start again from BASE)\n", friendStageCarried(1, head))
	assert.Contains(t, friendStageCarried(0, head), "CARRIED: head "+head)
}
