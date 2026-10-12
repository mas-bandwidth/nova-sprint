package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

func TestEndClassify(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		want     EndClass
		wantWork bool
	}{
		// Work classes
		{"ok", "ok", EndClassOK, true},
		{"finished", "finished", EndClassFailedWork, true},
		{"review", "review", EndClassFailedWork, true},
		{"no result", cardhdr.EndNoResult + "reason", EndClassNoResult, true},
		{"staging", cardhdr.EndStaging + "reason", EndClassCoordination, false},
		{"launch", cardhdr.EndLaunch + "reason", EndClassCoordination, false},
		{"pre-existing", cardhdr.EndPreExisting + "reason", EndClassCoordination, false},

		// Not-its-fault: provider
		{"provider failure", cardhdr.EndProvider + "failed", EndClassProvider, false},
		{"missing key", cardhdr.EndProvider + "missing key", EndClassProvider, false},

		// Not-its-fault: coordination
		{"move", "move mid-run", EndClassCoordination, false},
		{"drop", "drop mid-run", EndClassCoordination, false},
		{"clear", "clear", EndClassCoordination, false},

		// Not-its-fault: environment
		{"native refused", "native refused", EndClassEnvironment, false},
		{"134 exit", "134 exit", EndClassEnvironment, false},
		{"137 exit", "137 exit", EndClassEnvironment, false},

		// Not-its-fault: harness
		{"harness fault", "harness fault", EndClassHarness, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyEnd(tt.kind)
			if got != tt.want {
				t.Errorf("ClassifyEnd(%q) = %v, want %v", tt.kind, got, tt.want)
			}
			if IsWorkEnd(got) != tt.wantWork {
				t.Errorf("IsWorkEnd(%v) = %v, want %v", got, IsWorkEnd(got), tt.wantWork)
			}
		})
	}
}

func TestNotItsFaultEnds(t *testing.T) {
	ends := NotItsFaultEnds()
	expected := []string{cardhdr.EndProvider, cardhdr.EndStaging, cardhdr.EndLaunch, cardhdr.EndPreExisting}
	if len(ends) != len(expected) {
		t.Errorf("NotItsFaultEnds() = %v, want %v", ends, expected)
	}
}
