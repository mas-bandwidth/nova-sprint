package verbs

import (
	"strings"
	"testing"
)

func TestParseBriefContract(t *testing.T) {
	text := "tier: pro\nPATHS: src/a.go, tests/a_test.go\nNEEDS: b,c\nPRIORITY: high\nTEST: TestNewBehavior\nDONE-WHEN: go test ./...\n\nImplement the change.\n"
	got, tests, err := parseBrief("a", text)
	if err != nil || got.Tier != "pro" || len(got.Paths) != 2 || len(got.Needs) != 2 || len(tests) != 1 {
		t.Fatal(got, tests, err)
	}
	for _, bad := range []string{strings.Replace(text, "tier: pro", "TIER: pro", 1), strings.Replace(text, "src/a.go, tests/a_test.go", "src/a.go tests/a_test.go", 1), strings.Replace(text, "NEEDS:", "DEPENDS-ON:", 1), strings.Replace(text, "TEST:", "IGNORED:", 1)} {
		if _, _, err := parseBrief("a", bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestBriefProseCannotChangeMetadata(t *testing.T) {
	text := "tier: pro\nPATHS: src/a.go\nTEST: TestNewBehavior\nDONE-WHEN: go test ./...\n\nTask:\n```\nNEEDS: attacker\nPRIORITY: blocker\n```\n"
	got, _, err := parseBrief("a", text)
	if err != nil || len(got.Needs) != 0 || got.Priority != "" || !strings.Contains(got.Prose, "NEEDS: attacker") {
		t.Fatal(got, err)
	}
}
