package main

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// TestCardNamesThePrimaryOfAReadCardID pins readCardPrimaryHint: a read card
// id whose card names primary P is answered with the primary it reads, while
// any other id keeps the caller's line (this card,
// a-readers-second-verdict-answers-already-recorded; docs/CLI.md, the card
// verb: a read card id is answered with the primary it reads).
func TestCardNamesThePrimaryOfAReadCardID(t *testing.T) {
	t.Parallel()
	read := &sprint.Card{ID: "s1-1.r1.amy", Fields: map[string]string{"kind": "read", "primary": "s1-1"}}
	lookup := func(id string) *sprint.Card {
		if id == read.ID {
			return read
		}
		return nil
	}
	require.Equal(t, "card: s1-1.r1.amy is a read card of s1-1; run: nova-sprint card s1-1",
		readCardPrimaryHint(read.ID, lookup))
	require.Empty(t, readCardPrimaryHint("s1-1", lookup), "a primary id keeps the caller's line")
	require.Empty(t, readCardPrimaryHint("s1-1.r1.bob", lookup), "an unknown read id keeps the caller's line")
	require.Empty(t, readCardPrimaryHint("not-a-read", lookup), "an id that is no read card keeps the caller's line")
}
