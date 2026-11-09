package verbs

import (
	"strings"
	"testing"
)

// TestBusAnswer: the sprint's pushes are receipts. A message from another
// name is logged and never answered; its own name and no sender are nothing.
func TestBusAnswer(t *testing.T) {
	t.Parallel()
	if log := BusAnswer("alice"); !strings.Contains(log, "ignored a message from alice") {
		t.Fatalf("BusAnswer(alice) = %q", log)
	}
	for _, from := range []string{"sprint", ""} {
		if log := BusAnswer(from); log != "" {
			t.Fatalf("BusAnswer(%q) = %q, want nothing", from, log)
		}
	}
}
