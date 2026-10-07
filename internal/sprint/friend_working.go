package sprint

// Verified working (card sn-verified-working-b-ns-b.w1; docs/SPEC-SPRINT.md section 1,
// "Verified working"; the owner: "trust but VERIFY", "I want to trust the ok%",
// "Mechanical. You know the drill.", "Are they actually doing the work that is shown in
// the friend table? Really?", "i don't want dollar amounts for friends. token counts
// are fine."): the friends table counts a friend's working only on her cards someone can
// verify, a push on its branch or her beat naming it running, and dealt beside it counts
// every card dealt to her row in working, so the two differ exactly when she holds cards
// no push proves and her beat does not name. The verification is read outside the
// tables, as friend take reads it (FriendTakeReq.Started, built by the command: one
// git ls-remote a card and her last beat), and the counting is this package's, so the
// view, the pass and the verbs read one rule.

// FriendWorking is one friend's dealt and working counts, the friends table's dealt and
// working columns: Dealt every work card of hers in working on her row, Working the
// verified of them, a card with a push on its branch or named in her beat --running.
type FriendWorking struct {
	Dealt   int `json:"dealt"`
	Working int `json:"working"`
}

// FriendWorkingOf is the dealt and working of the cards in working on a friend's row:
// dealt is all of them, working each one the started map names, the work cards she has
// started as the caller read them (a push on the card's branch, or her beat naming it
// running: FriendTakeReq.Started, read outside the tables). A card of another friend's
// row the map names verifies nothing of hers: only the cards given count, and a card it
// names both ways counts once, as one entry.
func FriendWorkingOf(cards []*Card, started map[string]string) FriendWorking {
	w := FriendWorking{Dealt: len(cards)}
	for _, c := range cards {
		if started[c.ID] != "" {
			w.Working++
		}
	}
	return w
}
