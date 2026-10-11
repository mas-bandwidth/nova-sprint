package swarm

import (
	"strings"
	"testing"
)

// A header key inside the header block the gate reads passes: LintHeaderStranded
// answers nothing (docs/SPEC-SPRINT.md section 2, the card header).
func TestLintHeaderStrandedAcceptsAKeyInTheHeader(t *testing.T) {
	raw := []byte("RESULT: c1 sha=000000000000 tier: flash\n" +
		"KIND: fix\n" +
		"DEPENDS-ON: -\n" +
		"PATHS: internal/x.go\n" +
		"\n" +
		"Fix c1.\n")
	if got := LintHeaderStranded(raw); len(got) != 0 {
		t.Fatalf("a key in the header drew %+v; want no finding", got)
	}
}

// A header key written below the first prose line is one finding naming the key and
// its line: the gate reads only the header block, so the key would never be read.
func TestLintHeaderStrandedNamesAKeyBelowTheProse(t *testing.T) {
	raw := []byte("RESULT: c1 sha=000000000000 tier: flash\n" +
		"KIND: fix\n" +
		"PATHS: internal/x.go\n" +
		"\n" +
		"Fix c1.\n" +
		"DEPENDS-ON: -\n")
	got := LintHeaderStranded(raw)
	if len(got) != 1 {
		t.Fatalf("a stranded DEPENDS-ON drew %+v; want one finding", got)
	}
	if got[0].Line != 6 {
		t.Fatalf("stranded DEPENDS-ON line=%d; want 6", got[0].Line)
	}
	if !strings.Contains(got[0].Excerpt, "DEPENDS-ON") {
		t.Fatalf("excerpt %q does not name the key", got[0].Excerpt)
	}
	if !strings.Contains(got[0].Excerpt, "6") {
		t.Fatalf("excerpt %q does not name the line", got[0].Excerpt)
	}
}

// A sentence that merely mentions a key mid-line is not a `KEY: value` line and draws
// no finding: only a line that starts with the key is a header key below the block.
func TestLintHeaderStrandedIgnoresAMentionMidLine(t *testing.T) {
	raw := []byte("RESULT: c1 sha=000000000000 tier: flash\n" +
		"KIND: fix\n" +
		"PATHS: internal/x.go\n" +
		"\n" +
		"Fix c1: read DEPENDS-ON: for the chain, not this sentence.\n")
	if got := LintHeaderStranded(raw); len(got) != 0 {
		t.Fatalf("a mid-line mention drew %+v; want no finding", got)
	}
}

// Every key the card header names is refused when it sits below the prose: one finding
// each, in line order, naming the key.
func TestLintHeaderStrandedNamesEveryStrandedHeaderKey(t *testing.T) {
	for _, key := range []string{"KIND", "PATHS", "TEST", "LEGS", "SOURCE", "START", "STOP",
		"DEPENDS-ON", "PRIORITY", "NEW", "SHARED", "REPO", "BASE", "tier", "RESULT"} {
		raw := []byte("RESULT: c1 sha=000000000000 tier: flash\n" +
			"KIND: fix\n" +
			"PATHS: internal/x.go\n" +
			"\n" +
			"Fix c1.\n" +
			key + ": x\n")
		got := LintHeaderStranded(raw)
		if len(got) != 1 {
			t.Fatalf("%s below the prose drew %+v; want one finding", key, got)
		}
		if !strings.Contains(got[0].Excerpt, key+": on line 6") {
			t.Fatalf("%s: excerpt %q does not name the key and its line", key, got[0].Excerpt)
		}
	}
}
