package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// consumersOutSnap builds a snapshot with the given work cards, fleet cards and
// read cards, for the ConsumersOut unit tests: each card is placed whole.
func consumersOutSnap(work, fleet, reads []*Card) *Snapshot {
	w := NewTable(Work)
	for _, c := range work {
		w.Put(c)
	}
	f := NewTable(Fleet)
	for _, c := range fleet {
		f.Put(c)
	}
	r := NewTable(Readers)
	for _, c := range reads {
		r.Put(c)
	}
	return &Snapshot{Work: w, Fleet: f, Readers: r}
}

// quietSubjects is the subjects of the consumers-out quiets, in order.
func quietSubjects(qs []LaneQuiet) []string {
	var out []string
	for _, q := range qs {
		if q.Type == "consumers-out" {
			out = append(out, q.Subject)
		}
	}
	return out
}

// A working card with no attempt (its work field names nothing, and the fleet
// holds no work card for it) has a missing consumer: one judgment naming it.
func TestConsumersOutWorkingCardWithNoAttempt(t *testing.T) {
	t.Parallel()
	s := consumersOutSnap(
		[]*Card{{ID: "s1-1", Row: "s1", Col: Working, Fields: map[string]string{}}},
		nil, nil,
	)
	require.Equal(t, []string{"s1-1"}, quietSubjects(ConsumersOut(s)))
}

// A review card whose reads are all for other cards has a missing consumer:
// the readers table holds cards, but none is a read out for this card.
func TestConsumersOutReviewCardWhoseReadsAreAllForOtherCards(t *testing.T) {
	t.Parallel()
	s := consumersOutSnap(
		[]*Card{{ID: "s1-1", Row: "s1", Col: Review, Fields: map[string]string{}}},
		nil,
		[]*Card{{ID: "s1-2.r1.reader-a", Row: "reader-a", Col: Asked, Fields: map[string]string{"primary": "s1-2"}}},
	)
	require.Equal(t, []string{"s1-1"}, quietSubjects(ConsumersOut(s)))
}

// A review card with no read and no open judgment that names it has a missing
// consumer: one judgment naming it.
func TestConsumersOutReviewCardWithNoReadAndNoJudgment(t *testing.T) {
	t.Parallel()
	s := consumersOutSnap(
		[]*Card{{ID: "s1-1", Row: "s1", Col: Review, Fields: map[string]string{}}},
		nil, nil,
	)
	require.Equal(t, []string{"s1-1"}, quietSubjects(ConsumersOut(s)))
}

// The check leaves alone the cards whose consumers are out: a working card
// held by an up member at its live generation, a review card with a read out
// for it, and a review card parked by an open judgment that names it.
func TestConsumersOutDoesNotFlagCardsWhoseConsumersAreOut(t *testing.T) {
	t.Parallel()
	s := consumersOutSnap(
		[]*Card{
			{ID: "s1-1", Row: "s1", Col: Working, Fields: map[string]string{"work": "s1-1.w1"}},
			{ID: "s1-2", Row: "s1", Col: Review, Fields: map[string]string{"attempt": "1"}},
			{ID: "s1-3", Row: "s1", Col: Review, Fields: map[string]string{"attempt": "1"}},
		},
		[]*Card{
			{ID: "s1-1.w1", Row: "m1", Col: Working, Fields: map[string]string{"gen": "1"}},
			{ID: CtlID("m1"), Row: "m1", Col: Ctl, Fields: map[string]string{"status": Up}},
		},
		[]*Card{{ID: "s1-2.r1.reader-a", Row: "reader-a", Col: Reading, Fields: map[string]string{"primary": "s1-2"}}},
	)
	s.Open = []Open{{Key: "j1|s1-3", Note: Note{Kind: Judgment, Type: NStranded, Primaries: []string{"s1-3"}}}}
	require.Empty(t, quietSubjects(ConsumersOut(s)))
}
