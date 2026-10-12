package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A card hard-pinned to a friend (WHO: only friend) that reaches its attempt cap is
// offered to no friend and dealt to no machine, so the deal's friend skip must not come
// before the cap's judgment: the fourth ended attempt raises the one decision the
// coordinator owes, whoever the card is pinned to (brief_bound.go; docs/SPEC-SPRINT.md,
// the bounds). Found by a cold read's probe: the lane-capped finish readied the card on a
// new tier, three deals ran with the friend up and room free, no fifth attempt was dealt,
// and no judgment was ever raised.
func TestACappedCardPinnedToAFriendStillRaisesTheJudgment(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.s.Work.SetRows([]string{"s1"})
	w.s.Fleet.SetRows([]string{FriendRow("bob")})
	w.s.Work.Put(&Card{ID: "p1", Row: "s1", Col: Working, Fields: map[string]string{
		"kind": "primary", "stream": "s1", "work": "p1.w4", "attempt": "4", FieldWho: "only.friend.bob", "brief": "p1: a card\nWHO: only friend bob\n\nThe task.",
	}})
	w.s.Fleet.Put(&Card{ID: "p1.w4", Row: FriendRow("bob"), Col: Working, Fields: map[string]string{
		"kind": "work", "primary": "p1", "stream": "s1", "attempt": "4", "gen": "1", "member": FriendRow("bob"),
	}})
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"p1.w4"}}, As: FriendRow("bob"), Gens: map[string]int{"p1.w4": 1}, Failed: true, Report: "capped at 2m0s (tier flash, overrun 3s)"}))
	require.Equal(t, Ready, w.s.Primary("p1").Col, "the lane cap readies the card on another tier")
	require.True(t, OnlyFriend(w.s.Primary("p1")))
	for range 3 {
		p, _ := TickDeal(w.s, TickReq{Friends: []FriendSeat{{Name: "bob", Status: Up, Width: 2, Class: "flash,pro"}}})
		w.must(p)
	}
	assert.Nil(t, w.s.Fleet.Card("p1.w5"), "no fifth attempt")
	var judgments []Note
	for _, n := range w.notes {
		if n.Type == NBriefWrong {
			judgments = append(judgments, n)
		}
	}
	assert.Len(t, judgments, 1, "the fourth ended attempt raises one decision for the coordinator, pinned to a friend or not")
}
