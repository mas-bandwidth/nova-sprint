package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestAReadAskedTheOldWayKeepsItsInboxPath pins the install with live reads (PR 5392's cold
// read, finding 1): a friend's read asked before read cards (no read_deadline: the friends'
// read ask's card) is delivered, queued and closed at inbox/<card id> and outbox/<card id>
// at every epoch, as it was, so friend sync finds the BRIEF.md already there and delivers
// nothing again; a read card (with its read_deadline) has a card's job.
func TestAReadAskedTheOldWayKeepsItsInboxPath(t *testing.T) {
	t.Parallel()
	id := sprint.ReadCardID("dep-postgres-b", 3, "zhi")
	old := &sprint.Card{ID: id, Row: sprint.FriendRow("zhi"), Col: sprint.Working, Fields: map[string]string{"kind": "read", "primary": "dep-postgres-b", "attempt": "3", "reader": "zhi", "gen": "1"}}
	p := sprint.PacketOf("t-", 15, old, nil, nil, nil)
	require.Equal(t, id, friendJobOf(p), "an old read keeps its card id as its job")
	card := &sprint.Card{ID: id, Row: old.Row, Col: old.Col, Fields: map[string]string{"kind": "read", "primary": "dep-postgres-b", "attempt": "3", "reader": "zhi", "gen": "1", sprint.FieldReadCard: "1"}}
	require.Equal(t, sprint.StoredID(id, 15), friendJobOf(sprint.PacketOf("t-", 15, card, nil, nil, nil)), "a read card has a card's job")
}
