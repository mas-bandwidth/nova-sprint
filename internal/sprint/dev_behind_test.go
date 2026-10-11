package sprint_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestLandedSinceProtectedBranches tests that LandedSince filters out cards
// landed on protected branches (dev, main).
func TestLandedSinceProtectedBranches(t *testing.T) {
	t.Parallel()

	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	work := sprint.NewTable(sprint.Work)
	work.SetRows([]string{"s1"})

	// Card on main (protected) - should NOT be counted
	mainCard := &sprint.Card{
		ID:    "main-card",
		Row:   "s1",
		Col:   sprint.Landed,
		Score: 0,
		Fields: map[string]string{
			"primary": "main-card",
			"landed":  promotionTime.Add(10 * time.Minute).Format(time.RFC3339),
			"base":    "main",
		},
	}
	work.Put(mainCard)

	// Card on dev (protected) - should NOT be counted
	devCard := &sprint.Card{
		ID:    "dev-card",
		Row:   "s1",
		Col:   sprint.Landed,
		Score: 0,
		Fields: map[string]string{
			"primary": "dev-card",
			"landed":  promotionTime.Add(20 * time.Minute).Format(time.RFC3339),
			"base":    "dev",
		},
	}
	work.Put(devCard)

	// Card on sprint branch - SHOULD be counted
	sprintCard := &sprint.Card{
		ID:    "sprint-card",
		Row:   "s1",
		Col:   sprint.Landed,
		Score: 0,
		Fields: map[string]string{
			"primary": "sprint-card",
			"landed":  promotionTime.Add(30 * time.Minute).Format(time.RFC3339),
			"base":    "sprint/test",
		},
	}
	work.Put(sprintCard)

	s := &sprint.Snapshot{
		Now:  promotionTime.Add(60 * time.Minute),
		Work: work,
	}
	s.Work.SetProp("promoted_at", promotionTime.Format(time.RFC3339))

	cards, _ := sprint.LandedSince(s)
	assert.Equal(t, 1, len(cards), "Should only count cards on non-protected branches")
	assert.Equal(t, "sprint-card", cards[0].ID)
}

// TestDevBehindProtectedBranches tests that DevBehind ignores cards on protected branches.
func TestDevBehindProtectedBranches(t *testing.T) {
	t.Parallel()

	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	work := sprint.NewTable(sprint.Work)
	work.SetRows([]string{"s1"})

	// Add cards on protected branches only
	for i := 0; i < 30; i++ {
		id := "card-" + fmt.Sprint(i)
		c := &sprint.Card{
			ID:     id,
			Row:    "s1",
			Col:    sprint.Landed,
			Score:  0,
			Fields: map[string]string{
				"primary": id,
				"landed":  promotionTime.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
				"base":    "main",
			},
		}
		work.Put(c)
	}

	s := &sprint.Snapshot{
		Now:  promotionTime.Add(60 * time.Minute),
		Work: work,
	}
	s.Work.SetProp("promoted_at", promotionTime.Format(time.RFC3339))

	lag, ok := sprint.DevBehind(s)
	assert.False(t, ok, "Should not raise judgment when all cards are on protected branches")
	assert.Equal(t, 0, lag.Count)
}

// TestDevBehindSprintBranch tests that DevBehind raises judgment for cards on sprint branches.
func TestDevBehindSprintBranch(t *testing.T) {
	t.Parallel()

	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	work := sprint.NewTable(sprint.Work)
	work.SetRows([]string{"s1"})

	// Add cards on sprint branch
	for i := 1; i <= 30; i++ {
		id := "card-" + fmt.Sprint(i)
		c := &sprint.Card{
			ID:     id,
			Row:    "s1",
			Col:    sprint.Landed,
			Score:  0,
			Fields: map[string]string{
				"primary": id,
				"landed":  promotionTime.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
				"base":    "sprint/x",
			},
		}
		work.Put(c)
	}

	s := &sprint.Snapshot{
		Now:  promotionTime.Add(60 * time.Minute),
		Work: work,
	}
	s.Work.SetProp("promoted_at", promotionTime.Format(time.RFC3339))

	lag, ok := sprint.DevBehind(s)
	assert.True(t, ok, "Should raise judgment when cards on sprint branch exceed threshold")
	assert.Equal(t, 30, lag.Count)
}

// TestDevBehindMixedBranches tests that DevBehind only counts non-protected branches.
func TestDevBehindMixedBranches(t *testing.T) {
	t.Parallel()

	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	work := sprint.NewTable(sprint.Work)
	work.SetRows([]string{"s1"})

	// Add 20 cards on protected branches
	for i := 0; i < 20; i++ {
		id := "protected-" + fmt.Sprint(i)
		c := &sprint.Card{
			ID:     id,
			Row:    "s1",
			Col:    sprint.Landed,
			Score:  0,
			Fields: map[string]string{
				"primary": id,
				"landed":  promotionTime.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
				"base":    "main",
			},
		}
		work.Put(c)
	}

	// Add 1 card on sprint branch (oldest, but after promotion)
	c := &sprint.Card{
		ID:    "sprint-card",
		Row:   "s1",
		Col:   sprint.Landed,
		Score: 0,
		Fields: map[string]string{
			"primary": "sprint-card",
			"landed":  promotionTime.Add(5 * time.Minute).Format(time.RFC3339),
			"base":    "sprint/x",
		},
	}
	work.Put(c)

	s := &sprint.Snapshot{
		Now:  promotionTime.Add(60 * time.Minute),
		Work: work,
	}
	s.Work.SetProp("promoted_at", promotionTime.Format(time.RFC3339))

	lag, ok := sprint.DevBehind(s)
	assert.True(t, ok, "Should raise judgment when oldest non-protected card exceeds PromoteAge")
	assert.Equal(t, 1, lag.Count)
}

// TestDevBehindWhatListsCards tests that DevLag.What lists cards by name.
func TestDevBehindWhatListsCards(t *testing.T) {
	t.Parallel()

	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	work := sprint.NewTable(sprint.Work)
	work.SetRows([]string{"s1"})

	// Add 5 cards on sprint branch
	for i := 0; i < 5; i++ {
		id := "card-" + fmt.Sprint(i)
		c := &sprint.Card{
			ID:     id,
			Row:    "s1",
			Col:    sprint.Landed,
			Score:  0,
			Fields: map[string]string{
				"primary": id,
				"landed":  promotionTime.Add(time.Duration(i+10) * time.Minute).Format(time.RFC3339),
				"base":    "sprint/x",
			},
		}
		work.Put(c)
	}

	s := &sprint.Snapshot{
		Now:  promotionTime.Add(60 * time.Minute),
		Work: work,
	}
	s.Work.SetProp("promoted_at", promotionTime.Format(time.RFC3339))
	s.Work.SetProp("promoted_sha", "1234567890abcdef")

	lag, ok := sprint.DevBehind(s)
	assert.True(t, ok)
	assert.Equal(t, 5, lag.Count)

	what := lag.What()
	assert.Contains(t, what, "card-0")
	assert.Contains(t, what, "card-4")
}

// TestDevBehindWhatWithManyCards tests that DevLag.What truncates after 10 cards.
func TestDevBehindWhatWithManyCards(t *testing.T) {
	t.Parallel()

	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	work := sprint.NewTable(sprint.Work)
	work.SetRows([]string{"s1"})

	// Add 15 cards on sprint branch
	for i := 0; i < 15; i++ {
		id := "card-" + fmt.Sprint(i)
		c := &sprint.Card{
			ID:     id,
			Row:    "s1",
			Col:    sprint.Landed,
			Score:  0,
			Fields: map[string]string{
				"primary": id,
				"landed":  promotionTime.Add(time.Duration(i+10) * time.Minute).Format(time.RFC3339),
				"base":    "sprint/x",
			},
		}
		work.Put(c)
	}

	s := &sprint.Snapshot{
		Now:  promotionTime.Add(60 * time.Minute),
		Work: work,
	}
	s.Work.SetProp("promoted_at", promotionTime.Format(time.RFC3339))
	s.Work.SetProp("promoted_sha", "1234567890abcdef")

	lag, ok := sprint.DevBehind(s)
	assert.True(t, ok)
	assert.Equal(t, 15, lag.Count)

	what := lag.What()
	assert.Contains(t, what, "and 5 more")
}

// TestDevBehindZeroCardsOnProtectedBranches tests that no judgment is raised when
// only protected branch cards exist.
func TestDevBehindZeroCardsOnProtectedBranches(t *testing.T) {
	t.Parallel()

	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	work := sprint.NewTable(sprint.Work)
	work.SetRows([]string{"s1"})

	// Add a single card on main 40 minutes ago
	c := &sprint.Card{
		ID:    "main-card",
		Row:   "s1",
		Col:   sprint.Landed,
		Score: 0,
		Fields: map[string]string{
			"primary": "main-card",
			"landed":  promotionTime.Add(-40 * time.Minute).Format(time.RFC3339),
			"base":    "main",
		},
	}
	work.Put(c)

	s := &sprint.Snapshot{
		Now:  promotionTime.Add(60 * time.Minute),
		Work: work,
	}
	s.Work.SetProp("promoted_at", promotionTime.Format(time.RFC3339))

	lag, ok := sprint.DevBehind(s)
	assert.False(t, ok, "Should not raise judgment for cards on protected branches")
	assert.Equal(t, 0, lag.Count)
}
