package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAMergingCardIsHeldToTheMixCountItsAcceptRecorded: under the read mix a card accepted on
// the count its snapshot gave (two readers for it) is held to that count in merging, whatever
// pool a later snapshot counts (the lander's load carries no friend seats, reader states or
// routes; a later tick's pool moves as reads are dealt and taken back). Before, the pool was
// recounted past the accept, and on 2026-11-05 a card accepted on two ok reads was refused
// "from 2 of the 3 ok reads it needs" and sent back to review, every tick (tla/ReadMix.tla
// MergingHasLandReads, reversed witness "landpool").
func TestAMergingCardIsHeldToTheMixCountItsAcceptRecorded(t *testing.T) {
	t.Parallel()
	w := readCardsWorld(t, 4, "m1", "m2")
	readersReadEveryTier(w)
	w.s.Work.SetProp("read_mix", "friends=2,fleet=1")
	putReviewBy(w, "s1-1", "s1-1: work (s1) tier: pro\n", "zoe", 1)
	pr := w.s.Work.Card("s1-1")
	require.Equal(t, 2, ReadsNeededIn(w.s, pr), "the accept's snapshot: two readers for it")

	// accepted on two: merging, the count recorded as the accept records it
	w.place(w.s.Work, "s1-1", pr.Row, Merging)
	pr = w.s.Work.Card("s1-1")
	pr.Fields[FieldReadsNeeded] = "2"

	// another snapshot counts two friends more in the pool
	w.s.Friends = workerSeatFixtures([]FriendSeat{
		readerSeat("alex", 4, []string{"pro"}, []string{"reader"}),
		readerSeat("pat", 4, []string{"pro"}, []string{"reader"}),
	})
	mix, on := w.s.ReadMixSet()
	require.True(t, on)
	require.Equal(t, 3, readMixNeed(w.s, pr, mix), "the pool recounted: three")
	require.Equal(t, 2, ReadsNeededIn(w.s, pr), "merging: the count its accept recorded, not the pool recounted")

	// the tier's rule raised after the accept still binds (v1.2.6, tla/Land.tla NoLandWithoutReads)
	pr.Fields[FieldReadsNeeded] = "1"
	require.Equal(t, 2, ReadsNeededIn(w.s, pr), "a pro card's floor of two stands over a smaller count")

	// a card in review is counted on the pool, as before
	w.place(w.s.Work, "s1-1", pr.Row, Review)
	require.Equal(t, 3, ReadsNeededIn(w.s, w.s.Work.Card("s1-1")), "review: the pool")
}
