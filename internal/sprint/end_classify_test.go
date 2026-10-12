package sprint

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

func TestEndClassifyExactStoredKinds(t *testing.T) {
	tests := []struct {
		end string
		want EndClass
		work bool
	}{
		{"ok", EndClassOK, true},
		{"failed", EndClassFailedWork, true},
		{cardhdr.EndNoResult, EndClassNoResult, true},
		{cardhdr.EndProvider, EndClassProvider, false},
		{EndNeedsUnlanded, EndClassCoordination, false},
		{laneCapEnd, EndClassCoordination, false},
		{"the coordinator moved it", EndClassUnknown, false},
		{"134 exit", EndClassUnknown, false},
		{"harness fault", EndClassUnknown, false},
	}
	for _, tt := range tests {
		if got := ClassifyEnd(tt.end); got != tt.want || IsWorkEnd(got) != tt.work {
			t.Errorf("end=%q: class=%q work=%t; want %q work=%t", tt.end, got, IsWorkEnd(got), tt.want, tt.work)
		}
	}
}
