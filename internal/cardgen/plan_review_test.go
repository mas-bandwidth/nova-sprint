package cardgen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The plan review (internal/cardgen/plan_review.go) is what a seat reads before
// admitting a stream (docs/SPRINT-COORDINATOR.md, the plan review): it prints the
// overlap table — one line per path two or more cards name in PATHS, with the cards,
// ledger or not, and the edge if any — and refuses the whole set on the first
// non-ledger overlap without an edge (docs/SPEC-CARD-CONTRACT.md, the shared-path rule).

func TestReviewTableNamesPathCardsLedgerAndEdge(t *testing.T) {
	t.Parallel()
	table, refusal := Review([]CardPlan{
		{ID: "fix-a", Brief: planBrief("src/x.go", "", "")},
		{ID: "fix-b", Brief: planBrief("src/x.go", "", "")},
	})
	assert.Equal(t, "src/x.go  fix-a,fix-b  not-ledger  no-edge\n", table)
	assert.NotEqual(t, "", refusal, "a non-ledger overlap without an edge is refused")
}

func TestReviewNamesLedgerRows(t *testing.T) {
	t.Parallel()
	table, refusal := Review([]CardPlan{
		{ID: "fix-a", Brief: planBrief("FIXES.md", "", "")},
		{ID: "fix-b", Brief: planBrief("FIXES.md", "", "")},
	})
	assert.Equal(t, "FIXES.md  fix-a,fix-b  ledger  no-edge\n", table)
	assert.Equal(t, "", refusal, "a ledger the lander merges is shared freely")
}

func TestReviewNamesEdgeRows(t *testing.T) {
	t.Parallel()
	table, refusal := Review([]CardPlan{
		{ID: "fix-a", Brief: planBrief("src/x.go", "", "")},
		{ID: "fix-b", Brief: planBrief("src/x.go", "", "fix-a")},
	})
	assert.Equal(t, "src/x.go  fix-a,fix-b  not-ledger  edge\n", table)
	assert.Equal(t, "", refusal, "a needs edge chains the pair and admits them")
}

func TestReviewCleanPlanIsAnEmptyTableAndNoRefusal(t *testing.T) {
	t.Parallel()
	table, refusal := Review([]CardPlan{
		{ID: "fix-a", Brief: planBrief("src/a.go", "", "")},
		{ID: "fix-b", Brief: planBrief("src/b.go", "", "")},
	})
	assert.Equal(t, "", table)
	assert.Equal(t, "", refusal)
}

// briefNeeds reads a plan's needs the same way add reads a brief's: the first Needs:
// header, else DEPENDS-ON:, ids comma separated, each cut at a parenthesis; none, -,
// an owner/repo#n reference, or no line is no needs.
func TestBriefNeeds(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"fix-a", "fix-b"}, briefNeeds("t\nNeeds: fix-a, fix-b (a note)\n"))
	assert.Equal(t, []string{"fix-a"}, briefNeeds("t\nDEPENDS-ON: fix-a\n"))
	assert.Empty(t, briefNeeds("t\nNeeds: none\n"))
	assert.Empty(t, briefNeeds("t\nNeeds: -\n"))
	assert.Empty(t, briefNeeds("t\nNeeds: owner/repo#123\n"))
	assert.Empty(t, briefNeeds("t\nno needs line here\n"))
}

// headerPaths reads a plan's PATHS: (or SHARED:) header line, commas or blanks
// between the files; none and - name no file.
func TestHeaderPaths(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"src/a.go", "src/b.go"}, headerPaths("t\nPATHS: src/a.go, src/b.go\n", "PATHS"))
	require.Equal(t, []string{"src/a.go", "src/b.go"}, headerPaths("t\nPATHS: src/a.go src/b.go\n", "PATHS"))
	require.Empty(t, headerPaths("t\nPATHS: none\n", "PATHS"))
	require.Empty(t, headerPaths("t\nPATHS: -\n", "PATHS"))
}
