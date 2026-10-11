package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The working defaults (docs/SPEC-SPRINT.md section 11, the settings): a store the
// coordinator has set nothing on is the working machine, not a degraded one. Each test
// here pins one default that used to leave the machine degraded (the owner, 2026-10-11:
// "Defaults should always be the sprint machine works").

// workingDefaultsWorld is a fresh world with the members up at the width, each with a
// reader row, and nothing set on the work table: a first install.
func workingDefaultsWorld(t *testing.T, width int, members ...string) *world {
	t.Helper()
	var readers []string
	for _, m := range members {
		readers = append(readers, ReaderPrefix+m)
	}
	w := newWorld(t, readers...)
	for _, m := range members {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m, Width: width}))
	}
	return w
}

// TestReadCardsOnByDefault pins the read-cards default (docs/SPEC-SPRINT.md section 11,
// the settings; the owner, 2026-10-11): an absent property, or "default", is on; only
// "off" is off.
func TestReadCardsOnByDefault(t *testing.T) {
	t.Parallel()
	w := workingDefaultsWorld(t, 2, "m1")
	require.True(t, w.s.ReadCardsOn(), "an absent read-cards property is on")
	w.s.Work.SetProp(PropReadCards, ReadTierDefault)
	require.True(t, w.s.ReadCardsOn(), "default is on")
	w.s.Work.SetProp(PropReadCards, ReadCardsOnWord)
	require.True(t, w.s.ReadCardsOn(), "on is on")
	w.s.Work.SetProp(PropReadCards, ReadCardsOffWord)
	require.False(t, w.s.ReadCardsOn(), "off is the one way off")
}

// TestAbsentReadCardsDealsEveryReadAtOnce pins the first install (the owner, 2026-10-11:
// "The person installing nova-sprint for the first time must get smart defaults that
// WORK"): with no set run at all, the deal asks every read a primary in review needs in
// that one tick as read cards, not the readers table's one read per tick, each to a
// different reader, so both members are filled.
func TestAbsentReadCardsDealsEveryReadAtOnce(t *testing.T) {
	t.Parallel()
	w := workingDefaultsWorld(t, 2, "m1", "m2", "m3")
	putReviewBy(w, "s1-1", "s1-1: work (s1) tier: pro\n", "m1", 1)
	dealReads(t, w, nil)

	reads := readCardsOf(w, "s1-1")
	require.Len(t, reads, 2, "both reads asked in the one tick, no set run")
	seen := map[string]bool{}
	for _, c := range reads {
		seen[c.Row] = true
	}
	require.Len(t, seen, 2, "two different readers take a read")
	for _, c := range w.s.Readers.Cards() {
		require.False(t, c.Placed(), "the readers table is asked nothing while read cards are on")
	}
}
