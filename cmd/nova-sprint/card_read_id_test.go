package main

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// TestCardNamesThePrimaryOfAReadCardID pins readCardPrimaryHint (card
// a-readers-second-verdict-answers-already-recorded; docs/SPEC-SPRINT.md, a
// friend's read): a read card id names its primary; an unknown id does not.
func TestCardNamesThePrimaryOfAReadCardID(t *testing.T) {
	t.Parallel()
	const id = "s1-1.r1.amy"
	const primary = "s1-1"
	cards := map[string]*sprint.Card{
		id: {ID: id, Fields: map[string]string{"kind": "read", "primary": primary}},
	}
	lookup := func(want string) *sprint.Card { return cards[want] }
	got := readCardPrimaryHint(id, lookup)
	require.Equal(t, "card: "+id+" is a read card of "+primary+"; run: nova-sprint card "+primary, got)
	require.Equal(t, "", readCardPrimaryHint("no-such-card", lookup))
}
