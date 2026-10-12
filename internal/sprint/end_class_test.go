package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Every attempt end is classed as the worker's own work (EndWorker) or as coordination
// (EndCoordination): everything the machine, bench, harness, provider or seat caused. The
// card's rule: coordination ends never count against a worker, never consume an attempt
// toward the bound, and accumulate on a coordination-failure counter by cause. Unknown is
// coordination until classed -- never blame by default.
func TestClassEndClassesEachEndReason(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		verdict, report string
		wantClass       EndClass
		wantCause       CoordinationCause
	}{
		{"harness exit within seconds with no RESULT (crash before first turn)", "not-done", "the harness exited after 2s with no RESULT: crash before the first turn", EndCoordination, CauseHarnessCrash},
		{"a provider or account limit", "not-done", "provider failure: class=usage limit: the account is at its limit", EndCoordination, CauseProviderLimit},
		{"a provider content-filter or key refusal", "not-done", "provider failure: content-filter refusal: the prompt was refused", EndCoordination, CauseContentFilter},
		{"an idle-reap with no run (ran=false, idle=0s)", "not-done", "the lane was reaped at idle=0s with no run: ran=false", EndCoordination, CauseIdleReap},
		{"a missing bench tool or jar: TLC owed with no jar", "not-done", "attempt ended TLC owed on a bench without the jar", EndCoordination, CauseMissingTool},
		{"a missing bench tool: no rustc on PATH", "not-done", "the lane failed: no rustc on PATH", EndCoordination, CauseMissingTool},
		{"staging refused (no bench mirror)", "not-done", "staging refused: no bench mirror", EndCoordination, CauseStagingRefused},
		{"the tick taking a card back", "not-done", "the tick took the card back: no beat carried her live set", EndCoordination, CauseTakenBack},
		{"a hold taking a card back", "not-done", "a hold returned the card: taken back", EndCoordination, CauseTakenBack},
		{"a card dealt with unlanded needs", "not-done", "the card was dealt before its needs landed: needs not landed", EndCoordination, CauseUnlandedNeeds},
		{"a hook-gate refusal", "not-done", "the hook-gate refused the push", EndCoordination, CauseHookGate},
		{"a server or store fault", "not-done", "store fault: the server errored", EndCoordination, CauseStoreFault},
		{"a landing refused for a conflict with another card's landing", "not-done", "landing refused: conflict with another card's landing", EndCoordination, CauseLandConflict},
		{"a not-done verdict with a pushed head", "not-done", "verdict not-done; pushed=abc to b", EndWorker, ""},
		{"a gate refusal on the worker's own diff", "not-done", "the gate refused your own diff at the push", EndWorker, ""},
		{"a broken read verdict on its head", "broken", "a broken read stands at the head of the attempt", EndWorker, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			class, cause := ClassEnd(tc.verdict, tc.report)
			assert.Equal(t, tc.wantClass, class, "class")
			if tc.wantClass == EndCoordination {
				assert.Equal(t, tc.wantCause, cause, "cause")
			} else {
				assert.Empty(t, cause, "a worker end has no coordination cause")
			}
		})
	}
}

// Unknown is coordination until classed: a reason nothing names is never blamed on the worker.
func TestUnknownEndIsCoordination(t *testing.T) {
	t.Parallel()
	class, cause := ClassEnd("failed", "something we have never seen")
	assert.Equal(t, EndCoordination, class)
	assert.Equal(t, CauseUnknown, cause)
}

// Every coordination cause names itself, and the worker class is never a cause.
func TestCoordinationCausesNameThemselves(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "coordination", string(EndCoordination))
	assert.Equal(t, "worker", string(EndWorker))
	assert.Equal(t, "unknown", string(CauseUnknown))
}
