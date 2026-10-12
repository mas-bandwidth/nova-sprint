package sprint

import (
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// EndClass is how a take ended, by cause, read from the event that ended the run
// (the finish kind, the move, the drop, the refusal, the clear), never guessed from
// text. Work ends (the child ran to its own end) count toward ok%; every other end
// counts in its own named column and never in ok%.
type EndClass string

// The work classes: the child ran to its own end, and the end counts toward ok%
// (ok over ok + failed work + no result).
const (
	EndClassOK         EndClass = "ok"
	EndClassFailedWork EndClass = "failed_work"
	EndClassNoResult   EndClass = "no_result"
)

// The not-its-fault classes: the end did not count toward ok%.
const (
	EndClassProvider     EndClass = "provider"
	EndClassCoordination EndClass = "coordination"
	EndClassEnvironment  EndClass = "environment"
	EndClassHarness      EndClass = "harness"
	EndClassClear        EndClass = "clear"
)

// harnessEndWord begins an end a harness fault recorded. A harness fault is a failed
// report the harness caused (HarnessFault: "lane died", an empty run, "push refused",
// "no head", a "cost line", ...). It is not recorded as a distinct consumer end today:
// the finish records it as failed work, and the failed rule (ruleHarness, harness_fault.go)
// reworks the card on its tier, so it never lands in the failed cell and is never counted
// in ok%. The word here keeps ClassifyEnd complete over the class, should an end be
// recorded for it.
const harnessEndWord = "harness fault"

// clearEndWord begins an end a sprint clear recorded. A clear (cmdClear, teardown) wipes
// the store, so no take end is recorded for it; the word here keeps ClassifyEnd complete
// over the class and files it in the coordination column (EndClassColumn), as a card
// cleared mid-run by the coordinator is counted.
const clearEndWord = "clear"

// ClassifyEnd classifies a take's recorded end (the consumer's End, cost.go) from the
// real word the step recorded at the moment the run ended: the finish kind the member
// wrote ("ok", "failed", "provider failure", "no result", "staging refused"), the take
// kinds a step recorded ("capped", "needs not landed", "rate limited", "out of credit"),
// and the environment exit the finish wired with its own word ("environment exit=<n>").
// An end this table does not recognise is failed work: the child ran and came back wrong.
func ClassifyEnd(end string) EndClass {
	switch {
	case end == "ok":
		return EndClassOK
	case end == "failed":
		return EndClassFailedWork
	case strings.HasPrefix(end, envExitWord): // "environment exit=<n>", a 134/137 death
		return EndClassEnvironment
	case strings.HasPrefix(end, cardhdr.EndNoResult): // "no result", and "no result environment exit=<n>"
		return EndClassNoResult
	case strings.HasPrefix(end, cardhdr.EndProvider): // "provider failure"
		return EndClassProvider
	case end == "rate limited", end == "out of credit":
		return EndClassProvider
	case strings.HasPrefix(end, cardhdr.EndStaging),
		strings.HasPrefix(end, cardhdr.EndLaunch),
		strings.HasPrefix(end, cardhdr.EndPreExisting):
		return EndClassCoordination
	case end == laneCapEnd, end == EndNeedsUnlanded: // "capped", "needs not landed"
		return EndClassCoordination
	case strings.HasPrefix(end, harnessEndWord): // "harness fault: <class>"
		return EndClassHarness
	case strings.HasPrefix(end, clearEndWord): // "clear"
		return EndClassClear
	case strings.HasPrefix(end, cardhdr.EndNothing), strings.HasPrefix(end, cardhdr.EndNoCommit):
		return EndClassFailedWork
	default:
		return EndClassFailedWork
	}
}

// IsWorkEnd reports whether the end class counts toward ok%: ok, failed work and no
// result are the work ends, whatever the end was.
func IsWorkEnd(c EndClass) bool {
	switch c {
	case EndClassOK, EndClassFailedWork, EndClassNoResult:
		return true
	default:
		return false
	}
}

// EndClassColumn is the fleet column a non-work end class files in (schema.go): the
// named column beside ok%, never the failed cell. A work end has none ("").
func EndClassColumn(c EndClass) string {
	switch c {
	case EndClassProvider:
		return ProviderFailures
	case EndClassEnvironment:
		return Environment
	case EndClassHarness:
		return Harness
	case EndClassClear, EndClassCoordination:
		return Coordination
	default:
		return ""
	}
}

// EndedCol says a work card in the column sits off the active table: withdrawn, or in one
// of the end-class columns beside ok% (schema.go). The drop, the defer, the edit and the
// fleet's keep treat a card in one of these exactly as a withdrawn card, whatever its next
// move (a redeal or an escalation): the take that ended there is counted in its column.
func EndedCol(col string) bool { return col == Withdrawn || NotItsFaultCol(col) }

// NotItsFaultCol says a work card in the column ended not-its-fault (provider,
// coordination, environment, harness) or with no result: the named end-class columns
// beside ok% (schema.go). The deal redeals a card in one of them exactly as it redeals a
// withdrawn card, so the take that ended there is counted in its column and the card is
// still dealt again.
func NotItsFaultCol(col string) bool {
	switch col {
	case ProviderFailures, Coordination, Environment, Harness, NoResult:
		return true
	}
	return false
}

// Redealable says a work card in the column is dealt again (redealt) at a new generation,
// its attempt unchanged, as a withdrawn card is: Withdrawn, or a take that ended
// not-its-fault (provider, no result, environment, harness), or a staging refusal
// (coordination with a staging take). A lane-cap re-tier files in coordination too but
// escalates to a new attempt, never redealt: it has no staging take.
func Redealable(wc *Card) bool {
	if wc == nil {
		return false
	}
	if wc.Col == Withdrawn {
		return true
	}
	switch wc.Col {
	case ProviderFailures, NoResult, Environment, Harness:
		return true
	case Coordination:
		_, gens := StagingTakes(wc)
		return len(gens) > 0
	}
	return false
}
