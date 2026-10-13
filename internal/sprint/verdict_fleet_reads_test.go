package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The readers' verdict on a worker's finish (TickVerdicts) counts the reads the accept counts
// (okReaders, hasBrokenAtHead), from the one function both read (standingReads), whichever
// table the reads are on. Stella's finding of 2026-10-11: the verdicts read the readers table
// alone, so a card accepted on two fleet read cards (retired off their rows with their
// verdicts: no row, no column) left its worker's card in finished, and friend stella's done
// and ok% read 0 and 0.0% while the log recorded her finishes ok and their reads ok.

const (
	verdictHead = "b5acd15a8d62b117458148e1ddb7b261ddb4fe9d"
	otherHead   = "1111111111111111111111111111111111111111"
)

// finishedBy puts primary id in review at attempt 1 and head verdictHead, of the tier the
// brief names, and its work card in the finished cell of row, the worker's own word ok.
func finishedBy(w *world, id, brief, row string) {
	w.t.Helper()
	putReview(w, id, brief, 1, 1, verdictHead)
	putAttemptWork(w, id, 1, "sprint/"+id, verdictHead, "yes")
	wc := w.s.Fleet.Card(WorkCardID(id, 1))
	wc.Fields["member"], wc.Fields["finished"] = row, stamp(w.s.Now)
	if !w.s.Fleet.HasRow(row) {
		w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), row))
	}
	w.place(w.s.Fleet, wc.ID, row, Finished)
}

// retiredFleetRead is a read card of the fleet table retired with its verdict, as the read
// verb leaves it (friend_read.go, readCardEnd): on no row and in no column, its reader in
// the reader field and its id.
func retiredFleetRead(w *world, primary, reader, head, verdict string) *Card {
	w.t.Helper()
	c := pinnedToHeadRead(ReadCardID(primary, 1, reader), primary, 1, "", "", reader, head, verdict, "")
	c.Fields[FieldReadCard], c.Fields["retired"], c.Fields["retired_by"] = "1", stamp(w.s.Now), "read"
	w.s.Fleet.Put(c)
	return c
}

// doneCells is the row's finished, ok and failed counts: done is ok+failed (the table's formula).
func doneCells(w *world, row string) [3]int {
	return [3]int{w.s.Fleet.Count(row, Finished), w.s.Fleet.Count(row, DoneOK), w.s.Fleet.Count(row, DoneFailed)}
}

// A pro card a friend finished, accepted on two ok fleet reads from two members (b3 and
// hetzner1, retired: empty row and column): the verdicts move her work card to ok, as the
// accept counted the reads. The same of a member's work. Red before the fix: both stayed in
// finished (readsAt reads the readers table alone).
func TestTheVerdictCountsTheFleetReadsTheAcceptCounts(t *testing.T) {
	t.Parallel()
	for _, worker := range []string{FriendRow("stella"), "m1"} {
		t.Run(worker, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			finishedBy(w, "s1-1", "s1-1: the work (s1) tier: pro\n", worker)
			retiredFleetRead(w, "s1-1", "b3", verdictHead, "ok")
			retiredFleetRead(w, "s1-1", "hetzner1", verdictHead, "ok")
			pr := w.s.Work.Card("s1-1")
			require.Equal(t, 2, ReadsNeededIn(w.s, pr))
			require.Len(t, okReaders(w.s, pr), 2, "the accept counts both fleet reads")
			require.True(t, acceptable(w.s, pr))

			w.part(TickVerdicts, TickReq{})
			assert.Equal(t, [3]int{0, 1, 0}, doneCells(w, worker), "finished, ok, failed: the readers' verdict is ok, done 1 and ok%% 100")
			assert.Equal(t, DoneOK, w.s.Fleet.Card("s1-1.w1").Col)
		})
	}
}

// Two different readers whose retired reads both have an empty row are two readers, never
// one: the reads are counted by the stable reader each names, never by the row (the old
// readVerdict keyed them by rc.Row, which a retired read has empty). One reader read twice is
// still one reader, and a pro card's verdict waits for the second.
func TestTwoRetiredFleetReadsWithEmptyRowsAreTwoReaders(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	finishedBy(w, "s1-1", "s1-1: the work (s1) tier: pro\n", FriendRow("stella"))
	a := retiredFleetRead(w, "s1-1", "b3", verdictHead, "ok")
	require.Empty(t, a.Row)
	pr := w.s.Work.Card("s1-1")

	require.Len(t, okReaders(w.s, pr), 1)
	require.False(t, hasBrokenAtHead(w.s, pr))
	w.part(TickVerdicts, TickReq{})
	assert.Equal(t, [3]int{1, 0, 0}, doneCells(w, FriendRow("stella")), "one reader of the two a pro card needs: no verdict yet")

	b := retiredFleetRead(w, "s1-1", "hetzner1", verdictHead, "ok")
	require.Empty(t, b.Row)
	oks := okReaders(w.s, pr)
	require.Len(t, oks, 2, "two readers, both rows empty")
	assert.ElementsMatch(t, []string{"b3", "hetzner1"}, []string{oks[0].F("reader"), oks[1].F("reader")})
	w.part(TickVerdicts, TickReq{})
	assert.Equal(t, [3]int{0, 1, 0}, doneCells(w, FriendRow("stella")))
}

// A broken fleet read at the head is the failed verdict, as the accept refuses on it; one
// pinned to another head stands for nothing, and an ok read at another head is no read.
func TestABrokenFleetReadAtTheHeadIsTheFailedVerdict(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	finishedBy(w, "s1-1", "s1-1: the work (s1) tier: flash\n", FriendRow("stella"))
	retiredFleetRead(w, "s1-1", "b3", otherHead, "broken")
	retiredFleetRead(w, "s1-1", "hetzner1", otherHead, "ok")
	pr := w.s.Work.Card("s1-1")
	require.False(t, hasBrokenAtHead(w.s, pr))
	w.part(TickVerdicts, TickReq{})
	assert.Equal(t, [3]int{1, 0, 0}, doneCells(w, FriendRow("stella")), "reads of another head: no verdict")

	retiredFleetRead(w, "s1-1", "vision", verdictHead, "broken")
	require.True(t, hasBrokenAtHead(w.s, pr))
	w.part(TickVerdicts, TickReq{})
	assert.Equal(t, [3]int{0, 0, 1}, doneCells(w, FriendRow("stella")), "a broken read at the head: failed, done 1 and ok%% 0")
}

// A primary the accept took at its attempt is ok work whatever reads a snapshot still loads:
// the accept read them (Accept records the readers), and the lander holds them standing
// (ReadsShort). The verdict and the accept never disagree on it.
func TestAnAcceptedPrimarysWorkIsCountedOk(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	finishedBy(w, "s1-1", "s1-1: the work (s1) tier: pro\n", FriendRow("stella"))
	retiredFleetRead(w, "s1-1", "b3", verdictHead, "ok")
	retiredFleetRead(w, "s1-1", "hetzner1", verdictHead, "ok")
	w.s.Merge.SetRows([]string{"s1"})
	w.s.Merge.Put(&Card{ID: CtlID("s1"), Row: "s1", Col: Ctl, Rev: 1, Fields: map[string]string{"kind": "ctl", "state": StreamWaiting}})
	w.must(Accept(w.s, AcceptReq{Sel: Sel{IDs: []string{"s1-1"}}, Who: "seat"}))
	pr := w.s.Work.Card("s1-1")
	require.Equal(t, Merging, pr.Col)
	require.Equal(t, "s1-1.r1.b3,s1-1.r1.hetzner1", pr.F("readers"))
	w.part(TickVerdicts, TickReq{})
	assert.Equal(t, [3]int{0, 1, 0}, doneCells(w, FriendRow("stella")))
}

// The readers table's reads count as they did: a flash card's one ok read at the head is ok
// work, and an ok read of another head is no read (the accept's rule, okReaders).
func TestTheReadersTablesReadsStillCount(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	finishedBy(w, "s1-1", "s1-1: the work (s1) tier: flash\n", "m1")
	w.s.Readers.Put(pinnedToHeadRead(ReadCardID("s1-1", 1, "reader-a"), "s1-1", 1, "reader-a", OK, "reader-a", otherHead, "ok", ""))
	w.part(TickVerdicts, TickReq{})
	assert.Equal(t, [3]int{1, 0, 0}, doneCells(w, "m1"), "an ok read of another head is no read")
	w.s.Readers.Put(pinnedToHeadRead(ReadCardID("s1-1", 1, "reader-b"), "s1-1", 1, "reader-b", OK, "reader-b", verdictHead, "ok", ""))
	w.part(TickVerdicts, TickReq{})
	assert.Equal(t, [3]int{0, 1, 0}, doneCells(w, "m1"))
}
