package fleet

import (
	"strings"
	"testing"
)

// TestChildRulesNeverRewriteHistory pins the worker brief's history rule: a child that
// amends, rebases or resets onto origin rewrites the staged commit and the finish refuses
// its head as one that does not descend from it (pkg/cardcontract/carry.go).
func TestChildRulesNeverRewriteHistory(t *testing.T) {
	t.Parallel()
	raw, err := Rules.ReadFile("child-rules.txt")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "[no-rewrite-history] Never amend, rebase or reset onto origin") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want the history rule exactly once in child-rules.txt, got %d", n)
	}
}
