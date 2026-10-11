package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every move into review stamps the card's entry (the revision it entered from), and only a
// move into review: a write to a card already in review, or a move elsewhere, leaves it.
func TestAMoveIntoReviewStampsItsEntry(t *testing.T) {
	t.Parallel()
	c := &Card{ID: "a", Row: "s1", Col: Working, Rev: 7, Fields: map[string]string{"attempt": "1"}}
	set := map[string]string{"head": "abc"}
	e := moveEntry(c, "s1", Review, set)
	assert.Equal(t, "7", e.Set[FieldReviewEntered], "into review: the revision it entered from")
	assert.NotContains(t, set, FieldReviewEntered, "the caller's map is not written")
	assert.Equal(t, "abc", e.Set["head"])
	assert.Equal(t, "7", moveEntry(c, "s1", Review, nil).Set[FieldReviewEntered], "with no fields set")

	in := &Card{ID: "a", Row: "s1", Col: Review, Rev: 9, Fields: map[string]string{"attempt": "1"}}
	assert.NotContains(t, moveEntry(in, "s1", Review, map[string]string{"x": "1"}).Set, FieldReviewEntered, "a write in review is no entry")
	assert.NotContains(t, moveEntry(in, "s1", Merging, nil).Set, FieldReviewEntered, "a move out of review is no entry")
}

// TLC found it in the first design (tla/SettingsWarn.tla, the noentry witness): a card that
// leaves review and comes back at the same attempt between two ticks was silent, its record
// key the same. Keyed by its entry too, the second entry is warned of.
func TestACardBackInReviewAtTheSameAttemptBetweenTwoTicksIsWarnedOfAgain(t *testing.T) {
	t.Parallel()
	snap := func(entered string) *Snapshot {
		s := &Snapshot{Work: NewTable(Work), Coordinator: "boss"}
		s.Work.SetProps(map[string]string{PropReadCards: SwitchOff})
		s.Work.Put(&Card{ID: "a", Row: "s1", Col: Review, Rev: 12, Fields: map[string]string{"attempt": "1", FieldReviewEntered: entered}})
		return s
	}
	first := snap("5")
	p := tickSettingsWarnings(first, TickReq{})
	require.Len(t, notesOf(p, NReviewOffReadCards), 1, "the first entry is warned of")
	assert.Contains(t, notesOf(p, NReviewOffReadCards)[0].What, "a@1 entered review", "the warning says card and attempt")
	rec := recordOf(t, p, PropReviewWarned)
	assert.Equal(t, "a@1#5", rec)

	again := snap("5")
	again.Work.SetProp(PropReviewWarned, rec)
	assert.Empty(t, notesOf(tickSettingsWarnings(again, TickReq{}), NReviewOffReadCards), "the same entry, still in review: said once")

	back := snap("10") // out of review and back between the ticks, the same attempt
	back.Work.SetProp(PropReviewWarned, rec)
	w := notesOf(tickSettingsWarnings(back, TickReq{}), NReviewOffReadCards)
	require.Len(t, w, 1, "a new entry at the same attempt is warned of again")
	assert.Contains(t, w[0].What, "a@1 entered review")
}

func notesOf(p Plan, typ string) []Note {
	var out []Note
	for _, n := range p.Notes {
		if n.Type == typ {
			out = append(out, n)
		}
	}
	return out
}

func recordOf(t *testing.T, p Plan, name string) string {
	t.Helper()
	for _, w := range p.Props {
		if w.Name == name {
			return w.Value
		}
	}
	require.Failf(t, "no record written", "%s", name)
	return ""
}
