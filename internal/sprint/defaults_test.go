package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The audit of defaults (docs/SPEC-SPRINT.md section 11, "a clear starts the
// next epoch with none of them"): each setting's default must leave the machine
// working. Read cards were the one wrong default: off asked reads one per tick
// through the old readers table, so a fresh store, and every store after a
// clear, read at a sixth of the working rate. The flip is read cards default on;
// every other default is right and is said so in the report.

// defaultWorld is the in-memory world with the read-cards property absent, as a
// fresh store or a store after a clear leaves it: read cards default on.
func defaultWorld(t *testing.T, readers ...string) *world {
	t.Helper()
	s := &Snapshot{Now: t0, Work: NewTable(Work), Readers: NewTable(Readers), Merge: NewTable(Merge), Fleet: NewTable(Fleet),
		Coordinator: "coordinator", Actor: "coordinator"}
	s.Readers.SetRows(append(s.Readers.Rows(), readers...))
	return &world{t: t, s: s}
}

// TestReadCardsDefaultsOn pins the default of the read-cards switch
// (docs/SPEC-SPRINT.md section 11, set --read-cards): an absent property, on or
// default is on; off is the one value that turns it off, and it degrades the
// machine to one read per tick through the readers table.
func TestReadCardsDefaultsOn(t *testing.T) {
	t.Parallel()
	w := defaultWorld(t)
	require.True(t, w.s.ReadCardsOn(), "an absent property is on")
	w.s.Work.SetProp(PropReadCards, ReadCardsOnWord)
	require.True(t, w.s.ReadCardsOn(), "on is on")
	w.s.Work.SetProp(PropReadCards, ReadTierDefault)
	require.True(t, w.s.ReadCardsOn(), "default is on")
	w.s.Work.SetProp(PropReadCards, SwitchOff)
	require.False(t, w.s.ReadCardsOn(), "off is off, the one way to turn it off")
}

// TestReadCardsDefaultDealsEveryReadAtOnce pins that a snapshot with no
// read-cards property behaves as the working machine: two members up at width 2
// with a reader row each, and a pro primary in review, are asked both its reads
// in the one deal, as read cards on the fleet table, never one per tick through
// the readers table.
func TestReadCardsDefaultDealsEveryReadAtOnce(t *testing.T) {
	t.Parallel()
	w := defaultWorld(t, "reader-m1", "reader-m2", "reader-m3")
	for _, m := range []string{"m1", "m2", "m3"} {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m, Width: 2}))
	}
	putReviewBy(w, "s1-1", "s1-1: work (s1) tier: pro\n", "m1", 1)
	dealReads(t, w, nil)
	pro := readCardsOf(w, "s1-1")
	require.Len(t, pro, 2, "a pro card's two reads are cut at once, with no property set")
	seen := map[string]bool{}
	for _, c := range pro {
		require.Equal(t, "read", c.F("kind"))
		require.Equal(t, "1", c.F(FieldReadCard))
		require.NotEqual(t, "m1", c.Row, "never the worker's")
		seen[c.Row] = true
	}
	require.Len(t, seen, 2, "two different readers")
	for _, c := range w.s.Readers.Cards() {
		require.False(t, c.Placed(), "the readers table is asked nothing by default")
	}
}

// TestSetOffDegradesTheMachineSaysSo pins that set --read-cards off is the one
// way to turn read cards off and says it degrades the machine (settings.go's
// orDefault): the sprint line names the readers table's one per tick.
func TestSetOffDegradesTheMachineSaysSo(t *testing.T) {
	t.Parallel()
	w := defaultWorld(t)
	p := Set(w.s, SetReq{ReadCards: SwitchOff, Who: "coordinator"})
	require.Empty(t, p.Refused)
	require.Contains(t, p.Units[0].Moved, "read-cards off (reads are asked one per tick through the readers table)")
}
