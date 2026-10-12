package cardgen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// planBrief is a minimal card brief with the typed header lines a plan review
// reads.
func planBrief(paths, shared, needs string) string {
	b := "# t\nTEST: x\nREPO: r\nBASE: main\n"
	if shared != "" {
		b += "SHARED: " + shared + "\n"
	}
	if paths != "" {
		b += "PATHS: " + paths + "\n"
	}
	if needs != "" {
		b += "Needs: " + needs + "\n"
	}
	return b
}

// The shared-path rule, docs/SPEC-CARD-CONTRACT.md: two cards sharing a path that
// no lander-owned ledger covers are refused at admission with the overlap named
// (both card ids and the path), and there is no flag that admits them: a brief
// that lists such a path on a SHARED: line is refused the same way. A needs edge
// between the two admits them; a ledger the lander merges admits them.
func TestSharedPathRuleRefusesNonLedgerOverlap(t *testing.T) {
	t.Parallel()
	rows := SharedPathRows([]CardPlan{
		{ID: "fix-a", Brief: planBrief("src/x.go", "", "")},
		{ID: "fix-b", Brief: planBrief("src/x.go", "", "")},
	})
	require.Len(t, rows, 1)
	assert.Equal(t, "src/x.go", rows[0].Path)
	assert.Equal(t, []string{"fix-a", "fix-b"}, rows[0].Cards)
	assert.False(t, rows[0].Ledger, "src/x.go is no lander-owned ledger")
	assert.False(t, rows[0].Edge, "no needs edge chains the pair")
	assert.Equal(t, "src/x.go is named in PATHS by fix-a and fix-b, and neither needs the other", RefuseOverlaps(rows))
}

func TestSharedPathRuleRefusesSharededNonLedgerOverlap(t *testing.T) {
	t.Parallel()
	// a SHARED: line does not admit a non-ledger path: the lander merges only
	// ledgers, so two cards both declaring the file still collide.
	rows := SharedPathRows([]CardPlan{
		{ID: "fix-a", Brief: planBrief("src/x.go", "src/x.go", "")},
		{ID: "fix-b", Brief: planBrief("src/x.go", "src/x.go", "")},
	})
	require.Len(t, rows, 1)
	assert.False(t, rows[0].Ledger)
	assert.False(t, rows[0].Edge)
	assert.NotEqual(t, "", RefuseOverlaps(rows))
}

func TestSharedPathRuleRefusesTestFileOverlap(t *testing.T) {
	t.Parallel()
	// a *_test.go file is inside every card's PATHS by AlwaysInPathsRule, but it
	// is not a lander-owned ledger the lander merges, so two cards sharing it still
	// collide unless a needs edge chains them.
	rows := SharedPathRows([]CardPlan{
		{ID: "fix-a", Brief: planBrief("cmd/nova-sprint/add_test.go", "", "")},
		{ID: "fix-b", Brief: planBrief("cmd/nova-sprint/add_test.go", "", "")},
	})
	require.Len(t, rows, 1)
	assert.False(t, rows[0].Ledger, "add_test.go is no lander-owned ledger")
	assert.False(t, rows[0].Edge)
	assert.NotEqual(t, "", RefuseOverlaps(rows))
}

func TestSharedPathRuleAdmitsNeedsEdge(t *testing.T) {
	t.Parallel()
	// the second card lands after the first and is staged at its tip
	rows := SharedPathRows([]CardPlan{
		{ID: "fix-a", Brief: planBrief("src/x.go", "", "")},
		{ID: "fix-b", Brief: planBrief("src/x.go", "", "fix-a")},
	})
	require.Len(t, rows, 1)
	assert.False(t, rows[0].Ledger)
	assert.True(t, rows[0].Edge, "fix-b needs fix-a, so the pair is chained")
	assert.Equal(t, "", RefuseOverlaps(rows))
}

func TestSharedPathRuleAdmitsLedger(t *testing.T) {
	t.Parallel()
	// the lander merges FIXES.md, so two cards sharing it do not collide
	rows := SharedPathRows([]CardPlan{
		{ID: "fix-a", Brief: planBrief("FIXES.md", "", "")},
		{ID: "fix-b", Brief: planBrief("FIXES.md", "", "")},
	})
	require.Len(t, rows, 1)
	assert.True(t, rows[0].Ledger)
	assert.Equal(t, "", RefuseOverlaps(rows))
}

func TestSharedPathRuleNoOverlapWhenPathsDiffer(t *testing.T) {
	t.Parallel()
	rows := SharedPathRows([]CardPlan{
		{ID: "fix-a", Brief: planBrief("src/a.go", "", "")},
		{ID: "fix-b", Brief: planBrief("src/b.go", "", "")},
	})
	assert.Empty(t, rows)
}

// A brief whose text names a sibling path (../<name>, a sibling repository by name)
// is refused at admission unless it carries a SIBLINGS header naming the repositories
// (owner/name@ref, comma separated), with the path quoted (docs/SPEC-CARD-CONTRACT.md,
// the SIBLINGS header; docs/SPEC-STAGING.md, the siblings): the harness is pointed at
// the sibling by the staged JOB.md, so a brief that reaches outside its tree must say
// where the sibling comes from.
func TestALintRefusesASiblingPathWithoutASIBLINGSHeader(t *testing.T) {
	t.Parallel()
	base := Render(header, Card{ID: "a", File: "x/y.go", Paths: []string{"x/y.go"}, Test: "x TestA", Tier: "pro", Kind: "fix-red", Task: "Do it."})
	require.Empty(t, Lint("a", base), "the rendered brief is the clean base")

	naming := strings.Replace(base, "Do it.", "The harness expects the C++ reference corpus at ../serialize next to the checkout.", 1)
	checks := Lint("a", naming)
	var sibling LintFinding
	for _, f := range checks {
		if f.Check == "sibling-path" {
			sibling = f
		}
	}
	require.NotEmpty(t, sibling.Check, "a sibling path with no SIBLINGS header is refused")
	assert.Contains(t, sibling.Excerpt, "../serialize", "the path is quoted in the refusal")

	withHeader := strings.Replace(naming, "\nTHE TASK.", "\nSIBLINGS: mas-bandwidth/serialize@abc\nTHE TASK.", 1)
	assert.Empty(t, Lint("a", withHeader), "with the SIBLINGS header the sibling is admitted")
}

// A brief that names a sibling path but no sibling repository matches it in its
// SIBLINGS header is still refused: the header's repositories must cover the paths
// the text names.
func TestALintRefusesASiblingPathNotListedInTheHeader(t *testing.T) {
	t.Parallel()
	base := Render(header, Card{ID: "a", File: "x/y.go", Paths: []string{"x/y.go"}, Test: "x TestA", Tier: "pro", Kind: "fix-red", Task: "Do it."})
	naming := strings.Replace(base, "Do it.", "Compare against the reference corpus at ../serialize.c next to the checkout.", 1)
	naming = strings.Replace(naming, "\nTHE TASK.", "\nSIBLINGS: mas-bandwidth/serialize@abc\nTHE TASK.", 1)
	checks := Lint("a", naming)
	found := false
	for _, f := range checks {
		if f.Check == "sibling-path" && strings.Contains(f.Excerpt, "../serialize.c") {
			found = true
		}
	}
	assert.True(t, found, "a sibling path absent from the header is refused and quoted")
}
