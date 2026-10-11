package sprint_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestLandedSinceProtectedBranches tests that LandedSince filters out cards
// landed on protected branches (dev, main).
func TestLandedSinceProtectedBranches(t *testing.T) {
	t.Parallel()

	// Create a snapshot with cards on protected branches and sprint branches
	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	// Build work table manually
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

	// Create snapshot
	s := &sprint.Snapshot{
		Now:  promotionTime.Add(60 * time.Minute),
		Work: work,
	}
	s.Work.SetProp("promoted_at", promotionTime.Format(time.RFC3339))

	// Test LandedSince - should only return sprint branch cards
	cards, _ := sprint.LandedSince(s)
	assert.Equal(t, 1, len(cards), "Should only count cards on non-protected branches")
	assert.Equal(t, "sprint-card", cards[0].ID)
}

// TestDevBehindProtectedBranches tests that DevBehind ignores cards on protected branches.
func TestDevBehindProtectedBranches(t *testing.T) {
	t.Parallel()

	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	// Build work table with only protected branch cards
	work := sprint.NewTable(sprint.Work)
	work.SetRows([]string{"s1"})

	// Add cards on protected branches
	for i := 0; i < 30; i++ {
		id := "card-" + string(rune('0'+i))
		c := &sprint.Card{
			ID:    id,
			Row:   "s1",
			Col:   sprint.Landed,
			Score: 0,
			Fields: map[string]string{
				"primary": id,
				"landed":  promotionTime.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
				"base":    "main",
			},
		}
		work.Put(c)
	}

	s := &sprint.Snapshot{
		Now:  promotionTime.Add(35 * time.Minute),
		Work: work,
	}
	s.Work.SetProp("promoted_at", promotionTime.Format(time.RFC3339))

	// DevBehind should NOT fire when all cards are on protected branches
	_, ok := sprint.DevBehind(s)
	assert.False(t, ok, "DevBehind should not fire when all cards are on protected branches")
}

// TestDevBehindMixed tests mixed protected and non-protected branch cards.
func TestDevBehindMixed(t *testing.T) {
	t.Parallel()

	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	// Build work table with mixed cards
	work := sprint.NewTable(sprint.Work)
	work.SetRows([]string{"s1"})

	// Add cards on protected branches (should not count)
	for i := 0; i < 20; i++ {
		id := "protected-" + string(rune('0'+i))
		c := &sprint.Card{
			ID:    id,
			Row:   "s1",
			Col:   sprint.Landed,
			Score: 0,
			Fields: map[string]string{
				"primary": id,
				"landed":  promotionTime.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
				"base":    "dev",
			},
		}
		work.Put(c)
	}

	// Add cards on sprint branch (should count)
	for i := 0; i < 5; i++ {
		id := "sprint-" + string(rune('0'+i))
		c := &sprint.Card{
			ID:    id,
			Row:   "s1",
			Col:   sprint.Landed,
			Score: 0,
			Fields: map[string]string{
				"primary": id,
				"landed":  promotionTime.Add(time.Duration(20+i) * time.Minute).Format(time.RFC3339),
				"base":    "sprint/feature",
			},
		}
		work.Put(c)
	}

	s := &sprint.Snapshot{
		Now:  promotionTime.Add(60 * time.Minute),
		Work: work,
	}
	s.Work.SetProp("promoted_at", promotionTime.Format(time.RFC3339))

	// DevBehind should fire with count of 5 (only sprint branch cards)
	lag, ok := sprint.DevBehind(s)
	assert.True(t, ok)
	assert.Equal(t, 5, lag.Count, "Should only count non-protected branch cards")
}

// TestDevBehindNoBase tests cards without a base field.
func TestDevBehindNoBase(t *testing.T) {
	t.Parallel()

	promotionTime := time.Date(2030, 1, 2, 3, 4, 0, 0, time.UTC)

	work := sprint.NewTable(sprint.Work)
	work.SetRows([]string{"s1"})

	// Card without base field - should be counted
	c := &sprint.Card{
		ID:    "no-base-card",
		Row:   "s1",
		Col:   sprint.Landed,
		Score: 0,
		Fields: map[string]string{
			"primary": "no-base-card",
			"landed":  promotionTime.Add(10 * time.Minute).Format(time.RFC3339),
		},
	}
	work.Put(c)

	s := &sprint.Snapshot{
		Now:  promotionTime.Add(60 * time.Minute),
		Work: work,
	}
	s.Work.SetProp("promoted_at", promotionTime.Format(time.RFC3339))

	// Card without base should be counted
	lag, ok := sprint.DevBehind(s)
	assert.True(t, ok)
	assert.Equal(t, 1, lag.Count)
}
