package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The readers table counts the read cards each reader has in flight. With read
// cards on (the default, internal/sprint/read_cards.go) a read is a card on the
// fleet table, not a row of the readers table, so the readers rows read zero until
// where adds the fleet's read cards to them: the cards dealt (ready) to a reader
// count in its asked cell and those taken (working) in its reading cell, beside any
// legacy readers-table reads. On epoch 20 where --json's tables.readers[*].reading
// stayed 0 while reads --review showed eight reads reading, and the seat read the
// table as "nothing is being read" (the owner, 2026-10-13). The text frame's
// readers row, the sum over the readers, shows the same counts; the dashboard reads
// the same tables shape and needs no change.
func TestTheReadersTableCountsReadCardsInFlight(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.live = []string{"m1", "m2", "m3"}
	// m1 works the card and holds no reader row; reader-m2 and reader-m3 are the two
	// member readers that may read it (a reader never reads its own work)
	ta.ok("init --readers reader-m2,reader-m3 --members m1:8,m2:8,m3:8")
	ta.readersReadPro() // a fleet row reads flash alone while the store holds routes
	ta.ok("add --stream s1 --count 1 --one --brief-file " + proBriefFile(t))
	ta.ok("start")
	ta.ok("tick")
	ta.ok("tick")                   // the second tick deals, the first only starts the machine
	ta.ok("take --as m1 --limit 1") // the one pro card goes to m1, whose width takes it
	ta.ok("finish --as m1 s1-1.w1@1 --report done")
	ta.ok("tick") // the card is in review: its two reads are dealt, one to each reader

	readers := func() map[string]map[string]any {
		var w whereView
		ta.json("where", &w)
		return w.Tables[sprint.Readers]
	}
	textRow := func() []string {
		lines := strings.Split(strings.TrimRight(tableOf(ta.ok("where --all"), "readers"), "\n"), "\n")
		require.Len(t, lines, 3, "the readers table is one row: %v", lines)
		return cells(lines[2])
	}

	r := readers()
	require.Len(t, r, 2, "one row per reader")
	assert.Equal(t, "1", cellText(r["reader-m2"][sprint.Asked]), "m2's read is dealt")
	assert.Equal(t, "1", cellText(r["reader-m3"][sprint.Asked]), "m3's read is dealt")
	assert.Equal(t, "0", cellText(r["reader-m2"][sprint.Reading]))
	assert.Equal(t, "0", cellText(r["reader-m3"][sprint.Reading]))
	row := textRow()
	assert.Equal(t, allRow, row[0])
	assert.Equal(t, "2", row[1], "the text readers row sums the two asked")
	assert.Equal(t, "0", row[2])

	// m2 takes its read: asked -> reading on its row, the text row's sum follows
	ta.ok("take --as m2 --limit 1")
	ta.ok("tick")
	r = readers()
	assert.Equal(t, "0", cellText(r["reader-m2"][sprint.Asked]), "the taken read leaves m2's asked")
	assert.Equal(t, "1", cellText(r["reader-m2"][sprint.Reading]), "the taken read is in flight on m2's row")
	assert.Equal(t, "1", cellText(r["reader-m3"][sprint.Asked]), "m3's read is untouched")
	row = textRow()
	assert.Equal(t, "1", row[1])
	assert.Equal(t, "1", row[2])

	// m2's verdict takes its read back to 0
	ta.ok("read --as m2 --ok s1-1.r1.m2")
	ta.ok("tick")
	r = readers()
	assert.Equal(t, "0", cellText(r["reader-m2"][sprint.Asked]))
	assert.Equal(t, "0", cellText(r["reader-m2"][sprint.Reading]), "the verdict takes m2's read back to 0")
	row = textRow()
	assert.Equal(t, "1", row[1], "m3's read is still asked")
	assert.Equal(t, "0", row[2])
}
