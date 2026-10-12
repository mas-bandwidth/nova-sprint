package sprint_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The dev is behind rule (docs/SPEC-SPRINT.md section 7): a landed card counts toward
// the judgment only when the base it actually landed on is known and is a sprint branch
// promoted to a dev branch that exists for its repository. The base is read from the
// card's own base field, else its brief's BASE: line (the same source the bases verb
// reads); a card landed straight onto dev or main, a card whose base is absent, or a
// card on a branch whose repository has no dev branch, never counts. The judgment names
// each counted card by its id when its primary is empty, never a blank.

// devBehindSnap builds a snapshot holding the given landed cards and a promotion
// recorded at promotionTime; the cards' landing stamps must be after it for them to
// count. Extra cards (the dev-branch evidence) are placed landed too.
func devBehindSnap(promotionTime time.Time, cards ...*sprint.Card) *sprint.Snapshot {
	work := sprint.NewTable(sprint.Work)
	work.SetRows([]string{"s1"})
	for _, c := range cards {
		work.Put(c)
	}
	s := &sprint.Snapshot{Now: promotionTime.Add(60 * time.Minute), Work: work}
	s.Work.SetProp(sprint.PropPromotedAt, promotionTime.Format(time.RFC3339))
	return s
}

// landed is a landed card with the given brief (its REPO: and BASE: lines are the source
// of its repository and base) and an empty primary, landed at promotionTime+minutes.
func landed(id, brief string, promotionTime time.Time, minutes int) *sprint.Card {
	fields := map[string]string{
		"landed": promotionTime.Add(time.Duration(minutes) * time.Minute).Format(time.RFC3339),
	}
	if brief != "" {
		fields["brief"] = brief
	}
	return &sprint.Card{ID: id, Row: "s1", Col: sprint.Landed, Fields: fields}
}

// sprintBaseBrief is a brief cut on a base branch of a repository.
func sprintBaseBrief(repo, base string) string { return "REPO: " + repo + "\nBASE: " + base + "\n" }

// TestDevBehindUncountedBases: the bases that never count toward the judgment, each a
// single card landed forty minutes after the promotion: an absent base, a card landed
// straight onto dev or main, and a sprint branch whose repository has no dev branch.
func TestDevBehindUncountedBases(t *testing.T) {
	t.Parallel()
	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	cases := []struct {
		name  string
		id    string
		brief string
	}{
		{"TestDevBehindACardWithNoBaseDoesNotCount", "no-base", "REPO: mas-bandwidth/widgets\n"},
		{"TestDevBehindACardOnDevDoesNotCount", "on-dev", sprintBaseBrief("mas-bandwidth/nova-tools", "dev")},
		{"TestDevBehindACardOnMainDoesNotCount", "on-main", sprintBaseBrief("mas-bandwidth/nova-sprint", "main")},
		{"TestDevBehindASprintBranchWithoutADevBranchDoesNotCount", "schema-1", sprintBaseBrief("mas-bandwidth/schema", "sprint/schema-finish")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := landed(tc.id, tc.brief, promotionTime, 40)
			s := devBehindSnap(promotionTime, c)

			lag, ok := sprint.DevBehind(s)
			assert.False(t, ok, "%s: never counts", tc.name)
			assert.Equal(t, 0, lag.Count)
		})
	}
}

// TestDevBehindASprintBranchWithADevBranchCountsNamedByID: a sprint branch whose
// repository has a dev branch counts, and the judgment names the card by its id when its
// primary is empty.
func TestDevBehindASprintBranchWithADevBranchCountsNamedByID(t *testing.T) {
	t.Parallel()
	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	repo := "mas-bandwidth/nova-tools"
	// The repository has a dev branch: a card landed on dev there.
	devEvidence := landed("dev-evidence", sprintBaseBrief(repo, "dev"), promotionTime, 5)
	// The sprint card landed 50 minutes before the snapshot's now, past PromoteAge.
	sprintCard := landed("sprint-card", sprintBaseBrief(repo, "sprint/base"), promotionTime, 10)
	s := devBehindSnap(promotionTime, devEvidence, sprintCard)

	lag, ok := sprint.DevBehind(s)
	assert.True(t, ok, "a sprint branch whose repository has a dev branch counts")
	assert.Equal(t, 1, lag.Count)
	assert.Equal(t, []string{"sprint-card"}, lag.Cards, "named by its id when primary is empty")
	assert.Contains(t, lag.What(), "sprint-card")
}

// TestDevBehindTheJudgmentNeverCarriesAnEmptyName: the judgment text never contains an
// empty name, so a blank primary becomes the card's id.
func TestDevBehindTheJudgmentNeverCarriesAnEmptyName(t *testing.T) {
	t.Parallel()
	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	repo := "mas-bandwidth/nova-tools"
	devEvidence := landed("dev-evidence", sprintBaseBrief(repo, "dev"), promotionTime, 5)
	var cards []*sprint.Card
	for i := 1; i <= 30; i++ {
		cards = append(cards, landed("card-"+strconv.Itoa(i), sprintBaseBrief(repo, "sprint/base"), promotionTime, i))
	}
	s := devBehindSnap(promotionTime, append([]*sprint.Card{devEvidence}, cards...)...)

	lag, ok := sprint.DevBehind(s)
	assert.True(t, ok)
	assert.Equal(t, 30, lag.Count)
	what := lag.What()
	assert.NotContains(t, what, ", , ", "no blank names")
	assert.NotContains(t, what, ": ,", "no leading blank name")
	assert.Contains(t, what, "card-1", "the first counted card is named")
	for _, name := range lag.Cards {
		assert.NotEqual(t, "", name, "every counted card is named")
	}
}

// TestDevBehindNoRepositoryHasSprintAndDevNeverRaises: when no repository has both a
// sprint branch and a dev branch, the judgment is never raised, even past the count.
func TestDevBehindNoRepositoryHasSprintAndDevNeverRaises(t *testing.T) {
	t.Parallel()
	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	// Many cards on sprint branches, but none of their repositories has a dev branch.
	var cards []*sprint.Card
	for i := 1; i <= 30; i++ {
		cards = append(cards, landed("s-"+strconv.Itoa(i), sprintBaseBrief("mas-bandwidth/schema", "sprint/schema-finish"), promotionTime, i))
	}
	s := devBehindSnap(promotionTime, cards...)

	lag, ok := sprint.DevBehind(s)
	assert.False(t, ok, "no repository has both a sprint and a dev branch: never raised")
	assert.Equal(t, 0, lag.Count)
}
