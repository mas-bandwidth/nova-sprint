package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The three first-install cases (the owner, 2026-10-11: "The person installing
// nova-sprint for the first time must get smart defaults that WORK"): a fresh
// store, and every store after a clear, asks every read a card in review needs
// as read cards in the one tick, and the deal fills each member to its width,
// with no set run at all. The reads are read cards dealt to the members' reader
// rows (reader-m1, reader-m2, reader-m3), never the readers table.

// proBrief writes a pro card's brief, one that passes the card lint, named id.
func proBrief(t *testing.T, id string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, id+".md")
	require.NoError(t, os.WriteFile(path, []byte(passingBrief("RESULT: c sha=0123456789ab tier: pro\nPATHS: internal/x/*.go\nTEST: internal/x TestY\n\nTHE TASK. Fix x.")), 0o644))
	return path
}

// requireReadCards says the card in review was read by the reads its pro tier
// needs, all as read cards on the fleet table, not reads through the readers
// table.
func requireReadCards(t *testing.T, ta *testApp, id string) {
	t.Helper()
	var card cardView
	ta.json("card "+id, &card)
	require.Len(t, card.Reads, 2, "a pro card's two reads are asked in the one tick, read cards not one per tick through the readers table")
	for _, r := range card.Reads {
		require.Equal(t, "1", r.F("read_card"), "read %s is a read card, not a readers-table read", r.ID)
	}
}

// TestAFirstInstallAsksEveryReadInOneTick pins the owner's first case: a first
// install (nova-sprint init on a fresh store, no set run) asks every read a pro
// card in review needs in the one tick, as read cards on the fleet table, not one
// per tick through the readers table.
func TestAFirstInstallAsksEveryReadInOneTick(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.defaults = true
	ta.live = []string{"m1", "m2", "m3"}
	ta.ok("init --readers reader-m1,reader-m2,reader-m3 --members m1:2,m2:2,m3:2")
	ta.ok("add --stream s1 --one --brief-file " + proBrief(t, "s1-1"))
	ta.ok("start")
	ta.ok("tick")
	ta.ok("tick")
	ta.ok("take --as m1 --limit 1")
	ta.ok("finish --as m1 s1-1.w1@1 --report done")
	ta.ok("tick")
	requireReadCards(t, ta, "s1-1")
	ta.clean()
}

// TestAFirstInstallFillsBothMembersToTheirFullWidth pins the width half of the
// first case: the deal fills each member to its width (2) on a fresh store.
func TestAFirstInstallFillsBothMembersToTheirFullWidth(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.defaults = true
	ta.ok("init --readers reader-m1,reader-m2 --members m1:2,m2:2")
	ta.ok("add --stream s1 --count 4")
	ta.ok("start")
	ta.ok("tick")
	var w whereView
	ta.json("where", &w)
	require.Equal(t, "2", w.Tables["fleet"]["m1"]["ready"], "the deal fills m1 to its full width")
	require.Equal(t, "2", w.Tables["fleet"]["m2"]["ready"], "the deal fills m2 to its full width")
	ta.clean()
}

// TestAfterAClearReadsAreAskedAtOnceAgain pins the owner's second case: a store
// whose settings were all changed (read cards off, reads 2, dealt-max, go-lanes,
// the alarms, fleet off) comes back from a clear to the working defaults, so the
// tick asks every read at once again.
func TestAfterAClearReadsAreAskedAtOnceAgain(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.defaults = true
	ta.live = []string{"m1", "m2", "m3"}
	ta.ok("init --readers reader-m1,reader-m2,reader-m3 --members m1:2,m2:2,m3:2")
	ta.ok("set --read-cards off --reads 2 --dealt-max 1h --go-lanes 3 --alarm-review 1 --fleet off")
	ta.ok("clear --confirm sprint")
	ta.ok("reader add reader-m1 reader-m2 reader-m3")
	ta.ok("add --stream s1 --one --brief-file " + proBrief(t, "s1-1"))
	ta.ok("start")
	ta.ok("tick")
	ta.ok("tick")
	ta.ok("take --as m1 --limit 1")
	ta.ok("finish --as m1 s1-1.w1@1 --report done")
	ta.ok("tick")
	requireReadCards(t, ta, "s1-1")
	ta.clean()
}

// TestAnAbsentReadCardsPropertyBehavesAsOn pins the owner's third case: a store
// whose read-cards property is absent (never set) behaves as on.
func TestAnAbsentReadCardsPropertyBehavesAsOn(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.defaults = true
	ta.live = []string{"m1", "m2", "m3"}
	ta.ok("init --readers reader-m1,reader-m2,reader-m3 --members m1:2,m2:2,m3:2")
	ta.ok("add --stream s1 --one --brief-file " + proBrief(t, "s1-1"))
	ta.ok("start")
	ta.ok("tick")
	ta.ok("tick")
	ta.ok("take --as m1 --limit 1")
	ta.ok("finish --as m1 s1-1.w1@1 --report done")
	ta.ok("tick")
	requireReadCards(t, ta, "s1-1")
	ta.clean()
}
