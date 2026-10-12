package cardgen

import (
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
