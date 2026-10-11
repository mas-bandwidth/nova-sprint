package sprint

import (
	"regexp"
	"strings"
)

// Every attempt end is classed as the worker's (the model's own work) or as coordination
// (everything the machine, the bench, the harness, the provider or the seat caused).
// Coordination ends never count against a worker, never consume an attempt toward the
// bound, and accumulate on a coordination-failure counter by cause. Unknown is
// coordination until classed: never blame by default (docs/SPEC-SPRINT.md, "A
// coordination failure is never the workers").

// EndClass is the class of an attempt's end.
type EndClass string

// The two classes an end can be.
const (
	// EndWorker is the model's own work: a not-done verdict on its own work, its push
	// refused by the gate for its own code, a broken read of its head.
	EndWorker EndClass = "worker"
	// EndCoordination is everything the machine, the bench, the harness, the provider or
	// the seat caused: never the worker's failure.
	EndCoordination EndClass = "coordination"
)

// The coordination causes the counter counts (docs/SPEC-SPRINT.md, the
// coordination-failure counter): each is a thing the seat must fix, never the worker.
const (
	CauseHarnessCrash    = "harness exit before the first turn"
	CauseLimit           = "provider or account limit"
	CauseFilter          = "provider content-filter or key refusal"
	CauseIdleReap        = "idle-reap with no run"
	CauseMissingTool     = "missing bench tool or jar"
	CauseStaging         = "staging refused"
	CauseTakenBack       = "the tick or a hold took the card back"
	CauseUnlandedNeeds   = "a card dealt with unlanded needs"
	CauseHookGate        = "a hook-gate refusal"
	CauseStoreFault      = "a server or store fault"
	CauseLandingConflict = "a landing refused for a conflict"
	CauseUnknown         = "unknown"
)

// workerEndRe is each positive signal that an end is the worker's own work. A worker end
// carries no coordination cause.
var workerEndRe = []*regexp.Regexp{
	// a not-done verdict with a pushed head: the work exists, so its judgment is the
	// worker's. "push refused" (no pushed head) is not this.
	regexp.MustCompile(`\bpushed=`),
	// a gate refusal on the worker's own diff, never the hook's (a hook-gate refusal is
	// coordination: CauseHookGate). The '-' after 'a-z' keeps the hyphen of "hook-gate" out.
	regexp.MustCompile(`(?:^|[^a-z-])gate\b[^;\n]{0,48}refus`),
	// a broken read verdict on its head.
	regexp.MustCompile(`\bverdict:?\s*broken\b`),
}

// coordinationEndRe is each coordination cause and the words that say it, matched in order:
// the first whose pattern matches names the cause.
var coordinationEndRe = []struct {
	cause string
	re    *regexp.Regexp
}{
	{CauseHarnessCrash, regexp.MustCompile(`(?i)\bno result\b|\bno RESULT\.md\b|wrote no report|\bthe lane died\b|lane ended with no report`)},
	{CauseLimit, regexp.MustCompile(`(?i)rate[- ]limit|usage limit|out[- ]of[- ]credit|weekly usage`)},
	{CauseFilter, regexp.MustCompile(`(?i)content[- ]filter|key refused|invalid api key|authentication failed|no[- ]credential`)},
	{CauseIdleReap, regexp.MustCompile(`(?i)idle[- ]reap|ran=false|idle=0s`)},
	{CauseMissingTool, regexp.MustCompile(`(?i)\bTLC owed\b|no jar|no rustc on PATH|on no PATH entry|command not found|missing tool`)},
	{CauseStaging, regexp.MustCompile(`(?i)staging refused|refused at staging|no bench mirror`)},
	{CauseTakenBack, regexp.MustCompile(`(?i)taken back|took the card back|no beat carried`)},
	{CauseUnlandedNeeds, regexp.MustCompile(`(?i)before (?:its|their) needs landed|needs (?:have )?not landed|unlanded needs`)},
	{CauseHookGate, regexp.MustCompile(`(?i)hook[- ]gate`)},
	{CauseStoreFault, regexp.MustCompile(`(?i)\bstore fault\b|server fault|redis.*(?:stopped|down|refused)|store.*(?:stopped|down|fault)`)},
	{CauseLandingConflict, regexp.MustCompile(`(?i)landing refused|landing conflict|conflict with another card`)},
}

// ClassifyEnd is the class of an attempt's end, and its coordination cause when the end is
// coordination: the worker's own work (EndWorker, cause ""), a named coordination cause, or
// coordination with an unknown cause. Unknown is coordination until classed: a reason this
// function does not recognise is never blamed on the worker by default.
func ClassifyEnd(line string) (EndClass, string) {
	l := strings.ToLower(strings.TrimSpace(line))
	for _, re := range workerEndRe {
		if re.MatchString(l) {
			return EndWorker, ""
		}
	}
	for _, c := range coordinationEndRe {
		if c.re.MatchString(l) {
			return EndCoordination, c.cause
		}
	}
	return EndCoordination, CauseUnknown
}
