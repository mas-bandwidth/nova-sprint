package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEndClassReasonTable pins criterion (1): every attempt end reason is classed
// as the WORKER's or as COORDINATION, with a coordination cause, and unknown is
// coordination until classed (docs/SPEC-SPRINT.md, "Coordination failures"; the
// owner, 2026-10-11: "failures not attributable to the model, are supposed to
// accumulate to a counter for coordination failures"). Keep every line: each one
// is a failure the owner named as coordinator blame, not the model's work.
func TestEndClassReasonTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		reason string
		class  EndClass
		cause  EndCause
	}{
		// COORDINATION: the machine, bench, harness, provider or seat.
		{"harness crash before first turn", "harness exit 1 within 3s: no RESULT (crash before first turn)", CoordinationEnd, CauseHarnessCrash},
		{"provider usage limit", "provider refused: weekly usage limit reached", CoordinationEnd, CauseProviderLimit},
		{"content-filter refusal", "provider content-filter refusal: response blocked", CoordinationEnd, CauseProviderRefusal},
		{"key refusal", "provider key refused: invalid api key", CoordinationEnd, CauseProviderRefusal},
		{"idle reap with no run", "idle-reap with no run (ran=false, idle=0s)", CoordinationEnd, CauseIdleReap},
		{"tlc owed no jar", "TLC owed: no jar on the bench", CoordinationEnd, CauseMissingTool},
		{"no rustc on PATH", "no rustc on PATH", CoordinationEnd, CauseMissingTool},
		{"staging refused", "staging refused: no bench mirror for org/repo", CoordinationEnd, CauseStagingRefused},
		{"tick takes a card back", "the tick took the card back: no beat carried her live set", CoordinationEnd, CauseCardTakenBack},
		{"hold takes a card back", "the hold took the card back", CoordinationEnd, CauseCardTakenBack},
		{"card dealt with unlanded needs", "card dealt to a friend before their needs landed", CoordinationEnd, CauseUndealtNeeds},
		{"hook-gate refusal", "hook-gate refusal: the seat has no proved hook", CoordinationEnd, CauseHookGateRefusal},
		{"server or store fault", "store fault: the write was refused", CoordinationEnd, CauseServerStoreFault},
		{"landing conflict", "landing refused: conflict with another card's landing", CoordinationEnd, CauseLandingConflict},
		{"unknown is coordination", "an end nobody has classed yet", CoordinationEnd, CauseUnknown},

		// WORKER: the model's own work.
		{"not-done verdict with a pushed head", "not-done verdict with a pushed head", WorkerEnd, CauseNotDoneVerdict},
		{"gate refusal on the worker's own diff", "gate refusal on the worker's own diff", WorkerEnd, CauseGateOwnDiff},
		{"broken read verdict on its head", "broken read verdict on its head", WorkerEnd, CauseBrokenReadHead},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyEnd(c.reason)
			require.Equal(t, c.class, got.Class, "reason %q", c.reason)
			if c.class == CoordinationEnd {
				require.Equal(t, c.cause, got.Cause, "reason %q", c.reason)
			}
		})
	}
}
