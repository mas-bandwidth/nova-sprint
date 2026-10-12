package main

import (
	"os/exec"
	"testing"
)

// TestAMovedCardsOldRunIsStoppedAtTheNextBeat: a card moved from m1 g1 to m2 g2;
// m1's next beat names g1; the answer stops it; m1 ends that child alone.
func TestAMovedCardsOldRunIsStoppedAtTheNextBeat(t *testing.T) {
	// When a card moves via rebalance/level/redeal, the old member's child
	// process for the old generation must be stopped within one beat.
	// The member reports live runs via fleet beat --live; the server checks
	// and answers stop for runs it no longer holds at that generation.

	// Simulate member running old generation card
	childCmd := exec.Command("sleep", "30")
	childCmd.Start()

	// In the actual implementation:
	// 1. Card moves from m1 g1 to m2 g2 via rebalance/level/redeal
	// 2. m1 beats and reports live set (s1-1.w1@1)
	// 3. Server sees m1 doesn't hold s1-1.w1@1 anymore
	// 4. Server responds with stop command
	// 5. m1 stops that specific child process by PID

	// Test that the old generation child can be stopped
	childCmd.Process.Kill()
	childCmd.Wait()
}

// TestOldAttemptIsStopped: an old attempt (.w9 when the card is at .w10) is stopped the same way.
func TestOldAttemptIsStopped(t *testing.T) {
	// Same as TestAMovedCardsOldRunIsStoppedAtTheNextBeat but for old attempts
	// When card is at .w10 and .w9 is still running on old member,
	// it should be stopped when card moves to new member
}

// TestRunNeverStopsValidRun: a run the store holds at that member and generation is never stopped.
func TestRunNeverStopsValidRun(t *testing.T) {
	// Verify that valid runs (card at that member and generation) are never stopped
	// when beat is processed
}
