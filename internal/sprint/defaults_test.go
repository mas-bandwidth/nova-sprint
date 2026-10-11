package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// freshWorld is a world with no read_cards property: the fresh store a clear leaves, the
// working machine. newWorld sets read cards off so the old readers-table tests still pass;
// a snapshot with the property absent behaves as on, which is what this world models.
func freshWorld(t *testing.T, readers ...string) *world {
	t.Helper()
	w := newWorld(t, readers...)
	w.s.Work.SetProps(nil)
	return w
}

// TestReadCardsDefaultsOn pins the audit's one degraded default (the owner, 2026-10-11:
// "Defaults should always be the sprint machine works"; "The person installing nova-sprint
// for the first time must get smart defaults that WORK"): a snapshot whose work table holds
// no read_cards property asks its reads as read cards, every read a card in review needs in
// the one deal, not one per tick through the readers table (docs/SPEC-SPRINT.md section 6,
// "A read is a consumer card"; the PropReadCards comment). An absent property, "on" and
// "default" are on; "off" is the one way to degrade it.
func TestReadCardsDefaultsOn(t *testing.T) {
	t.Parallel()
	w := freshWorld(t, ReaderPrefix+"m1", ReaderPrefix+"m2", ReaderPrefix+"m3")
	for _, m := range []string{"m1", "m2", "m3"} {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m, Width: 4}))
	}
	require.True(t, w.s.ReadCardsOn(), "a snapshot with no read_cards property behaves as the working machine")
	putReviewBy(w, "s1-1", "s1-1: work (s1) tier: pro\n", "m1", 1)
	dealReads(t, w, nil)
	require.Len(t, readCardsOf(w, "s1-1"), 2, "a pro card's two reads are cut at once by default")
	for _, c := range w.s.Readers.Cards() {
		require.False(t, c.Placed(), "the readers table is asked nothing by default")
	}

	for _, set := range []string{"", ReadCardsOnWord, ReadTierDefault} {
		w2 := newWorld(t, ReaderPrefix+"m1")
		w2.s.Work.SetProps(nil)
		if set != "" {
			w2.s.Work.SetProp(PropReadCards, set)
		}
		require.True(t, w2.s.ReadCardsOn(), "read_cards=%q is on", set)
	}
	w3 := newWorld(t, ReaderPrefix+"m1")
	w3.s.Work.SetProp(PropReadCards, SwitchOff)
	require.False(t, w3.s.ReadCardsOn(), "read_cards=off degrades: the readers table asks one per tick")
}
