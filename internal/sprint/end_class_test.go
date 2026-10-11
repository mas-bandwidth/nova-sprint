package sprint

import "testing"

// Every attempt end is classed as the worker's (the model's own work: a not-done verdict
// on its own work, its push refused by the gate for its own code, a broken read of its
// head) or as coordination (everything the machine, the bench, the harness, the provider
// or the seat caused). Coordination ends never count against a worker, never consume an
// attempt toward the bound, and accumulate on a coordination-failure counter by cause.
// Unknown is coordination until classed: never blame by default.
func TestClassifyEnd(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		class  EndClass
		cause  string
	}{
		// coordination
		{"harness exit within seconds with no result", "no result: no RESULT.md shape", EndCoordination, CauseHarnessCrash},
		{"a lane died writing no report", "verdict not-done; the lane died: the runner ended job s1-1.w1 with no report", EndCoordination, CauseHarnessCrash},
		{"a provider or account limit", "provider failure: class=rate-limit status=429 msg=usage limit reached", EndCoordination, CauseLimit},
		{"out of credit", "provider failure: class=out-of-credit status=402 msg=account at its weekly usage limit", EndCoordination, CauseLimit},
		{"a content filter", "provider failure: class=content-filter status=400 msg=content filtered", EndCoordination, CauseFilter},
		{"a key refusal", "provider failure: class=auth status=401 msg=key refused: invalid api key", EndCoordination, CauseFilter},
		{"idle-reap with no run", "idle-reap: ran=false idle=0s", EndCoordination, CauseIdleReap},
		{"a missing bench tool", "no rustc on PATH", EndCoordination, CauseMissingTool},
		{"a missing bench jar", "TLC owed: no jar on this bench", EndCoordination, CauseMissingTool},
		{"staging refused for no mirror", "staging refused: no bench mirror; no child ran", EndCoordination, CauseStaging},
		{"the tick took a card back", "taken back by the tick: no beat carried her live set", EndCoordination, CauseTakenBack},
		{"a hold took a card back", "the hold took the card back", EndCoordination, CauseTakenBack},
		{"a card dealt with unlanded needs", "dealt to friend.amy before its needs landed", EndCoordination, CauseUnlandedNeeds},
		{"a hook-gate refusal", "hook-gate refused: the coordinator's hook is not proven", EndCoordination, CauseHookGate},
		{"a server or store fault", "store fault: the redis server stopped answering", EndCoordination, CauseStoreFault},
		{"a landing conflict", "landing refused: conflict with another card's landing", EndCoordination, CauseLandingConflict},
		// worker
		{"a not-done verdict with a pushed head", "verdict not-done; pushed=abc123 to sprint/foo: tests red in internal/a", EndWorker, ""},
		{"a gate refusal on the worker's own diff", "gate refused: the worker's own diff fails its tests", EndWorker, ""},
		{"a broken read verdict on its head", "verdict broken: internal/a.go:12 fails", EndWorker, ""},
		// unknown is coordination
		{"anything unknown is coordination", "something we have never seen before", EndCoordination, CauseUnknown},
		{"an empty line is coordination", "", EndCoordination, CauseUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			class, cause := ClassifyEnd(tc.reason)
			if class != tc.class {
				t.Errorf("ClassifyEnd(%q) class = %q, want %q", tc.reason, class, tc.class)
			}
			if cause != tc.cause {
				t.Errorf("ClassifyEnd(%q) cause = %q, want %q", tc.reason, cause, tc.cause)
			}
		})
	}
}
