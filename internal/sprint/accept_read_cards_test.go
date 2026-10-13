package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOkReadCardsCountNotMachines(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	s := &Snapshot{
		Now:     now,
		Work:    NewTable(Work),
		Fleet:   NewTable(Fleet),
		Readers: NewTable(Readers),
		Merge:   NewTable(Merge),
	}
	s.Work.SetRows([]string{"s1"})
	s.Merge.SetRows([]string{"s1"})
	s.Merge.Put(&Card{ID: CtlID("s1"), Row: "s1", Col: StreamWorking, Fields: map[string]string{"state": StreamWorking}})
	s.Fleet.SetRows([]string{"m1"})

	head := "fa6d1991"
	pr := &Card{
		ID:  "p",
		Row: "s1",
		Col: Review,
		Fields: map[string]string{
			"kind":    "primary",
			"stream":  "s1",
			"tier":    "pro", // needs 2 reads
			"attempt": "4",
			"head":    head,
		},
	}
	s.Work.Put(pr)

	// Two ok read cards of one reader (m1) at the head: p.r4.m1 ok, p.r4.m1.g1 ok
	rc1 := &Card{
		ID: "p.r4.m1",
		Fields: map[string]string{
			"kind":       "read",
			"primary":    "p",
			"attempt":    "4",
			"reader":     "m1",
			"head":       head,
			"verdict":    "ok",
			"retired_by": "read",
		},
	}
	rc2 := &Card{
		ID: "p.r4.m1.g1",
		Fields: map[string]string{
			"kind":       "read",
			"primary":    "p",
			"attempt":    "4",
			"reader":     "m1",
			"head":       head,
			"verdict":    "ok",
			"retired_by": "read",
		},
	}
	s.Fleet.Put(rc1)
	s.Fleet.Put(rc2)

	// Distinct read cards count, not machines: need 2, have 2 ok cards
	assert.True(t, acceptable(s, pr), "two ok cards of one reader at the head should be acceptable")

	plan := Accept(s, AcceptReq{Sel: Sel{IDs: []string{"p"}}})
	assert.Empty(t, plan.Refused, "Accept should not refuse card with two ok reads: %v", plan.Refused)
	assert.NotEmpty(t, plan.Units, "Accept should move card to merging")

	have, need, short := ReadsShort(s, pr)
	assert.False(t, short, "ReadsShort should not be short")
	assert.Equal(t, 2, have, "ReadsShort have should be 2")
	assert.Equal(t, 2, need, "ReadsShort need should be 2")
}

func TestTheRefusalNamesEveryRead(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	s := &Snapshot{
		Now:     now,
		Work:    NewTable(Work),
		Fleet:   NewTable(Fleet),
		Readers: NewTable(Readers),
		Merge:   NewTable(Merge),
	}
	s.Work.SetRows([]string{"s1"})
	s.Merge.SetRows([]string{"s1"})
	s.Fleet.SetRows([]string{"m1", "m2", "m3", "m4"})

	head := "h2"
	pr := &Card{
		ID:  "p",
		Row: "s1",
		Col: Review,
		Fields: map[string]string{
			"kind":    "primary",
			"stream":  "s1",
			"tier":    "pro", // needs 2 reads
			"attempt": "4",
			"head":    head,
		},
	}
	s.Work.Put(pr)

	// need 2, one ok at H, one ok at an older head, one retired with no verdict, one placed open:
	// 1. p.r4.m1: ok at H
	rc1 := &Card{
		ID: "p.r4.m1",
		Fields: map[string]string{
			"kind":       "read",
			"primary":    "p",
			"attempt":    "4",
			"reader":     "m1",
			"head":       head,
			"verdict":    "ok",
			"retired_by": "read",
		},
	}
	// 2. p.r4.m2: ok at an older head (h1)
	rc2 := &Card{
		ID: "p.r4.m2",
		Fields: map[string]string{
			"kind":       "read",
			"primary":    "p",
			"attempt":    "4",
			"reader":     "m2",
			"head":       "h1",
			"verdict":    "ok",
			"retired_by": "read",
		},
	}
	// 3. p.r4.m3: retired with no verdict
	rc3 := &Card{
		ID: "p.r4.m3",
		Fields: map[string]string{
			"kind":       "read",
			"primary":    "p",
			"attempt":    "4",
			"reader":     "m3",
			"head":       head,
			"retired_by": "environment",
		},
	}
	// 4. p.r4.m4: placed, still open
	rc4 := &Card{
		ID:  "p.r4.m4",
		Row: "m4",
		Col: Working,
		Fields: map[string]string{
			"kind":    "read",
			"primary": "p",
			"attempt": "4",
			"reader":  "m4",
			"head":    head,
		},
	}
	s.Fleet.Put(rc1)
	s.Fleet.Put(rc2)
	s.Fleet.Put(rc3)
	s.Fleet.Put(rc4)

	plan := Accept(s, AcceptReq{Sel: Sel{IDs: []string{"p"}}})
	require.Len(t, plan.Refused, 1, "Accept should refuse card needing 2 reads with only 1 ok at head")
	refusal := plan.Refused[0].Why

	// The refusal names the counted card and each other card with its reason
	assert.Contains(t, refusal, "p.r4.m1", "refusal should name counted card p.r4.m1")
	assert.Contains(t, refusal, "p.r4.m2 (another head)", "refusal should name card p.r4.m2 with another head")
	assert.Contains(t, refusal, "p.r4.m3 (retired with no verdict)", "refusal should name card p.r4.m3 with retired with no verdict")
	assert.Contains(t, refusal, "p.r4.m4 (open)", "refusal should name placed still-open card p.r4.m4 as open")
}

func TestABrokenReadAtTheHeadStillOutweighs(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	s := &Snapshot{
		Now:     now,
		Work:    NewTable(Work),
		Fleet:   NewTable(Fleet),
		Readers: NewTable(Readers),
		Merge:   NewTable(Merge),
	}
	s.Work.SetRows([]string{"s1"})
	s.Merge.SetRows([]string{"s1"})
	s.Fleet.SetRows([]string{"m1", "m2", "m3"})

	head := "h2"
	pr := &Card{
		ID:  "p",
		Row: "s1",
		Col: Review,
		Fields: map[string]string{
			"kind":    "primary",
			"stream":  "s1",
			"tier":    "pro", // needs 2 reads
			"attempt": "4",
			"head":    head,
		},
	}
	s.Work.Put(pr)

	// two ok cards and one broken at H
	rc1 := &Card{
		ID: "p.r4.m1",
		Fields: map[string]string{
			"kind":       "read",
			"primary":    "p",
			"attempt":    "4",
			"reader":     "m1",
			"head":       head,
			"verdict":    "ok",
			"retired_by": "read",
		},
	}
	rc2 := &Card{
		ID: "p.r4.m2",
		Fields: map[string]string{
			"kind":       "read",
			"primary":    "p",
			"attempt":    "4",
			"reader":     "m2",
			"head":       head,
			"verdict":    "ok",
			"retired_by": "read",
		},
	}
	rc3 := &Card{
		ID: "p.r4.m3",
		Fields: map[string]string{
			"kind":       "read",
			"primary":    "p",
			"attempt":    "4",
			"reader":     "m3",
			"head":       head,
			"verdict":    "broken",
			"retired_by": "read",
		},
	}
	s.Fleet.Put(rc1)
	s.Fleet.Put(rc2)
	s.Fleet.Put(rc3)

	assert.False(t, acceptable(s, pr), "broken read at head outweighs ok reads")

	plan := Accept(s, AcceptReq{Sel: Sel{IDs: []string{"p"}}})
	require.Len(t, plan.Refused, 1, "Accept should refuse when a broken read stands at head")
	assert.True(t, strings.Contains(plan.Refused[0].Why, "broken read") || strings.Contains(plan.Refused[0].Why, "broken verdict"),
		"refusal should mention broken read: %s", plan.Refused[0].Why)
}

// A card accepted on two ok read cards from one reader (m1 and m1.g1) does not bounce
// out of merging: in a fresh snapshot, where retired read cards of a merging card are
// not loaded, ReadsStanding counts each recorded read card id from the readers field,
// so ReadsShort is not short and ShortReadsBack leaves the card in merging.
func TestMergingCardDoesNotBounceWithTwoReadsFromOneReader(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.s.Work.SetRows([]string{"s1"})
	w.s.Merge.SetRows([]string{"s1"})
	w.s.Merge.Put(&Card{ID: CtlID("s1"), Row: "s1", Col: StreamWorking, Fields: map[string]string{"state": StreamWorking}})
	w.s.Fleet.SetRows([]string{"m1"})

	head := "fa6d1991"
	pr := &Card{
		ID:  "p",
		Row: "s1",
		Col: Review,
		Fields: map[string]string{
			"kind":    "primary",
			"stream":  "s1",
			"tier":    "pro", // needs 2 reads
			"attempt": "4",
			"head":    head,
		},
	}
	w.s.Work.Put(pr)

	rc1 := &Card{
		ID: "p.r4.m1",
		Fields: map[string]string{
			"kind":       "read",
			"primary":    "p",
			"attempt":    "4",
			"reader":     "m1",
			"head":       head,
			"verdict":    "ok",
			"retired_by": "read",
		},
	}
	rc2 := &Card{
		ID: "p.r4.m1.g1",
		Fields: map[string]string{
			"kind":       "read",
			"primary":    "p",
			"attempt":    "4",
			"reader":     "m1",
			"head":       head,
			"verdict":    "ok",
			"retired_by": "read",
		},
	}
	w.s.Fleet.Put(rc1)
	w.s.Fleet.Put(rc2)

	w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{"p"}}}))

	mergingCard := w.s.Work.Card("p")
	require.Equal(t, Merging, mergingCard.Col)
	require.Equal(t, "p.r4.m1,p.r4.m1.g1", mergingCard.F("readers"))

	// In a fresh snapshot for a merging card, retired read cards are not loaded
	// (ReadCardExtras only loads read cards for cards in Review).
	s2 := &Snapshot{
		Now:     w.s.Now,
		Work:    NewTable(Work),
		Fleet:   NewTable(Fleet),
		Readers: NewTable(Readers),
		Merge:   NewTable(Merge),
	}
	s2.Work.SetRows([]string{"s1"})
	s2.Merge.SetRows([]string{"s1"})
	s2.Merge.Put(&Card{ID: CtlID("s1"), Row: "s1", Col: StreamWorking, Fields: map[string]string{"state": StreamWorking}})
	s2.Fleet.SetRows([]string{"m1"})
	s2.Work.Put(mergingCard)

	require.Nil(t, s2.Fleet.Card("p.r4.m1"))
	require.Nil(t, s2.Fleet.Card("p.r4.m1.g1"))

	// Assert ReadsShort is not short
	have, need, short := ReadsShort(s2, mergingCard)
	assert.False(t, short, "ReadsShort should not be short for merging card in fresh snapshot")
	assert.Equal(t, 2, have, "have should be 2")
	assert.Equal(t, 2, need, "need should be 2")

	// Assert ShortReadsBack does not bounce the card back to review
	backPlan := ShortReadsBack(s2, "coordinator")
	assert.Empty(t, backPlan.Units, "merging card should not bounce back to review")
}
