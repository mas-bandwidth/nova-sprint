package sprint

import (
	"regexp"
	"strings"
)

// The end class of an attempt (the owner, 2026-10-11 ~20:30Z: "failures not
// attributable to the model are supposed to accumulate to a counter for coordination
// failures"; "I'm sick of seeing friends and fleet machines get blamed for coordinator
// fuckups"). Every end is the WORKER's -- the model's own work: a not-done verdict on
// its work, its push refused by the gate for its own code, a broken read of its head --
// or COORDINATION's, everything the machine, the bench, the harness, the provider or
// the seat caused. Rule: a coordination end never counts against a worker, never
// consumes an attempt toward its card's bound, and accumulates on the
// coordination-failure counter by cause; unknown is COORDINATION until classed, so a
// worker is never blamed by default (docs/SPEC-SPRINT.md section 1).
const (
	// EndWorker is the class of an end that is the model's own work.
	EndWorker = "worker"
	// EndCoordination is the class of an end the machine, the bench, the harness, the
	// provider or the seat caused.
	EndCoordination = "coordination"
)

// The causes a coordination end accumulates on: the classes of end_class_test.go.
const (
	CauseHarnessCrash    = "harness crash"
	CauseProviderLimit   = "provider limit"
	CauseContentFilter   = "content filter"
	CauseIdleReap        = "idle reap"
	CauseMissingBench    = "missing bench"
	CauseStagingRefused  = "staging refused"
	CauseTickReclaim     = "tick reclaim"
	CauseHoldReclaim     = "hold reclaim"
	CauseUnlandedNeeds   = "unlanded needs"
	CauseHookGate        = "hook gate"
	CauseStoreFault      = "store fault"
	CauseLandingConflict = "landing conflict"
)

// The words each coordination cause is matched by, in the order a reason is read.
var (
	reHookGate     = regexp.MustCompile(`(?i)\bhook[- ]gate\b|hook gate refused`)
	reCrashNoTurn  = regexp.MustCompile(`(?i)crash before the first turn|harness exit(ed)?( after| within| in) [0-9]`)
	reContentKey   = regexp.MustCompile(`(?i)content[- ]filter|content_filter|class=key\b|invalid api key|key (refusal|refused)`)
	reLimitWords   = regexp.MustCompile(`(?i)\b(usage|account|weekly|monthly) limit\b|out[- ]of[- ]credit|out of credits|quota\b`)
	reIdleReap     = regexp.MustCompile(`(?i)\bidle[- ]reap\b|ran=false`)
	reMissingBench = regexp.MustCompile(`(?i)\bno rustc\b|\bno jar\b|\bTLC owed\b|missing (tool|binary|jar)|\bno such (tool|jar)\b|no [A-Za-z0-9_.+-]+ on (this bench's )?PATH`)
	reStoreFault   = regexp.MustCompile(`(?i)\b(store|server) fault\b`)
	reLandConflict = regexp.MustCompile(`(?i)landing refused.*conflict|conflict with another card'?s landing`)
)

// EndClass classifies an attempt's end reason as the WORKER's (EndWorker) or
// COORDINATION's (EndCoordination) and, for a coordination end, its cause (one of the
// Cause constants or "" when no cause is recognised: unknown is still coordination,
// never the worker). A not-done verdict on the work, a gate refusal on the card's own
// diff and a broken read verdict on its head are the worker's; every other end the
// card names -- a harness crash before the first turn, a provider or account limit, a
// content-filter or key refusal, an idle-reap with no run, a missing bench tool or
// jar, a staging refusal, the tick or a hold taking a card back, a card dealt with
// unlanded needs, a hook-gate refusal, a store fault, a landing conflict -- is a
// coordination end.
func EndClass(reason string) (class, cause string) {
	r := strings.TrimSpace(reason)
	if r == "" {
		return EndCoordination, ""
	}
	// a hook-gate refusal is the coordinator's, read before any "gate refused" below
	if reHookGate.MatchString(r) {
		return EndCoordination, CauseHookGate
	}
	// the worker's own work
	if strings.HasPrefix(r, "verdict ") || strings.Contains(r, "verdict broken") || strings.Contains(r, "pushed=") {
		return EndWorker, ""
	}
	if strings.Contains(r, "gate refused") {
		return EndWorker, ""
	}
	// coordination, by cause
	switch {
	case reContentKey.MatchString(r):
		return EndCoordination, CauseContentFilter
	case reLimitWords.MatchString(r) || IsRateLimited(r) || IsProviderLimit(r):
		return EndCoordination, CauseProviderLimit
	case reCrashNoTurn.MatchString(r):
		return EndCoordination, CauseHarnessCrash
	case reIdleReap.MatchString(r):
		return EndCoordination, CauseIdleReap
	case reMissingBench.MatchString(r):
		return EndCoordination, CauseMissingBench
	case IsStagingRefusal(r) || strings.Contains(r, "staging refused"):
		return EndCoordination, CauseStagingRefused
	case strings.Contains(r, "taken back by the coordinator") || strings.Contains(r, "tick reclaimed"):
		return EndCoordination, CauseTickReclaim
	case strings.Contains(r, "taken back by the hold"):
		return EndCoordination, CauseHoldReclaim
	case strings.Contains(r, "unlanded needs"):
		return EndCoordination, CauseUnlandedNeeds
	case reStoreFault.MatchString(r):
		return EndCoordination, CauseStoreFault
	case reLandConflict.MatchString(r):
		return EndCoordination, CauseLandingConflict
	}
	// unknown is the coordination's until classed: never blame the worker by default
	return EndCoordination, ""
}

// EndCountsAgainstWorker says an end reason counts against the worker: it is the
// model's own work (EndClass). A coordination end never counts, never consumes an
// attempt toward the bound and accumulates on the coordination counter.
func EndCountsAgainstWorker(reason string) bool {
	class, _ := EndClass(reason)
	return class == EndWorker
}

// IsProviderLimit says a reason names a provider or account limit: a usage, account,
// weekly or monthly limit, out of credit, or a quota (EndClass's CauseProviderLimit).
func IsProviderLimit(reason string) bool {
	return reLimitWords.MatchString(reason)
}
