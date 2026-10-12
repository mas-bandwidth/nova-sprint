package sprint

import (
	"regexp"
	"strings"
)

// Every attempt end is classed as the worker's own work or as coordination: everything the
// machine, the bench, the harness, the provider or the seat caused. Coordination ends never
// count against a worker, never consume an attempt toward the bound, and accumulate on a
// coordination-failure counter by cause. Unknown is coordination until classed: never blame
// by default (the owner, 2026-10-11: "I'm sick of seeing friends and fleet machines get
// blamed for coordinator fuckups.").

// EndClass is the class of an attempt's end.
type EndClass string

const (
	// EndWorker is the model's own work: a not-done verdict on its own work, its push
	// refused by the gate for its own code, a broken read of its head.
	EndWorker EndClass = "worker"
	// EndCoordination is everything the machine, bench, harness, provider or seat caused.
	EndCoordination EndClass = "coordination"
)

// CoordinationCause is the cause of a coordination-failure end, for the counter.
type CoordinationCause string

const (
	CauseHarnessCrash   CoordinationCause = "harness crash before first turn"
	CauseProviderLimit  CoordinationCause = "provider or account limit"
	CauseContentFilter  CoordinationCause = "provider content-filter or key refusal"
	CauseIdleReap       CoordinationCause = "idle-reap with no run"
	CauseMissingTool    CoordinationCause = "missing bench tool or jar"
	CauseStagingRefused CoordinationCause = "staging refused"
	CauseTakenBack      CoordinationCause = "tick or hold took the card back"
	CauseUnlandedNeeds  CoordinationCause = "card dealt with unlanded needs"
	CauseHookGate       CoordinationCause = "hook-gate refusal"
	CauseStoreFault     CoordinationCause = "server or store fault"
	CauseLandConflict   CoordinationCause = "landing refused on a conflict"
	CauseUnknown        CoordinationCause = "unknown"
)

// coordinationCauseREs is each coordination cause and the words that say it, in the order a
// report is matched: the first cause whose pattern matches names it, so a report that says
// more than one (a lane that died on a temp-directory crash, a usage limit) classes once.
var coordinationCauseREs = []struct {
	cause CoordinationCause
	re    *regexp.Regexp
}{
	{CauseHarnessCrash, regexp.MustCompile(`(?i)no result before the first turn|crash before the first turn|harness exit.*no result|died in \d+s on a temp-directory crash`)},
	{CauseProviderLimit, regexp.MustCompile(`(?i)provider.*(limit|out.of.credit|usage limit)|account limit|weekly usage limit|rate.limit`)},
	{CauseContentFilter, regexp.MustCompile(`(?i)content.filter|key refusal|api key|a key.*refused|safety (system|filter)`)},
	{CauseIdleReap, regexp.MustCompile(`(?i)idle.*reap|reaped at idle|ran=false.*idle=0s|idle=0s with no run`)},
	{CauseMissingTool, regexp.MustCompile(`(?i)TLC owed|no rustc on PATH|no jar on the bench|missing (tool|jar)|executable.*not found`)},
	{CauseStagingRefused, regexp.MustCompile(`(?i)staging refused|no bench mirror|mirror missing|refused at staging`)},
	{CauseTakenBack, regexp.MustCompile(`(?i)took the card back|card taken back|tick.*take.?back|a hold returned the card|no beat carried.*live set`)},
	{CauseUnlandedNeeds, regexp.MustCompile(`(?i)unlanded needs|needs.*not landed|dealt before.*needs|before its needs landed`)},
	{CauseHookGate, regexp.MustCompile(`(?i)hook.gate|the hook.*refused|coordinator hook|the gate.*the hook`)},
	{CauseStoreFault, regexp.MustCompile(`(?i)store fault|server fault|internal server error|database.*(unavail|error|down)|the store errored`)},
	{CauseLandConflict, regexp.MustCompile(`(?i)landing refused.*conflict|conflict with another card|refused.*conflict|a conflict with another card's landing`)},
}

// workerEnd says an end is the model's own work: a not-done verdict over a pushed head, a
// gate refusal on the worker's own diff, or a broken read verdict on its head.
func workerEnd(verdict, report string) bool {
	low := strings.ToLower(verdict + " " + report)
	switch {
	case strings.Contains(low, "not-done") && strings.Contains(report, "pushed="):
		return true
	case strings.Contains(low, "gate") && strings.Contains(low, "diff") && strings.Contains(low, "your"):
		return true
	case strings.Contains(low, "broken read") && strings.Contains(low, "head"):
		return true
	}
	return false
}

// ClassEnd classes an attempt's end by its verdict and report: the worker's own (EndWorker)
// when it is the model's work, else coordination (EndCoordination) with its cause. Unknown
// is coordination, never the worker by default.
func ClassEnd(verdict, report string) (EndClass, CoordinationCause) {
	if workerEnd(verdict, report) {
		return EndWorker, ""
	}
	for _, c := range coordinationCauseREs {
		if c.re.MatchString(report) {
			return EndCoordination, c.cause
		}
	}
	return EndCoordination, CauseUnknown
}
