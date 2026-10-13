//go:build functional

package store

import "testing"

// The owed step's restore at most once on the real store (clear_restore_once_test.go): a
// clear crashed after its restore, an edit or a removal in the new epoch, then the clear
// run again, on a Redis of the test's own.

func TestRedisARerunAfterACrashKeepsATierSetSince(t *testing.T) {
	h, _ := liveHarness(t)
	scenarioATierSetAfterACrashSurvivesTheRerun(t, h, crashAfterRestore)
}

func TestRedisARerunAfterACrashKeepsARowRemovedSince(t *testing.T) {
	h, _ := liveHarness(t)
	scenarioARowRemovedAfterACrashStaysRemoved(t, h, crashAfterRestore)
}

func TestRedisARerunReportsOnlyTheResetsItWrote(t *testing.T) {
	h, _ := liveHarness(t)
	scenarioAChoiceSetAfterACrashIsKeptNotReportedReset(t, h, crashAfterRestore)
}
