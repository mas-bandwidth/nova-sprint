package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// where names a member that drains: the no-room word a fresh beat that says
// --draining carries (sprint.DrainWord) is shown beside its up status, as the
// deal already reads it (Snapshot.NoRoom; drain.go; tla/NoRoom.tla,
// NeverDealtToDraining). Before this the word was hidden and vision read up
// through a whole drain.
func TestWhereNamesADrainingMember(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1:1,m2:1")
	ta.ok("add --stream s1 --count 2")
	ta.ok("start")
	ta.ok("tick")

	// (a) a ready card is dealt to m1, then it says it drains: the tick returns
	// the card it was dealt and had not taken, and deals it nothing again. Green
	// before this change: it pins the contract at the verbs (a later change to
	// the word or its parse fails here).
	var q struct{ Cards []queueCard }
	ta.json("queue --as m1", &q)
	require.Len(t, q.Cards, 1, "m1's ready card: %+v", q.Cards)
	card := q.Cards[0].ID

	ta.ok("fleet beat m1 --draining")
	ta.mu.Lock()
	ta.live = []string{"m2"} // m1 stops beating: the drain beat stands, fresh at the tick
	ta.mu.Unlock()
	ta.ok("tick")
	ta.json("queue --as m1", &q)
	require.Empty(t, q.Cards, "a draining member is dealt nothing and its untaken deal returned")
	var qm2 struct{ Cards []queueCard }
	ta.json("queue --as m2", &qm2)
	found := false
	for _, c := range qm2.Cards {
		if c.ID == card {
			found = true
		}
	}
	assert.True(t, found, "the returned card %s reaches m2: %+v", card, qm2.Cards)

	// (b) where's text names m1 draining with its since stamp beside its up status
	text := ta.ok("where --all")
	assert.Contains(t, text, "draining since", "where's text names the drain:\n%s", text)
	assert.Contains(t, text, "2030-01-02T03:04:05Z", "the since stamp is beside it:\n%s", text)
	assert.Contains(t, text, "up (", "the word is beside its up status:\n%s", text)

	// (c) where --json's row for m1 carries draining_since (RFC 3339) and its no-room word
	var w whereView
	ta.json("where", &w)
	row := w.Tables["fleet"]["m1"]
	require.NotNil(t, row, "where --json's fleet row for m1: %+v", w.Tables["fleet"])
	since, _ := row["draining_since"].(string)
	require.NotEmpty(t, since, "the row carries draining_since: %+v", row)
	_, err := time.Parse(time.RFC3339, since)
	require.NoError(t, err, "draining_since is RFC 3339: %q", since)
	assert.Contains(t, row["no_room"], "draining since", "the row carries the no-room word: %+v", row)

	// (d) a plain beat of m1 afterwards clears both from where (the beat without
	// the word clears it; the tick's fleet mirror writes the bare status again)
	ta.ok("fleet beat m1")
	ta.ok("tick")
	text = ta.ok("where --all")
	assert.NotContains(t, text, "draining since", "a plain beat clears the word:\n%s", text)
	ta.json("where", &w)
	row = w.Tables["fleet"]["m1"]
	require.NotNil(t, row, "where --json's fleet row for m1")
	assert.NotContains(t, row, "draining_since", "a plain beat clears the stamp: %+v", row)
	assert.Empty(t, row["no_room"], "a plain beat clears the word: %+v", row)
}

// The verbs take the flag nova-tools' member sends: fleet beat --draining and
// queue --as <reader> --draining (pkg/member Drain, drainArgs; nova-tools
// 5206eae87). A rename on this side fails here first.
func TestDrainingVerbFlagsParse(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1:1,m2:1")
	ta.ok("add --stream s1 --count 1 --one")
	ta.ok("start")

	out := ta.ok("fleet beat m1 --draining")
	assert.Contains(t, out, "draining_since=", "fleet beat takes --draining: %s", out)
	ta.ok("queue --as reader-a --draining")
	help := ta.ok("fleet beat -h")
	assert.Contains(t, help, "--draining", "fleet beat names --draining: %s", help)
	help = ta.ok("queue -h")
	assert.Contains(t, help, "--draining", "queue names --draining: %s", help)
}
