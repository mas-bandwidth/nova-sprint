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

// The coordination-failure counter accumulates each coordination end by cause and by the
// member or friend who suffered it, per epoch, and never counts a worker's own end (the
// owner, 2026-10-11: "failures not attributable to the model, are supposed to accumulate
// to a counter for coordination failures.").
func TestCoordinationCounterAccumulatesByCauseAndWho(t *testing.T) {
	t.Parallel()
	c := NewCoordinationCounter(19)
	// a worker's own end is never a coordination failure
	if class, _ := c.Record("not-done", "verdict not-done; pushed=abc to b", "hetzner2"); class != EndWorker {
		t.Fatalf("a pushed not-done is the worker's")
	}
	if c.Total != 0 || c.WhoTotal("hetzner2") != 0 {
		t.Fatalf("a worker end must not move the coordination counter")
	}
	c.Record("not-done", "usage limit reached", "hetzner2")
	c.Record("not-done", "weekly usage limit reached", "hetzner2")
	c.Record("failed", "no rustc on PATH", "friend-a")
	c.Record("not-done", "something never seen", "friend-a")
	assert.Equal(t, 19, int(c.Epoch), "the counter is per epoch")
	assert.Equal(t, 4, c.Total, "total")
	assert.Equal(t, 2, c.CauseTotal(CauseProviderLimit), "by cause")
	assert.Equal(t, 1, c.CauseTotal(CauseMissingTool), "by cause")
	assert.Equal(t, 1, c.CauseTotal(CauseUnknown), "unknown is coordination, counted")
	assert.Equal(t, 2, c.WhoTotal("hetzner2"), "per member")
	assert.Equal(t, 2, c.WhoTotal("friend-a"), "per friend")
	assert.Equal(t, 0, c.WhoTotal("stranger"), "no end, no count")
}

// The counter prints one deterministic line for the dashboard, causes in a settled order,
// for the table under the fleet and friends (the owner asked for it there).
func TestCoordinationCounterLine(t *testing.T) {
	t.Parallel()
	c := NewCoordinationCounter(19)
	c.Record("not-done", "TLC owed on a bench without the jar", "hetzner2")
	c.Record("not-done", "usage limit reached", "hetzner2")
	c.Record("not-done", "usage limit reached", "friend-a")
	assert.Equal(t,
		"coordination failures: 3 total (missing bench tool or jar: 1; provider or account limit: 2)",
		c.Line())
}

// A coordination end pushed to the coordinator's hook is exactly the ends the counter
// keeps: ClassEnd says coordination, the counter records it, and the hook word carries the
// cause so the seat can fix it.
func TestCoordinationCounterOnlyRecordsCoordination(t *testing.T) {
	t.Parallel()
	c := NewCoordinationCounter(19)
	class, cause := c.Record("not-done", "harness exited after 2s with no RESULT: crash before the first turn", "hetzner2")
	assert.Equal(t, EndCoordination, class)
	assert.Equal(t, CauseHarnessCrash, cause)
	assert.Equal(t, 1, c.Total)
	assert.Equal(t, 1, c.CauseTotal(CauseHarnessCrash))
}

// A counter is per epoch: records of another epoch never leak into this one (a fresh
// counter starts at zero however many the sprint has seen).
func TestCoordinationCounterPerEpoch(t *testing.T) {
	t.Parallel()
	c := NewCoordinationCounter(20)
	assert.Equal(t, 0, c.Total, "a fresh epoch starts at zero")
	assert.Equal(t, 20, int(c.Epoch))
}
