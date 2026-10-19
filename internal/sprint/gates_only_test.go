package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The gates-only deal (gates_only.go; tla/GatesOnly.tla): a native Windows member whose
// machine row names gates_only windows takes only the cards whose GATE-NEEDS names windows,
// never a generic card and never a read. Unit tests: no time but the world's, no sockets.

// gatesWorld is two members up at width 2: m-a (gates_only windows, facts windows) and m-b
// (a plain linux bench), m-a first in the deal's round, with one ready card.
func gatesWorld(t *testing.T) *world {
	t.Helper()
	w := fleetWorld(t, 1, 2, "m-a", "m-b")
	w.s.MemberCtl("m-a").Fields[FieldFacts] = "windows,amd64"
	w.s.MemberCtl("m-a").Fields[FieldGatesOnly] = "windows"
	w.s.MemberCtl("m-b").Fields[FieldFacts] = "linux,amd64,go"
	return w
}

// TestAGenericCardIsNeverDealtToAGatesOnlyMember: the round starts at the gates-only member
// and the card names no GATE-NEEDS: it goes to the plain bench. GenericNeverToGatesOnly.
func TestAGenericCardIsNeverDealtToAGatesOnlyMember(t *testing.T) {
	t.Parallel()
	w := gatesWorld(t)
	w.s.Work.Card("s1-1").Fields["brief"] = "tier: pro\n\nThe task.\n"
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc, "the generic card is dealt")
	assert.Equal(t, "m-b", wc.Row, "a generic card never reaches a gates-only member")
}

// TestAWindowsCardIsDealtToTheGatesOnlyMember: a card whose GATE-NEEDS names windows goes to
// the windows member (and the linux bench lacks the fact). WindowsCardDealt.
func TestAWindowsCardIsDealtToTheGatesOnlyMember(t *testing.T) {
	t.Parallel()
	w := gatesWorld(t)
	w.s.Work.Card("s1-1").Fields["brief"] = "tier: pro\nGATE-NEEDS: windows\n\nThe task.\n"
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc, "the windows card is dealt")
	assert.Equal(t, "m-a", wc.Row)
}

// TestACardNamingAnotherGateSkipsTheGatesOnlyMember: GATE-NEEDS linux names none of m-a's
// gates, so m-a is not even asked about its facts.
func TestACardNamingAnotherGateSkipsTheGatesOnlyMember(t *testing.T) {
	t.Parallel()
	w := gatesWorld(t)
	w.s.MemberCtl("m-a").Fields[FieldFacts] = "windows,linux"
	w.s.Work.Card("s1-1").Fields["brief"] = "tier: pro\nGATE-NEEDS: linux\n\nThe task.\n"
	pool, why := w.s.GateHostPool(w.s.Work.Card("s1-1"), []string{"m-a", "m-b"})
	assert.Equal(t, []string{"m-b"}, pool, "a gates-only member takes only the cards naming its gates, whatever its facts")
	assert.Empty(t, why)
}

// TestAGenericCardWaitsWhenOnlyGatesOnlyMembersAreUp: with the windows member alone up, a
// generic card stays ready with a wait that names the gates, never the gate-host judgment.
func TestAGenericCardWaitsWhenOnlyGatesOnlyMembersAreUp(t *testing.T) {
	t.Parallel()
	w := fleetWorld(t, 1, 2, "m-a")
	w.s.MemberCtl("m-a").Fields[FieldFacts] = "windows,amd64"
	w.s.MemberCtl("m-a").Fields[FieldGatesOnly] = "windows"
	w.s.Work.Card("s1-1").Fields["brief"] = "tier: pro\n\nThe task.\n"
	p, _ := TickDeal(w.s, TickReq{})
	require.Len(t, p.Refused, 1, "the generic card waits: %+v", p)
	assert.Equal(t, "s1-1", p.Refused[0].Key)
	assert.Contains(t, p.Refused[0].Why, "m-a (gates_only: windows)", "the wait names the gates")
	for _, n := range p.Notes {
		assert.NotEqual(t, NGateNeeds, n.Type, "no need is unmet: no gate judgment")
	}
	assert.Nil(t, w.s.Fleet.Card("s1-1.w1"), "the generic card is never dealt to the gates-only member")
	pool, why := w.s.GateHostPool(w.s.Work.Card("s1-1"), []string{"m-a"})
	assert.Empty(t, pool)
	assert.Empty(t, why, "an empty pool by gates alone is a wait, not a gate judgment")
	assert.Contains(t, gatesOnlyWhy(w.s, []string{"m-a"}), "m-a (gates_only: windows)")
}

// TestGatesOnlyAdmits: the rule itself.
func TestGatesOnlyAdmits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		gates, needs []string
		want         bool
	}{
		{nil, nil, true},
		{nil, []string{"linux"}, true},
		{[]string{"windows"}, nil, false},
		{[]string{"windows"}, []string{"linux"}, false},
		{[]string{"windows"}, []string{"windows"}, true},
		{[]string{"windows"}, []string{"windows", "amd64"}, true},
		{[]string{"windows", "xeon"}, []string{"xeon"}, true},
	} {
		assert.Equal(t, tc.want, gatesOnlyAdmits(tc.gates, tc.needs), "gates %v needs %v", tc.gates, tc.needs)
	}
	var s *Snapshot
	assert.Nil(t, s.MemberGatesOnly("m"), "a nil snapshot names no gates")
}

// TestAGatesOnlyMemberReadsNothing: every up member is a reader, but a gates-only one is not
// a read unit. ReadsNeverToGatesOnly.
func TestAGatesOnlyMemberReadsNothing(t *testing.T) {
	t.Parallel()
	w := gatesWorld(t)
	w.s.Readers.SetRows(append(w.s.Readers.Rows(), ReaderPrefix+"m-a", ReaderPrefix+"m-b"))
	var names []string
	for _, u := range readUnitsOf(w.s, nil) {
		names = append(names, u.name)
	}
	assert.Contains(t, names, "m-b", "the plain bench reads")
	assert.NotContains(t, names, "m-a", "the gates-only member reads nothing")
}

// TestFleetSyncWritesAndClearsGatesOnly: the sync writes the machine row's gates_only onto
// the member's control card with an add, sets it on a member whose card differs, clears it
// when the row drops it, and a sync after a sync writes nothing.
func TestFleetSyncWritesAndClearsGatesOnly(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	sync := func(gates []string) FleetReq {
		return FleetReq{Op: "sync", Who: "sync",
			Sync:     []SyncMember{{Name: "m1", Width: DefaultWidth}, {Name: "m9", Width: 2, GatesOnly: gates}},
			Machines: []string{"m1", "m9"}}
	}
	w.must(FleetStep(w.s, sync([]string{"windows"})))
	require.NotNil(t, w.s.MemberCtl("m9"), "the sync adds m9")
	assert.Equal(t, "windows", w.s.MemberCtl("m9").F(FieldGatesOnly), "the add writes the gates with the card")
	assert.Empty(t, w.s.MemberCtl("m1").F(FieldGatesOnly))
	assert.Empty(t, FleetDrift(w.s, sync([]string{"windows"}).Sync, []string{"m1", "m9"}), "a sync after a sync has nothing to write")

	d := FleetDrift(w.s, sync([]string{"windows", "xeon"}).Sync, []string{"m1", "m9"})
	require.Len(t, d, 1)
	assert.Equal(t, DriftGates, d[0].Kind)
	assert.Equal(t, `m9 has gates_only "windows" and the inventory says "windows,xeon"`, d[0].Line())
	w.must(FleetStep(w.s, sync([]string{"windows", "xeon"})))
	assert.Equal(t, "windows,xeon", w.s.MemberCtl("m9").F(FieldGatesOnly))

	w.must(FleetStep(w.s, sync(nil)))
	_, has := w.s.MemberCtl("m9").Fields[FieldGatesOnly]
	assert.False(t, has, "a row that drops gates_only clears the card's")
	assert.Empty(t, FleetDrift(w.s, sync(nil).Sync, []string{"m1", "m9"}))
}

// TestAClearCarriesGatesOnly: the epoch's shape keeps a member's gates_only, so the new
// epoch's control card has it before any sync.
func TestAClearCarriesGatesOnly(t *testing.T) {
	t.Parallel()
	w := gatesWorld(t)
	sh := ShapeOf(w.s)
	assert.Equal(t, map[string]string{"m-a": "windows"}, sh.GatesOnly)
	w.s.Fleet = NewTable(Fleet)
	w.s.Fleet.SetRows(sh.Members)
	p := RestoreShape(w.s, sh)
	var fields map[string]string
	for _, u := range p.Units {
		if u.Key == CtlID("m-a") {
			fields = u.Changes[0].Entry.Set
		}
	}
	require.NotNil(t, fields, "the clear makes m-a's control card again: %+v", p.Units)
	assert.Equal(t, "windows", fields[FieldGatesOnly])
}
