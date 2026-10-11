package sprint

import "strings"

// Every attempt end is classed as the WORKER's or as COORDINATION (the owner,
// 2026-10-11: "failures not attributable to the model, are supposed to accumulate
// to a counter for coordination failures"; "I'm sick of seeing friends and fleet
// machines get blamed for coordinator fuckups"). A worker end is the model's own
// work: a not-done verdict at a pushed head, the gate refusing the worker's own
// diff, or a broken read verdict on its own head. Everything the machine, the
// bench, the harness, the provider or the seat caused is coordination: it never
// counts against a worker, never consumes an attempt toward its bound, and
// accumulates on the coordination-failure counter by cause (docs/SPEC-SPRINT.md,
// "Coordination failures"; tla/SprintRules.tla Part "attempts").
//
// Unknown is COORDINATION until classed: the sprint never blames a worker by
// default (README, "trust but VERIFY"; docs/SPEC-SPRINT.md section 1).

// EndClass is which side an attempt end belongs to.
type EndClass string

const (
	// WorkerEnd is an end the model's own work caused; it counts against the
	// worker's ok% and toward its attempt bound.
	WorkerEnd EndClass = "worker"
	// CoordinationEnd is an end the machine, bench, harness, provider or seat
	// caused; it is excluded from ok% and from the attempt bound and
	// accumulates on the coordination-failure counter.
	CoordinationEnd EndClass = "coordination"
)

// EndCause names why a coordination end happened, so the counter can total by
// cause (docs/SPEC-SPRINT.md, "Coordination failures").
type EndCause string

const (
	// CauseUnknown is any end the sprint cannot yet class; it is coordination
	// until classed, never a worker's blame (README, "trust but VERIFY").
	CauseUnknown EndCause = "unknown"

	// Coordination causes.
	CauseHarnessCrash     EndCause = "harness-crash"      // exit within seconds with no RESULT
	CauseProviderLimit    EndCause = "provider-limit"     // a provider or account usage limit
	CauseProviderRefusal  EndCause = "provider-refusal"   // a provider content filter or key refusal
	CauseIdleReap         EndCause = "idle-reap"          // reaped with no run (ran=false, idle=0s)
	CauseMissingTool      EndCause = "missing-bench-tool" // a missing bench tool or jar ("TLC owed", "no rustc on PATH")
	CauseStagingRefused   EndCause = "staging-refused"    // no bench mirror at staging
	CauseCardTakenBack    EndCause = "card-taken-back"    // the tick or a hold taking a card back
	CauseUndealtNeeds     EndCause = "unlanded-needs"     // a card dealt with unlanded needs
	CauseHookGateRefusal  EndCause = "hook-gate-refusal"  // the seat's hook gate refused the command
	CauseServerStoreFault EndCause = "server-store-fault" // a server or store fault
	CauseLandingConflict  EndCause = "landing-conflict"   // a landing refused for a conflict with another card's landing

	// Worker causes.
	CauseNotDoneVerdict EndCause = "not-done-verdict"      // a not-done verdict with a pushed head
	CauseGateOwnDiff    EndCause = "gate-refusal-own-diff" // a gate refusal on the worker's own diff
	CauseBrokenReadHead EndCause = "broken-read-head"      // a broken read verdict on its own head
)

// EndClassified is an end reason's class and, for a coordination end, its cause.
type EndClassified struct {
	Class EndClass
	Cause EndCause
}

// endRules maps the words a reason arrives with to a class and cause, most
// specific first; the first rule whose needle the reason holds decides. A worker
// rule is tried before a coordination rule and only on the model's own work, so
// a hook-gate refusal or a landing conflict is never read as the worker's own
// gate (criterion (1), docs/SPEC-SPRINT.md, "Coordination failures").
var endRules = []struct {
	needle string
	class  EndClass
	cause  EndCause
}{
	// Worker ends: the model's own work.
	{"own diff", WorkerEnd, CauseGateOwnDiff},
	{"not-done verdict", WorkerEnd, CauseNotDoneVerdict},
	{"broken read", WorkerEnd, CauseBrokenReadHead},

	// Coordination ends.
	{"crash before first turn", CoordinationEnd, CauseHarnessCrash},
	{"no result", CoordinationEnd, CauseHarnessCrash},
	{"harness exit", CoordinationEnd, CauseHarnessCrash},
	{"usage limit", CoordinationEnd, CauseProviderLimit},
	{"content filter", CoordinationEnd, CauseProviderRefusal},
	{"content-filter", CoordinationEnd, CauseProviderRefusal},
	{"key refused", CoordinationEnd, CauseProviderRefusal},
	{"key refusal", CoordinationEnd, CauseProviderRefusal},
	{"idle-reap", CoordinationEnd, CauseIdleReap},
	{"idle=0", CoordinationEnd, CauseIdleReap},
	{"ran=false", CoordinationEnd, CauseIdleReap},
	{"no jar", CoordinationEnd, CauseMissingTool},
	{"no rustc", CoordinationEnd, CauseMissingTool},
	{"tlc owed", CoordinationEnd, CauseMissingTool},
	{"missing bench", CoordinationEnd, CauseMissingTool},
	{"bench tool", CoordinationEnd, CauseMissingTool},
	{"staging refused", CoordinationEnd, CauseStagingRefused},
	{"no bench mirror", CoordinationEnd, CauseStagingRefused},
	{"took the card back", CoordinationEnd, CauseCardTakenBack},
	{"taken back", CoordinationEnd, CauseCardTakenBack},
	{"card back", CoordinationEnd, CauseCardTakenBack},
	{"needs landed", CoordinationEnd, CauseUndealtNeeds},
	{"unlanded needs", CoordinationEnd, CauseUndealtNeeds},
	{"undealt needs", CoordinationEnd, CauseUndealtNeeds},
	{"hook-gate", CoordinationEnd, CauseHookGateRefusal},
	{"hook gate", CoordinationEnd, CauseHookGateRefusal},
	{"store fault", CoordinationEnd, CauseServerStoreFault},
	{"server fault", CoordinationEnd, CauseServerStoreFault},
	{"landing conflict", CoordinationEnd, CauseLandingConflict},
	{"landing refused", CoordinationEnd, CauseLandingConflict},
	{"conflict with another", CoordinationEnd, CauseLandingConflict},
}

// ClassifyEnd classes one attempt end by the reason it arrives with. Unknown is
// COORDINATION until classed, never a worker's blame by default (criterion (1);
// README, "trust but VERIFY"; docs/SPEC-SPRINT.md, "Coordination failures").
func ClassifyEnd(reason string) EndClassified {
	low := strings.ToLower(reason)
	for _, r := range endRules {
		if strings.Contains(low, r.needle) {
			return EndClassified{Class: r.class, Cause: r.cause}
		}
	}
	return EndClassified{Class: CoordinationEnd, Cause: CauseUnknown}
}
