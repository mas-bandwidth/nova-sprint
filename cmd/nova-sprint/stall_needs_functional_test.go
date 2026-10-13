//go:build functional

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- need cycles (#103) ---

// TestStallANeedCycleThroughAWorkingCardIsRefusedOnTheStore is #103 on a real store: a dealt
// card that gains a need keeps the edge while it works, and a rewrite that closes a loop
// through it was taken, so the tick withdrew the working card to waiting and the two cards
// waited on each other for ever. The rewrite is refused (exit 2, nothing written, the loop
// named) and the tick deals the need: no card is left waiting on a cycle.
func TestStallANeedCycleThroughAWorkingCardIsRefusedOnTheStore(t *testing.T) {
	t.Parallel()
	r := newStallRig(t)
	r.do("init", "--readers", "reader-a,reader-b", "--members", "m1:1")
	declareStallFlashRoute(t, r)
	r.do("add", "--stream", "s1", "--count", "2")
	r.beat("m1")
	r.do("start")
	r.tick() // presence: the members up
	r.tick() // the deal
	require.Equal(t, "working", r.col("s1-1"), "the deal: s1-1")
	require.Equal(t, "working", r.col("s1-2"), "the deal: s1-2, dealt ahead")

	r.do("needs", "set", "s1-1", "--needs", "s1-2")
	code, out, errs := r.run("needs", "set", "s1-2", "--needs", "s1-1")
	assert.NotEqual(t, 0, code, "the rewrite closes a loop through the working card: %s%s", out, errs)
	assert.Contains(t, out+errs, "cycle", "the refusal names the cycle")
	assert.Empty(t, r.card("s1-2").Primary.F("needs"), "nothing is written")

	// the machine: s1-1 goes back to waiting for s1-2, which is dealt: nothing waits on a loop
	for i := 0; i < 3; i++ {
		r.tick()
	}
	assert.Equal(t, "waiting", r.col("s1-1"), "a dealt card whose need has not landed waits for it")
	assert.Equal(t, "working", r.col("s1-2"), "its need stays dealt: no cycle holds either card")
	code, out, errs = r.run("check")
	assert.Equal(t, 0, code, "check: %s%s", out, errs)
}
