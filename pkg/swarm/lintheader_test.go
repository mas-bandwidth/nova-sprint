package swarm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A header key inside the header block the gate reads passes: LintHeaderStranded
// answers nothing (docs/SPEC-SPRINT.md section 2, the card header).
func TestLintHeaderStrandedAcceptsAKeyInTheHeader(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	raw := []byte("RESULT: c1 sha=000000000000 tier: flash\n" +
		"KIND: fix\n" +
		"PATHS: internal/x.go\n" +
		"\n" +
		"Fix c1: read DEPENDS-ON: for the chain, not this sentence.\n")
	if got := LintHeaderStranded(raw); len(got) != 0 {
		t.Fatalf("a mid-line mention drew %+v; want no finding", got)
	}
}

// CardHeaderValue reads one key of the typed block: the first of two lines with the same
// key, the empty value of an empty line, and nothing for a key only below the prose.
func TestCardHeaderValueReadsTheBlockOnly(t *testing.T) {
	t.Parallel()

	repeated := []byte("RESULT: c1 sha=000000000000\nKIND: fix\nKIND: bug\n\nTHE TASK.\n")
	v, ok := CardHeaderValue(repeated, "KIND")
	assert.True(t, ok)
	assert.Equal(t, "fix", v, "the first line with the key is the value")

	empty := []byte("RESULT: c1 sha=000000000000\nKIND: \n\nTHE TASK.\n")
	v, ok = CardHeaderValue(empty, "KIND")
	assert.True(t, ok, "an empty value is still a present key")
	assert.Equal(t, "", v)

	below := []byte("RESULT: c1 sha=000000000000\nKIND: fix\n\nTHE TASK.\nPATHS: pkg/x.go\n")
	_, ok = CardHeaderValue(below, "PATHS")
	assert.False(t, ok, "a key only below the header block reads as absent")

	_, ok = CardHeaderValue(below, "DEPENDS-ON")
	assert.False(t, ok)
}

// CardPaths is the typed header's PATHS and NEW globs: comma separated (a space after the
// comma is trimmed), `none` names nothing, and a key below the prose grants no scope.
func TestCardPathsReadsPATHSAndNEW(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"comma separated", "RESULT: c1 sha=0\nPATHS: a.go,b.go\n\nT.\n", []string{"a.go", "b.go"}},
		{"comma and space separated", "RESULT: c1 sha=0\nPATHS: a.go, b.go\n\nT.\n", []string{"a.go", "b.go"}},
		{"none names nothing", "RESULT: c1 sha=0\nPATHS: none\n\nT.\n", nil},
		{"empty value names nothing", "RESULT: c1 sha=0\nPATHS: \n\nT.\n", nil},
		{"NEW joins PATHS", "RESULT: c1 sha=0\nPATHS: a.go\nNEW: b.go\n\nT.\n", []string{"a.go", "b.go"}},
		{"repeated PATHS takes the first", "RESULT: c1 sha=0\nPATHS: a.go\nPATHS: b.go\n\nT.\n", []string{"a.go"}},
		{"a key below the prose grants no scope", "RESULT: c1 sha=0\nKIND: fix\n\nT.\nPATHS: a.go\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, CardPaths([]byte(tt.raw)))
		})
	}
}

// LintCardHeader refuses a stranded typed key, an unknown KIND, and a required header that
// is missing, each with the token the lint prints.
func TestLintCardHeaderRefusesTypedHeaderDefects(t *testing.T) {
	t.Parallel()

	stranded := []byte("RESULT: c1 sha=000000000000\nPATHS: pkg/x.go\n\nTHE TASK.\nKIND: fix\n")
	got := LintCardHeader(stranded, nil, false)
	assert.Contains(t, checksOfFindings(got), "kind-declared", "%+v", got)

	unknown := []byte("RESULT: c1 sha=000000000000\nKIND: completely-unknown-kind\nPATHS: pkg/x.go\nTEST: ./pkg TestA\n\nTHE TASK.\n")
	got = LintCardHeader(unknown, nil, false)
	require.Len(t, findingsFor(got, "kind-declared"), 1)
	assert.Contains(t, findingsFor(got, "kind-declared")[0].Excerpt, "not a kind this toolchain declares")

	missing := LintCardHeader([]byte("RESULT: c1 sha=000000000000\n\nTHE TASK.\n"), nil, true)
	for _, want := range []string{"kind-declared", "paths-declared", "test-named", "start-named", "stop-named"} {
		assert.Contains(t, checksOfFindings(missing), want, "%+v", missing)
	}
}

func checksOfFindings(fs []CardHeaderFinding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Check)
	}
	return out
}

func findingsFor(fs []CardHeaderFinding, check string) []CardHeaderFinding {
	var out []CardHeaderFinding
	for _, f := range fs {
		if f.Check == check {
			out = append(out, f)
		}
	}
	return out
}

// UnfilledTemplateLines names a line of the card template still holding its <...>
// fill-ins, and only the template's own lines: a similar line the card writes itself is
// not this finding.
func TestUnfilledTemplateLinesNamesTemplatePlaceholders(t *testing.T) {
	t.Parallel()
	card := "RESULT: c1 sha=000000000000\n" +
		"REPO: <owner>/<name>\n" +
		"The task uses --count <n> and that is fine.\n"
	got := UnfilledTemplateLines(card)
	require.Len(t, got, 1)
	assert.Equal(t, PlaceholderCheck, got[0].Check)
	assert.Equal(t, 2, got[0].Line)
	assert.Equal(t, "REPO: <owner>/<name>", got[0].Excerpt)

	assert.Empty(t, UnfilledTemplateLines("RESULT: c1 sha=0\nREPO: owner/name\n"))
}

// CardParentPaths names the card lines that reach above the job, and not a `../` the card
// merely quotes (a fenced block, a backtick span, a markdown link target).
func TestCardParentPathsNamesParentPathsButNotQuotedOnes(t *testing.T) {
	t.Parallel()
	lines := []string{
		"cd ../notes",                    // 1: walked
		"a line with `cat ../x`",         // 2: inline code, quoted
		"```",                            // 3: fence opens
		"cd ../inside-the-fence",         // 4: quoted
		"```",                            // 5: fence closes
		"see [the notes](../notes/x.md)", // 6: link target, quoted
		"just prose mentioning ../up",    // 7: reached
	}
	assert.Equal(t, []int{1, 7}, CardParentPaths(lines))
}

// Every key the card header names is refused when it sits below the prose: one finding
// each, in line order, naming the key.
func TestLintHeaderStrandedNamesEveryStrandedHeaderKey(t *testing.T) {
	t.Parallel()
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

// CardHeaderChecks is every token this file draws, in one byte-stable order.
func TestCardHeaderChecksListsTheTokens(t *testing.T) {
	t.Parallel()
	got := CardHeaderChecks()
	assert.Equal(t, []string{"kind-declared", "paths-declared", "paused", "test-named"}, got)
}

// ResolveInstructionKind is the instruction kind of a card's KIND: line, read through the
// one kinds list; a card with no KIND, or an unknown kind, is not ok.
func TestResolveInstructionKindReadsTheKindColumn(t *testing.T) {
	t.Parallel()
	work, inst, ok := ResolveInstructionKind([]byte("RESULT: c1 sha=0\nKIND: fix-red\n\nT.\n"))
	assert.True(t, ok)
	assert.Equal(t, "fix-red", work)
	assert.Equal(t, "think", inst)

	_, _, ok = ResolveInstructionKind([]byte("RESULT: c1 sha=0\n\nT.\n"))
	assert.False(t, ok)

	_, _, ok = ResolveInstructionKind([]byte("RESULT: c1 sha=0\nKIND: completely-unknown-kind\n\nT.\n"))
	assert.False(t, ok)
}

// ReadTrustFixture reads the coordinator's per-kind state from a file in the shape
// `nova-pulse trust` prints; non-TRUST rows and the TRUST OK summary are read past.
func TestReadTrustFixtureReadsTheTrustRows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "trust.txt")
	body := "TRUST kind=fix area=core state=paused cards=1/2\n" +
		"TRUST kind=read area=core state=trial cards=3/4\n" +
		"TRUST OK kinds=2 trial=1 trusted=0 paused=1\n" +
		"some other line\n"
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	got, err := ReadTrustFixture(p)
	require.NoError(t, err)
	assert.Equal(t, TrustState{"fix": "paused", "read": "trial"}, got)

	_, err = ReadTrustFixture(filepath.Join(dir, "absent.txt"))
	require.Error(t, err)
}

// LintCardHeader refuses a repeated typed key, an empty value, a TEST: none on a gated
// kind, a paused kind, and a brief missing its START/STOP lines.
func TestLintCardHeaderRefusesMoreTypedHeaderDefects(t *testing.T) {
	t.Parallel()

	repeated := []byte("RESULT: c1 sha=0\nKIND: fix-red\nKIND: read\nPATHS: pkg/x.go\nTEST: ./pkg TestA\n\nT.\n")
	got := LintCardHeader(repeated, nil, false)
	assert.Contains(t, checksOfFindings(got), "kind-declared", "%+v", got)

	emptyKind := []byte("RESULT: c1 sha=0\nKIND: \nPATHS: pkg/x.go\nTEST: ./pkg TestA\n\nT.\n")
	got = LintCardHeader(emptyKind, nil, false)
	assert.Contains(t, checksOfFindings(got), "kind-declared", "%+v", got)

	emptyPaths := []byte("RESULT: c1 sha=0\nKIND: fix-red\nPATHS: \nTEST: ./pkg TestA\n\nT.\n")
	got = LintCardHeader(emptyPaths, nil, false)
	assert.Contains(t, checksOfFindings(got), "paths-declared", "%+v", got)

	emptyTest := []byte("RESULT: c1 sha=0\nKIND: fix-red\nPATHS: pkg/x.go\nTEST: \n\nT.\n")
	got = LintCardHeader(emptyTest, nil, false)
	assert.Contains(t, checksOfFindings(got), "test-named", "%+v", got)

	noneGated := []byte("RESULT: c1 sha=0\nKIND: fix-red\nPATHS: pkg/x.go\nTEST: none no test needed\n\nT.\n")
	got = LintCardHeader(noneGated, nil, false)
	assert.Contains(t, checksOfFindings(got), "test-named", "%+v", got)

	paused := []byte("RESULT: c1 sha=0\nKIND: fix-red\nPATHS: pkg/x.go\nTEST: ./pkg TestA\n\nT.\n")
	got = LintCardHeader(paused, TrustState{"fix-red": "paused"}, false)
	assert.Contains(t, checksOfFindings(got), "paused", "%+v", got)

	brief := []byte("RESULT: c1 sha=0 tier: pro\nKIND: fix-red\nPATHS: pkg/x.go\nTEST: ./pkg TestA\n\nT.\n")
	got = LintCardHeader(brief, nil, false)
	assert.Contains(t, checksOfFindings(got), "start-named", "%+v", got)
	assert.Contains(t, checksOfFindings(got), "stop-named", "%+v", got)
}

// A declared KIND whose row of the one kinds list names no instruction kind is refused as
// kind-declared, through the lintCardHeader seam.
func TestLintCardHeaderRefusesAKindWithNoInstructionKind(t *testing.T) {
	t.Parallel()
	raw := []byte("RESULT: c1 sha=0\nKIND: fix-red\nPATHS: pkg/x.go\nTEST: ./pkg TestA\n\nT.\n")
	got := lintCardHeader(raw, nil, false, func(string) (string, bool) { return "", false })
	require.Len(t, findingsFor(got, "kind-declared"), 1)
	assert.Contains(t, findingsFor(got, "kind-declared")[0].Excerpt, "names no instruction kind")
}
