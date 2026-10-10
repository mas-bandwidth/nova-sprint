package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// No card lands short of its reads (tla/Land.tla NoLandWithoutReads). On the Studio sprint
// of 2026-10-10 (epoch 16) cards accepted on one read were merging when they needed two (the
// accept had recorded no count, and the tier's rule recomputed past review gave two once the
// tier was pinned pro), check raised rule 6, and the lander landed two of them on one read:
// nothing past review counted the reads.

// oneReadMerging is tierWorld with its flash card s1-1 accepted by the tick on its one ok
// read, under the default setting.
func oneReadMerging(t *testing.T) *world {
	t.Helper()
	w := tierWorld(t)
	w.must(Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	reads := liveReadsAt(w.s, w.s.Work.Card("s1-1"), 1)
	require.Len(t, reads, 1, "a flash card is asked of one reader")
	w.must(Read(w.s, ReadReq{Usage: "input=1000 output=100", As: reads[0].Row, Verdict: "ok", Sel: Sel{IDs: []string{reads[0].ID}}}))
	p, _ := TickAccept(w.s, TickReq{})
	w.must(p)
	require.Equal(t, Merging, w.s.Work.Card("s1-1").Col)
	return w
}

// The accept records the count it accepted on whether or not it is the tier's rule, so no
// count is left to the tier's rule recomputed after.
func TestTheAcceptRecordsItsReadsCountAlways(t *testing.T) {
	t.Parallel()
	w := oneReadMerging(t)
	assert.Equal(t, "1", w.s.Work.Card("s1-1").F(FieldReadsNeeded), "accepted on one read, the tier's rule")
}

// A merging card short of its reads (the setting raised to two, or its tier pinned pro, after
// its accept) is refused by the lander's record, naming it and its count, and the tick sends
// it back to review, not marked returned, for the read it lacks: no judgment, no repair. The
// read it gets there and the tick accepts it again on two.
func TestAMergingCardShortOfItsReadsGoesBackToReview(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		raise func(w *world)
	}{
		{"the setting raised to two", func(w *world) { w.s.Work.SetProp(PropReadsNeeded, "2") }},
		{"its tier pinned pro", func(w *world) { w.s.Work.Card("s1-1").Fields[FieldTier] = "pro" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := oneReadMerging(t)
			tc.raise(w)
			pr := w.s.Work.Card("s1-1")
			assert.Equal(t, 2, ReadsNeededIn(w.s, pr), "a merging card needs what a card in review needs")
			assert.True(t, rule6(w.s), "check: merging on one read of two")

			// what the lander refuses it with (land.go readsWhy): the card and its count
			assert.Equal(t, "s1-1 has ok reads at head "+pr.F("head")+" from 1 of the 2 readers it needs", ReadsShortWhy(w.s, pr))

			// the tick's pump: back to review, off the merge queue, never held for the seat
			p, _ := TickAccept(w.s, TickReq{})
			require.NotEmpty(t, p.Units)
			assert.Contains(t, p.Units[0].Moved, "s1-1 merging -> review")
			w.must(p)
			pr = w.s.Work.Card("s1-1")
			assert.Equal(t, Review, pr.Col)
			assert.Equal(t, Returned, w.s.Merge.Placed("s1-1").Col)
			assert.Empty(t, AcceptHeld(pr), "not marked returned: the pump accepts it again on its reads")
			assert.Empty(t, pr.F(FieldReadsNeeded))
			assert.False(t, rule6(w.s))
			w.clean("back in review for the read it lacks")

			// the second read, and the tick accepts it again on two
			w.must(Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}}))
			for _, rc := range liveReadsAt(w.s, pr, 1) {
				if rc.Col != OK {
					w.must(Read(w.s, ReadReq{Usage: "input=1000 output=100", As: rc.Row, Verdict: "ok", Sel: Sel{IDs: []string{rc.ID}}}))
				}
			}
			p, _ = TickAccept(w.s, TickReq{})
			w.must(p)
			pr = w.s.Work.Card("s1-1")
			assert.Equal(t, Merging, pr.Col)
			assert.Equal(t, "2", pr.F(FieldReadsNeeded))
			assert.False(t, rule6(w.s))
		})
	}
}

// A card pushed and not reported is on the base already: short of its reads or not, the tick
// never sends it back to review (the lander records it), so its receipt is never cleared
// for a card git holds (tla/Land.tla PushedNeverSentBack).
func TestAPushedUnreportedCardIsNotSentBack(t *testing.T) {
	t.Parallel()
	w := oneReadMerging(t)
	pr := w.s.Work.Card("s1-1")
	w.must(MarkPushedUnreported(w.s, pr.Row, "abc1234", []PushedPin{{ID: "s1-1", Head: pr.F("head"), Attempt: pr.F("attempt")}}))
	require.True(t, PushedUnreportedMatches(w.s, "s1-1"))
	w.s.Work.SetProp(PropReadsNeeded, "2")
	p, _ := TickAccept(w.s, TickReq{})
	assert.Empty(t, p.Units, "pushed: never back to review")
	assert.Equal(t, Merging, w.s.Work.Card("s1-1").Col)
}

// A merging card with its reads is left where it is, and a landed card is held to the count
// it landed on: a setting raised after it landed does not make it short.
func TestACardWithItsReadsIsNotSentBackAndLandedKeepsItsCount(t *testing.T) {
	t.Parallel()
	w := oneReadMerging(t)
	p, _ := TickAccept(w.s, TickReq{})
	assert.Empty(t, p.Units, "one read of one: nothing to send back")
	pr := w.s.Work.Card("s1-1")
	p = MergeStep(w.s, MergeReq{Stream: pr.Row, Landed: []LandedPin{{ID: "s1-1", Head: pr.F("head"), InBase: true}}, Who: "lander"})
	require.Empty(t, p.Refused)
	w.must(p)
	pr = w.s.Work.Card("s1-1")
	require.Equal(t, Landed, pr.Col)
	assert.Equal(t, "1", pr.F(FieldReadsNeeded), "the count it landed on")
	w.s.Work.SetProp(PropReadsNeeded, "2")
	w.s.Work.Card("s1-1").Fields[FieldTier] = "pro"
	assert.Equal(t, 1, ReadsNeededIn(w.s, pr), "landed on one read under one: never judged again")
	assert.False(t, rule6(w.s))
}
