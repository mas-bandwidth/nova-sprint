package sprint

import (
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// EndClass classifies how a take ended: work (the child ran to its own end)
// vs not-its-fault (coordination, provider, environment, harness, clear).
type EndClass string

const (
	// Work classes: the child ran to its own end, and the end counts toward ok%.
	EndClassWork       EndClass = "work"       // ok, failed work, no result
	EndClassOK         EndClass = "ok"
	EndClassFailedWork EndClass = "failed_work"
	EndClassNoResult   EndClass = "no_result"

	// Not-its-fault classes: the end did not count toward ok%.
	EndClassCoordination EndClass = "coordination" // moved, dropped, re-briefed, re-tiered, sprint clear
	EndClassProvider     EndClass = "provider"     // provider failure, missing key
	EndClassEnvironment  EndClass = "environment"  // 134/137 environment death, native refused
	EndClassHarness      EndClass = "harness"      // harness fault
)

// ClassifyEnd returns the end class based on the finish kind (from the event that ended the run).
func ClassifyEnd(kind string) EndClass {
	switch {
	case kind == "ok":
		return EndClassOK
	case strings.HasPrefix(kind, cardhdr.EndProvider):
		return EndClassProvider
	case strings.HasPrefix(kind, cardhdr.EndNoResult):
		return EndClassNoResult
	case strings.HasPrefix(kind, cardhdr.EndStaging):
		return EndClassCoordination
	case strings.HasPrefix(kind, cardhdr.EndLaunch):
		return EndClassCoordination
	case strings.HasPrefix(kind, cardhdr.EndPreExisting):
		return EndClassCoordination
	case strings.HasPrefix(kind, "native refused"):
		return EndClassEnvironment
	case strings.HasPrefix(kind, "134"):
		return EndClassEnvironment
	case strings.HasPrefix(kind, "137"):
		return EndClassEnvironment
	case strings.HasPrefix(kind, "harness"):
		return EndClassHarness
	case strings.HasPrefix(kind, "move"):
		return EndClassCoordination
	case strings.HasPrefix(kind, "drop"):
		return EndClassCoordination
	case strings.HasPrefix(kind, "clear"):
		return EndClassCoordination
	case kind == "finished" || kind == "review":
		return EndClassFailedWork
	default:
		return EndClassWork // Default to work for unknown ends
	}
}

// IsWorkEnd returns true if the end class counts toward ok%.
func IsWorkEnd(c EndClass) bool {
	switch c {
	case EndClassOK, EndClassFailedWork, EndClassNoResult:
		return true
	default:
		return false
	}
}

// EndItsFaultEnds returns the end kinds that classify as not-its-fault.
func NotItsFaultEnds() []string {
	return []string{
		cardhdr.EndProvider,
		cardhdr.EndStaging,
		cardhdr.EndLaunch,
		cardhdr.EndPreExisting,
	}
}
