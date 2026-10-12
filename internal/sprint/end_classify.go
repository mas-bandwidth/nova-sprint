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
