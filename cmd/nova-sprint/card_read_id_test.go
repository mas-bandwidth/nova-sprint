package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// card <read id> names the primary the read card reads, not "no primary" (docs/CLI.md, the
// card verb): a read card id is answered with the primary it reads, and any other id keeps
// today's answer. readCardPrimaryHint is pure: it reads the card's own "primary" field through
// the lookup it is given, placed or retired.
func TestCardNamesThePrimaryOfAReadCardID(t *testing.T) {
	t.Parallel()
	readID := sprint.ReadCardID("s1-1", 1, "amy")
	lookup := func(id string) *sprint.Card {
		if id == readID {
			return &sprint.Card{ID: id, Fields: map[string]string{"kind": "read", "primary": "s1-1"}}
		}
		return nil
	}
	require.Equal(t, "s1-1", readCardPrimaryHint(readID, lookup), "a read card id names its primary")
	require.Equal(t, "", readCardPrimaryHint("s1-1", lookup), "a primary id is not a read card id")
	require.Equal(t, "", readCardPrimaryHint("s1-9.r1.amy", lookup), "an unknown read card id gives nothing")
}
