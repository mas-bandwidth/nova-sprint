//go:build functional

package store

import "testing"

func TestRedisStartGuardAndClearAdvanceShareFence(t *testing.T) {
	h, _ := liveHarness(t)
	scenarioStartGuardRacingAdvance(t, h)
}
func TestRedisClearStopsAnInterveningCompletedStart(t *testing.T) {
	h, _ := liveHarness(t)
	scenarioCompletedStartBeforeClearAdvance(t, h)
}
func TestRedisAdvanceHonorsFromEpochFence(t *testing.T) {
	h, _ := liveHarness(t)
	scenarioAdvanceHonorsFromEpochFence(t, h)
}
func TestRedisStartRefusesAfterEpochMoved(t *testing.T) {
	h, _ := liveHarness(t)
	scenarioStartRefusesAfterEpochMoved(t, h)
}
