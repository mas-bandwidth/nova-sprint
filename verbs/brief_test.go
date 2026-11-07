package verbs

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/nova-sprint/store"
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

func TestBriefLintNamesLineAndRule(t *testing.T) {
	cases := []struct{ body, want string }{
		{"RESULT: do work\n", "demo: line 1 must be tier: <flash|pro|heavy|frontier>"},
		{"tier: pro\nPATHS: ok.go\nDEPENDS-ON: other\n\nwork\n", "demo: line 3 must contain a valid NEEDS section"},
		{"tier: pro\nPATHS: ../bad\n\nwork\n", "demo: line 2 must contain a valid PATHS section"},
		{"tier: pro\nPATHS: ok.go\nTEST: test\n\nwork", "demo: line 5 must contain a valid DONE-WHEN section"},
	}
	for _, tc := range cases {
		_, _, err := parseBrief("demo", tc.body)
		if err == nil || err.Error() != tc.want {
			t.Fatalf("got %v; want %s", err, tc.want)
		}
		result := operationalFailure("add", err)
		if result.Exit != 1 || !strings.Contains(result.Output, tc.want) || strings.Count(result.Output, "\n") != 1 {
			t.Fatal(result)
		}
	}
}

func TestFileBriefLintIsUsefulToDirectCaller(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deterministic-lead-code.md")
	if err := os.WriteFile(path, []byte("RESULT: task\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := (FileBriefs{}).Parse(context.Background(), path, store.Stream{})
	want := "deterministic-lead-code: line 1 must be tier: <flash|pro|heavy|frontier>"
	if err == nil || err.Error() != want {
		t.Fatal(err)
	}
}
