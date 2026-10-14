package main

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The findings a reader met on the v1.2.22 binary (the cold-reader run):
// where with no store chosen silently dialed the seat store and its first line
// named no store, so a refusal read like a defect in a store the reader never
// chose; the friends table padded every row with the whole held reason in its
// status cell; and the store refusal named the wrong flag for the tool the
// reader runs. This pins the first line naming the store and its source, the
// cell short with the held reason below the tables, and the refusal naming
// --redis.
func TestWhereNamesItsStoreAndKeepsHeldReasonsBelowTheTable(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.ok("friend sync")
	ta.pong("amy")

	// a held friend with a long reason: the cell keeps the word, the reason goes below
	reason := strings.Repeat("held-reason ", 20)
	out := ta.ok("friend hold amy --reason '" + reason + "'")
	require.Contains(t, out, "HOLD OK", out)

	frame := ta.ok("where")
	lines := strings.Split(frame, "\n")
	require.NotEmpty(t, lines)
	// the first line names the store it chose and where the choice came from
	assert.Contains(t, lines[0], "mem:0", "the first line names the store: %q\n%s", lines[0], frame)
	assert.Contains(t, lines[0], "NOVA_SPRINT_REDIS", "the first line names the source: %q\n%s", lines[0], frame)

	// a held friend's reason does not appear in the table's status column
	friends := tableOf(frame, sprint.Friends)
	require.NotEmpty(t, friends, "the frame draws the friends table:\n%s", frame)
	assert.NotContains(t, friends, reason, "the status cell keeps the reason out of the table:\n%s", friends)
	var v whereView
	ta.json("where", &v)
	assert.Equal(t, sprint.Held, cellText(v.Tables[sprint.Friends]["amy"]["status"]), "the cell is the word alone")

	// and it appears under held friends:
	i := strings.Index(frame, "held friends:")
	require.GreaterOrEqual(t, i, 0, "the frame holds a held friends block:\n%s", frame)
	assert.Contains(t, frame[i:], reason, "the held reason is below the table:\n%s", frame[i:])

	// the refusal when no store is named names the verb's own flag, never --addr
	code, _, errs := ta.do("where --redis ''")
	require.NotEqual(t, 0, code, "where with no store refuses: %s", errs)
	assert.Contains(t, errs, "--redis", "the refusal names --redis: %s", errs)
	assert.NotContains(t, errs, "--addr", "the refusal never names --addr: %s", errs)
}
