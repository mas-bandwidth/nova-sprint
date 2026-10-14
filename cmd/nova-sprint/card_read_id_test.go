package main

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// readCardPrimaryHint names the primary a read card id reads, from the card's
// own primary field, placed or retired; anything else is "" (a-readers-second-
// verdict-answers-already-recorded).
func TestCardNamesThePrimaryOfAReadCardID(t *testing.T) {
	t.Parallel()
	readID := "s1-1.r1.amy.g2"
	primary := "s1-1"
	lookup := func(id string) *sprint.Card {
		switch id {
		case readID:
			return &sprint.Card{ID: readID, Fields: map[string]string{"kind": "read", sprint.PrimaryField: primary}}
		case "s2-1":
			return &sprint.Card{ID: "s2-1", Fields: map[string]string{"kind": "work", sprint.PrimaryField: "s2-1"}}
		default:
			return nil
		}
	}
	require.Equal(t, primary, readCardPrimaryHint(readID, lookup))
	require.Equal(t, "", readCardPrimaryHint("unknown", lookup))
	require.Equal(t, "", readCardPrimaryHint("s2-1", lookup))
	require.Equal(t, "", readCardPrimaryHint("", lookup))
}
