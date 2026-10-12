package sprint

import "github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"

// EndClass is the cause recorded with a work take. Only the first three
// classes describe work the child was able to complete on its own terms.
type EndClass string

const (
	EndClassOK           EndClass = "ok"
	EndClassFailedWork   EndClass = "failed_work"
	EndClassNoResult     EndClass = "no_result"
	EndClassCoordination EndClass = "coordination"
	EndClassProvider     EndClass = "provider"
	EndClassEnvironment  EndClass = "environment"
	EndClassHarness      EndClass = "harness"
	EndClassClear        EndClass = "clear"
	EndClassUnknown      EndClass = "unknown"
)

// ClassifyEnd translates only the exact end kinds written by the sprint's
// producers. A historical free-text end cannot establish who caused it.
func ClassifyEnd(kind string) EndClass {
	switch kind {
	case "ok":
		return EndClassOK
	case "failed":
		return EndClassFailedWork
	case cardhdr.EndNoResult:
		return EndClassNoResult
	case cardhdr.EndProvider:
		return EndClassProvider
	case EndNeedsUnlanded, laneCapEnd:
		return EndClassCoordination
	default:
		return EndClassUnknown
	}
}

// IsWorkEnd says whether a classified end belongs in the ok% denominator.
func IsWorkEnd(c EndClass) bool {
	return c == EndClassOK || c == EndClassFailedWork || c == EndClassNoResult
}
