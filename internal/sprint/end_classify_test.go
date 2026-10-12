package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// ClassifyEnd reads the end the store actually records (a work take's consumer end,
// cost.go): the finish kind, the take kind, the cap, the needs stop, the rate limit,
// the environment exit. Work ends count toward ok%; every other end counts in its own
// named column and never in ok%.
func TestEndClassify(t *testing.T) {
	tests := []struct {
		name string
		end  string
		want EndClass
		work bool
	}{
		// the work ends: the child ran to its own end
		{"ok", "ok", EndClassOK, true},
		{"failed work", "failed", EndClassFailedWork, true},
		{"no result", cardhdr.EndNoResult + ": no RESULT.md shape", EndClassNoResult, true},
		{"no result at an environment exit", cardhdr.EndNoResult + " environment exit=1", EndClassNoResult, true},
		{"nothing to do", cardhdr.EndNothing + ": nothing", EndClassFailedWork, true},
		{"no commit", cardhdr.EndNoCommit + ": no commit", EndClassFailedWork, true},

		// not-its-fault: provider
		{"provider failure", cardhdr.EndProvider + ": provider: class=provider-5xx", EndClassProvider, false},
		{"rate limited", "rate limited", EndClassProvider, false},
		{"out of credit", "out of credit", EndClassProvider, false},

		// not-its-fault: coordination
		{"staging refused", cardhdr.EndStaging + ": no bench mirror", EndClassCoordination, false},
		{"launch refused", cardhdr.EndLaunch + ": no model", EndClassCoordination, false},
		{"pre-existing", cardhdr.EndPreExisting + ": internal/x.go:1", EndClassCoordination, false},
		{"capped re-tier", laneCapEnd, EndClassCoordination, false},
		{"needs not landed", EndNeedsUnlanded, EndClassCoordination, false},

		// not-its-fault: environment (a 134/137 environment death)
		{"environment exit 134", EnvironmentExit(134), EndClassEnvironment, false},
		{"environment exit 137", EnvironmentExit(137), EndClassEnvironment, false},

		// an unrecognised end is failed work, never silently ignored
		{"unrecognised", "some other end", EndClassFailedWork, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyEnd(tt.end)
			if got != tt.want {
				t.Errorf("ClassifyEnd(%q) = %v, want %v", tt.end, got, tt.want)
			}
			if IsWorkEnd(got) != tt.work {
				t.Errorf("IsWorkEnd(%v) = %v, want %v", got, IsWorkEnd(got), tt.work)
			}
		})
	}
}

// The not-its-fault classes each map to their own named fleet column, never to the
// failed cell, so ok% never counts them.
func TestEndClassColumn(t *testing.T) {
	tests := []struct {
		class EndClass
		col   string
	}{
		{EndClassProvider, ProviderFailures},
		{EndClassCoordination, Coordination},
		{EndClassEnvironment, Environment},
		{EndClassHarness, Harness},
		{EndClassClear, Coordination},
		{EndClassOK, ""}, // work ends have no named not-its-fault column
		{EndClassFailedWork, ""},
		{EndClassNoResult, ""},
	}
	for _, tt := range tests {
		if got := EndClassColumn(tt.class); got != tt.col {
			t.Errorf("EndClassColumn(%v) = %q, want %q", tt.class, got, tt.col)
		}
	}
}
