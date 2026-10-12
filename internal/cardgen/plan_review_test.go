package cardgen

import (
	"strings"
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

// These briefs use the repository staging actually reads, including base-repo
// overrides, rather than a raw REPO label.
func TestReviewRepositoryIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, a, b string
		overlap    bool
	}{
		{"distinct repositories", "REPO: mas-bandwidth/nova-tools", "REPO: mas-bandwidth/nova-sprint", false},
		{"same repository", "REPO: mas-bandwidth/nova-tools", "REPO: mas-bandwidth/nova-tools", true},
		{"owner name and HTTPS", "REPO: mas-bandwidth/nova-tools", "REPO: https://github.com/mas-bandwidth/nova-tools.git", true},
		{"HTTPS and scp", "REPO: https://forge.test/owner/name.git", "REPO: git@forge.test:owner/name", true},
		{"HTTPS and SSH", "REPO: https://forge.test/owner/name", "REPO: ssh://git@forge.test/owner/name.git/", true},
		{"host case", "REPO: https://Forge.TEST/owner/name.git", "REPO: https://forge.test/owner/name", true},
		{"path case", "REPO: https://forge.test/Owner/name.git", "REPO: https://forge.test/owner/name.git", false},
		{"local alias", "REPO: /srv/git/name.git", "REPO: /srv/git/name", true},
		{"local path case", "REPO: /srv/git/Repo.git", "REPO: /srv/git/repo.git", false},
		{"override joins different labels", "REPO: mas-bandwidth/nova-tools\nbase-repo: https://forge.test/owner/name.git", "REPO: mas-bandwidth/nova-sprint\nbase-repo: git@forge.test:owner/name", true},
		{"override splits same labels", "REPO: mas-bandwidth/nova-tools\nbase-repo: https://forge.test/owner/name.git", "REPO: mas-bandwidth/nova-tools\nbase-repo: https://forge.test/owner/other.git", false},
		{"unnamed and known", "", "REPO: mas-bandwidth/nova-tools", true},
		{"unresolved and known", "REPO: unresolved", "REPO: mas-bandwidth/nova-tools", true},
		{"different unresolved labels", "REPO: unresolved-a", "REPO: unresolved-b", true},
		{"unresolved override", "REPO: mas-bandwidth/nova-tools\nbase-repo: unresolved", "REPO: mas-bandwidth/nova-sprint", true},
		{"body does not supply identity", "\nREPO: mas-bandwidth/nova-tools", "REPO: mas-bandwidth/nova-sprint", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cards := []CardPlan{
				{ID: "fix-a", Brief: "# t\nPATHS: src/x.go\n" + tc.a + "\n"},
				{ID: "fix-b", Brief: "# t\nPATHS: src/x.go\n" + tc.b + "\n"},
			}
			rows := SharedPathRows(cards)
			if !tc.overlap {
				require.Empty(t, rows)
				return
			}
			require.Len(t, rows, 1)
			assert.Equal(t, "src/x.go", rows[0].Path)
			assert.Equal(t, []string{"fix-a", "fix-b"}, rows[0].Cards)
			assert.Contains(t, RefuseOverlaps(rows), "neither needs the other")
		})
	}
}

func TestReviewKnownRepositoryKeepsLedgerAndNeeds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path, needs  string
		ledger, edge bool
	}{
		{"FIXES.md", "", true, false},
		{"src/x.go", "fix-a", false, true},
	} {
		t.Run(tc.path, func(t *testing.T) {
			brief := func(repo, needs string) string {
				b := strings.Replace(planBrief(tc.path, "", needs), "REPO: r", "REPO: "+repo, 1)
				return b
			}
			rows := SharedPathRows([]CardPlan{
				{ID: "fix-a", Brief: brief("mas-bandwidth/nova-tools", "")},
				{ID: "fix-b", Brief: brief("https://github.com/mas-bandwidth/nova-tools.git", tc.needs)},
			})
			require.Len(t, rows, 1)
			assert.Equal(t, tc.ledger, rows[0].Ledger)
			assert.Equal(t, tc.edge, rows[0].Edge)
			assert.Empty(t, RefuseOverlaps(rows))
		})
	}
}

func TestReviewUnresolvedRepositoryOverlapsEachKnownRepository(t *testing.T) {
	t.Parallel()
	rows := SharedPathRows([]CardPlan{
		{ID: "fix-a", Brief: "# t\nPATHS: src/x.go\nREPO: owner/a\n"},
		{ID: "fix-b", Brief: "# t\nPATHS: src/x.go\nREPO: owner/b\n"},
		{ID: "unknown", Brief: "# t\nPATHS: src/x.go\nNeeds: fix-a\n"},
	})
	require.Len(t, rows, 2)
	assert.Equal(t, []string{"fix-a", "unknown"}, rows[0].Cards)
	assert.True(t, rows[0].Edge)
	assert.Equal(t, []string{"fix-b", "unknown"}, rows[1].Cards)
	assert.False(t, rows[1].Edge)
	assert.Contains(t, RefuseOverlaps(rows), "fix-b and unknown")
}

func TestReviewRepositoryRowsKeepPlanOrderAndDeduplicatePaths(t *testing.T) {
	t.Parallel()
	rows := SharedPathRows([]CardPlan{
		{ID: "unknown", Brief: "# t\nPATHS: src/x.go src/x.go, src/y.go\n"},
		{ID: "fix-a", Brief: "# t\nPATHS: src/x.go src/y.go\nREPO: owner/a\n"},
		{ID: "fix-b", Brief: "# t\nPATHS: src/x.go\nREPO: owner/b\n"},
	})
	require.Len(t, rows, 3)
	assert.Equal(t, "src/x.go", rows[0].Path)
	assert.Equal(t, "src/x.go", rows[1].Path)
	assert.Equal(t, "src/y.go", rows[2].Path)
	assert.Equal(t, []string{"unknown", "fix-a"}, rows[0].Cards)
	assert.Equal(t, []string{"unknown", "fix-b"}, rows[1].Cards)
	assert.Equal(t, []string{"unknown", "fix-a"}, rows[2].Cards)
}

func TestReviewFirstNamedPathOrderIncludesUnresolvedCards(t *testing.T) {
	t.Parallel()
	rows := SharedPathRows([]CardPlan{
		{ID: "unknown", Brief: "# t\nPATHS: p.go q.go\n"},
		{ID: "fix-a", Brief: "# t\nPATHS: q.go p.go\nREPO: owner/a\n"},
	})
	require.Len(t, rows, 2)
	assert.Equal(t, "p.go", rows[0].Path)
	assert.Equal(t, "q.go", rows[1].Path)
	assert.Equal(t, []string{"unknown", "fix-a"}, rows[0].Cards)
	assert.Contains(t, RefuseOverlaps(rows), "p.go is named in PATHS")
}

func TestReviewPathsReadMarkerKeepsOverlap(t *testing.T) {
	t.Parallel()
	rows := SharedPathRows([]CardPlan{
		{ID: "a", Brief: "RESULT: a\nREPO: owner/repo\nPATHS: src/x.go (read)\n"},
		{ID: "b", Brief: "RESULT: b\nREPO: owner/repo\nPATHS: src/x.go\n"},
	})
	require.Len(t, rows, 1)
	assert.Equal(t, "src/x.go", rows[0].Path)
	assert.Equal(t, []string{"a", "b"}, rows[0].Cards)
	assert.NotEmpty(t, RefuseOverlaps(rows))
}
