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

// Two clears at once and a verb racing a clear, on the real store: the restore's rows and a
// verb's rows go in one WATCH/MULTI/EXEC with the read of the epoch, its flag and the marker
// (clear_concurrent_test.go).

func TestRedisTwoClearsAtOnceRestoreOnce(t *testing.T) {
	h, _ := liveHarness(t)
	scenarioTwoClearsOneRestore(t, h)
}

func TestRedisASetTiersRacingAClearWritesNothing(t *testing.T) {
	h, _ := liveHarness(t)
	scenarioAVerbRacingAClearWritesNothing(t, h, func(h *harness) error {
		return h.st.SetReaderTiers(h.ctx, []string{"reader-a"}, "")
	})
}

func TestRedisAReaderRemoveRacingAClearWritesNothing(t *testing.T) {
	h, _ := liveHarness(t)
	scenarioAVerbRacingAClearWritesNothing(t, h, func(h *harness) error {
		return h.st.RemoveReaders(h.ctx, []string{"reader-b"})
	})
}
