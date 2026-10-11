package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The class of an attempt's end (the owner, 2026-10-11 ~20:30Z: "failures not
// attributable to the model are supposed to accumulate to a counter for coordination
// failures"; "my friends and fleet machines get blamed for coordinator fuckups").
// Every end is the WORKER's (the model's work) or COORDINATION's (everything the
// machine, the bench, the harness, the provider or the seat caused). A coordination
// end never counts against a worker. Unknown is COORDINATION until classed: never
// blame by default.
func TestEndClassIsWorkerOrCoordination(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		why   string
		class string
		cause string
	}{
		// the worker's own work
		{"a not-done verdict with a pushed head", "verdict not-done; pushed=abc1234 to sprint/x.w1: tests red in internal/sprint/foo.go:12", EndWorker, ""},
		{"a gate refusal on the worker's own diff", "gate refused: the card's own diff is red: internal/sprint/foo.go:12", EndWorker, ""},
		{"a broken read verdict on its head", "verdict broken; the reader read the head broken: internal/sprint/foo.go:12", EndWorker, ""},

		// coordination: the machine, the bench, the harness, the provider, the seat
		{"a harness exit within seconds, no RESULT, crash before the first turn", "no result: the harness exited after 2s, crash before the first turn", EndCoordination, CauseHarnessCrash},
		{"a provider or account limit", "provider failure: provider: class=rate-limited status=429 msg=usage limit reached", EndCoordination, CauseProviderLimit},
		{"an account limit in the provider's words", "the weekly usage limit for this account is reached", EndCoordination, CauseProviderLimit},
		{"a provider content-filter refusal", "provider failure: provider: class=content-filter status=400 msg=blocked by content filter", EndCoordination, CauseContentFilter},
		{"a provider key refusal", "provider failure: provider: class=key status=401 msg=invalid api key", EndCoordination, CauseContentFilter},
		{"an idle-reap with no run", "idle-reap: the take was reaped with no run (ran=false, idle=0s)", EndCoordination, CauseIdleReap},
		{"a missing bench tool", "no rustc on this bench's PATH", EndCoordination, CauseMissingBench},
		{"a missing bench jar", "TLC owed: no jar on this bench", EndCoordination, CauseMissingBench},
		{"staging refused, no bench mirror", "staging refused: head abc is in neither the mirror nor origin", EndCoordination, CauseStagingRefused},
		{"the tick taking a card back", "taken back by the coordinator: the tick reclaimed the card", EndCoordination, CauseTickReclaim},
		{"a hold taking a card back", "taken back by the hold of friend amy", EndCoordination, CauseHoldReclaim},
		{"a card dealt with unlanded needs", "dealt with unlanded needs: a-take-refuses-a-card-whose-needs-have-not-landed", EndCoordination, CauseUnlandedNeeds},
		{"a hook-gate refusal", "hook gate refused: the coordinator's gate said no", EndCoordination, CauseHookGate},
		{"a server or store fault", "store fault: the store refused the read", EndCoordination, CauseStoreFault},
		{"a server fault", "server fault: the server is down", EndCoordination, CauseStoreFault},
		{"a landing refused for a conflict with another card's landing", "landing refused: conflict with another card's landing", EndCoordination, CauseLandingConflict},

		// unknown is the coordination's until classed
		{"an empty reason", "", EndCoordination, ""},
		{"an unclassed reason", "the machine said something no one has classed yet", EndCoordination, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			class, cause := EndClass(tc.why)
			assert.Equal(t, tc.class, class, "class of %q", tc.why)
			assert.Equal(t, tc.cause, cause, "cause of %q", tc.why)
		})
	}
}

// A coordination end is never the worker's and never the same failure as the worker's:
// it accumulates on the coordination counter by cause and never toward an attempt bound.
func TestACoordinationEndNeverCountsAgainstTheWorker(t *testing.T) {
	t.Parallel()
	for _, why := range []string{
		"no result: the harness exited after 2s, crash before the first turn",
		"provider failure: provider: class=rate-limited status=429 msg=usage limit reached",
		"idle-reap: the take was reaped with no run (ran=false, idle=0s)",
		"staging refused: head abc is in neither the mirror nor origin",
		"landing refused: conflict with another card's landing",
	} {
		class, cause := EndClass(why)
		assert.Equal(t, EndCoordination, class, why)
		assert.NotEmpty(t, cause, "a coordination end names its cause: %s", why)
		assert.False(t, EndCountsAgainstWorker(why), "never the worker's: %s", why)
	}
	for _, why := range []string{
		"verdict not-done; pushed=abc1234 to sprint/x.w1: tests red in foo.go:12",
		"verdict broken; the reader read the head broken",
	} {
		assert.True(t, EndCountsAgainstWorker(why), "the worker's: %s", why)
	}
}
