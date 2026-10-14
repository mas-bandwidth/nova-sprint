package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSyncProblemsRefusesWhatTheFleetRefuses: a name the fleet would refuse,
// and a name twice, are refused whole. A width is nova-config's alone, so 0
// and a width past the maximum are membership and are not refused
// (docs/SPEC-SPRINT.md section 5; tla/Level.tla Width).
func TestSyncProblemsRefusesWhatTheFleetRefuses(t *testing.T) {
	t.Parallel()
	ps := SyncProblems([]SyncMember{{"m1", 4}, {"m2", MaxWidth}})
	require.Empty(t, ps, "a valid sync: %v", ps)
	ps = SyncProblems(nil)
	require.Empty(t, ps, "an empty sync holds the fleet and is valid: %v", ps)
	ps = SyncProblems([]SyncMember{{"m1", 0}, {"m2", MaxWidth + 1}})
	require.Empty(t, ps, "a width is not the sync's to refuse: %v", ps)
	for _, c := range []struct {
		want []SyncMember
		say  string
	}{
		{[]SyncMember{{"m 1", 4}}, "a member name wants"},
		{[]SyncMember{{"m1", 1}, {"m1", 2}}, "m1 is named twice"},
	} {
		ps = SyncProblems(c.want)
		require.Len(t, ps, 1, "%v: %v", c.want, ps)
		assert.Contains(t, ps[0].Why, c.say, "%v: %q, want %q", c.want, ps[0].Why, c.say)
	}
}

// TestDriftSaysWhatASyncWouldWrite: one line for each kind of drift.
func TestDriftSaysWhatASyncWouldWrite(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		d   Drift
		say string
	}{
		{Drift{Member: "m1", Kind: DriftAdd, To: 4}, "m1 is a member of the inventory and not of the fleet: add it"},
		{Drift{Member: "m1", Kind: DriftRelease}, "m1 was held by the sync and is back in the inventory: release it"},
		{Drift{Member: "m1", Kind: DriftRemove}, "m1 has no machine row in the inventory and no card stays on it: remove it from the fleet"},
		{Drift{Member: "m1", Kind: DriftHold}, "m1 is a member of the fleet and not of the inventory: hold it down"},
	} {
		got := c.d.Line()
		assert.Equal(t, c.say, got, "%q, want %q", got, c.say)
	}
}

// syncHeld is a world with members m1 and m2 up, and m2 held by a sync that
// names m1 alone a member and m2 a machine of width 0.
func syncHeld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-a")
	w.up("m1")
	w.up("m2")
	w.must(FleetStep(w.s, FleetReq{Op: "sync", Who: "sync", Sync: []SyncMember{{"m1", DefaultWidth}}, Machines: []string{"m1", "m2"}}))
	ctl := w.s.MemberCtl("m2")
	require.NotEmpty(t, ctl.F("held"), "m2 is not held by the sync: %v", ctl.Fields)
	require.Equal(t, HeldBySync, ctl.F(FieldHeldBy), "m2 is not held by the sync: %v", ctl.Fields)
	return w
}

// TestASyncAddedMemberComesUpOnlyAfterItsProbe: a member the sync adds to the
// fleet is held until its own probe passes, so presence's beat alone never
// brings it up and it is dealt no card (internal/sprint/fleet_sync.go's
// DriftAdd branch; docs/SPEC-SPRINT.md section 5, the fleet; tla/ProviderBudget.tla,
// NewMemberProbed).
func TestASyncAddedMemberComesUpOnlyAfterItsProbe(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.up("m1")
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 2}))
	w.must(FleetStep(w.s, FleetReq{Op: "sync", Who: "sync",
		Sync:     []SyncMember{{"m1", DefaultWidth}, {"m9", DefaultWidth}},
		Machines: []string{"m1", "m9"}}))
	require.NotNil(t, w.s.MemberCtl("m9"), "the sync adds m9")
	presenceWith(w, "m1", "m9")
	w.must(Deal(w.s, DealReq{}))
	assert.NotContains(t, w.s.UpMembers(), "m9", "a sync-added member is not up before its probe passes")
	total := w.s.Fleet.Count("m9", Ready) + w.s.Fleet.Count("m9", Working)
	assert.Zero(t, total, "a member not up is dealt no card")
}

// TestFleetUpASyncAddedMemberNeedsItsProbe: fleet up of a member the sync added
// is refused in the new-member words until its own probe passes; with the probe
// passed the member comes up at the sync's width, its hold lifted, and is dealt
// to (fleetStepPlan, steps_work.go; docs/SPEC-SPRINT.md section 5, the fleet;
// tla/ProviderBudget.tla, NewMemberProbed).
func TestFleetUpASyncAddedMemberNeedsItsProbe(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.up("m1")
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 2}))
	w.must(FleetStep(w.s, FleetReq{Op: "sync", Who: "sync",
		Sync:     []SyncMember{{"m1", DefaultWidth}, {"m9", 3}},
		Machines: []string{"m1", "m9"}}))

	p := FleetStep(w.s, FleetReq{Op: "up", Member: "m9"})
	require.Len(t, p.Refused, 1, "fleet up with no probe is refused: %+v", p)
	assert.Contains(t, p.Refused[0].Why, "its own probe has not passed")

	w.setWidth("m9", 3)
	p = FleetStep(w.s, FleetReq{Op: "up", Member: "m9", Probe: EnvProbePassed})
	require.Empty(t, p.Refused, "%+v", p)
	w.must(p)
	require.Contains(t, w.s.UpMembers(), "m9", "the passed probe brings it up")
	ctl := w.s.MemberCtl("m9")
	assert.Equal(t, 3, w.s.Width("m9"), "at the config width")
	assert.Empty(t, ctl.F("held"), "the probe hold is lifted")
	assert.Empty(t, ctl.F(FieldHeldBy), "the mark is cleared")
	assert.Empty(t, ctl.F(FieldHeldReason), "the reason is cleared")
	w.must(Deal(w.s, DealReq{}))
	assert.NotZero(t, w.s.Fleet.Count("m9", Ready)+w.s.Fleet.Count("m9", Working), "an up member is dealt to")
}

// TestACoordinatorsHoldOnAProbeHeldMemberKeepsTheMark: fleet down (the
// coordinator's hold) of a member the sync added held by the probe leaves the
// probe's mark in place, so a following fleet up with no probe is still refused
// and the member is not up (downPlan, steps_work.go; docs/SPEC-SPRINT.md section
// 5, the fleet; tla/ProviderBudget.tla, NewMemberProbed).
func TestACoordinatorsHoldOnAProbeHeldMemberKeepsTheMark(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.up("m1")
	w.must(FleetStep(w.s, FleetReq{Op: "sync", Who: "sync",
		Sync:     []SyncMember{{"m1", DefaultWidth}, {"m9", DefaultWidth}},
		Machines: []string{"m1", "m9"}}))
	require.Equal(t, HeldByProbe, w.s.MemberCtl("m9").F(FieldHeldBy), "the sync adds m9 held by the probe")

	w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: "m9", Who: "coordinator"}))
	ctl := w.s.MemberCtl("m9")
	require.NotEmpty(t, ctl.F("held"), "m9 after the coordinator's hold: %v", ctl.Fields)
	assert.Equal(t, HeldByProbe, ctl.F(FieldHeldBy), "the probe's mark stays: %v", ctl.Fields)

	p := FleetStep(w.s, FleetReq{Op: "up", Member: "m9"})
	require.Len(t, p.Refused, 1, "fleet up with no probe is refused: %+v", p)
	assert.Contains(t, p.Refused[0].Why, "its own probe has not passed")
	assert.NotContains(t, w.s.UpMembers(), "m9", "a probe-held member is not up")
}

// TestASecondSyncAfterAProbeAddWritesNothing: the add held by the probe leaves
// no drift behind, so a sync after it writes nothing (FleetDrift, fleet_sync.go).
func TestASecondSyncAfterAProbeAddWritesNothing(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.up("m1")
	sync := FleetReq{Op: "sync", Who: "sync",
		Sync:     []SyncMember{{"m1", DefaultWidth}, {"m9", DefaultWidth}},
		Machines: []string{"m1", "m9"}}
	w.must(FleetStep(w.s, sync))
	require.NotNil(t, w.s.MemberCtl("m9"), "the sync adds m9")
	assert.Empty(t, FleetDrift(w.s, sync.Sync, sync.Machines), "the add leaves no drift")
	p := FleetStep(w.s, sync)
	assert.Empty(t, p.Units, "a second sync writes nothing: %+v", p)
}

// TestAMemberTheSyncHeldAndReleasedIsUnchanged: a member the sync held (and
// whose probe passed before it was up) is released by the sync, its width kept,
// no probe hold written (FleetDrift DriftRelease, fleet_sync.go).
func TestAMemberTheSyncHeldAndReleasedIsUnchanged(t *testing.T) {
	t.Parallel()
	w := syncHeld(t)
	before := w.s.MemberCtl("m2").F(FieldWidth)
	w.must(FleetStep(w.s, FleetReq{Op: "sync", Who: "sync",
		Sync:     []SyncMember{{"m1", DefaultWidth}, {"m2", DefaultWidth}},
		Machines: []string{"m1", "m2"}}))
	ctl := w.s.MemberCtl("m2")
	assert.Empty(t, ctl.F("held"), "released")
	assert.Empty(t, ctl.F(FieldHeldBy), "no mark")
	assert.NotEqual(t, HeldByProbe, ctl.F(FieldHeldBy), "never a probe hold")
	assert.Equal(t, before, ctl.F(FieldWidth), "its width is unchanged")
}

// TestACoordinatorsHoldOnAMemberTheSyncHoldsClearsTheMark: fleet down on a
// member the sync already holds makes the hold the coordinator's, so the sync
// never releases it (fleetStep, downPlan).
func TestACoordinatorsHoldOnAMemberTheSyncHoldsClearsTheMark(t *testing.T) {
	t.Parallel()
	w := syncHeld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: "m2", Who: "coordinator"}))
	ctl := w.s.MemberCtl("m2")
	require.NotEmpty(t, ctl.F("held"), "m2 after the coordinator's hold: %v", ctl.Fields)
	require.Empty(t, ctl.F(FieldHeldBy), "m2 after the coordinator's hold: %v", ctl.Fields)
	for _, d := range FleetDrift(w.s, []SyncMember{{"m1", DefaultWidth}, {"m2", DefaultWidth}}, []string{"m1", "m2"}) {
		require.NotEqual(t, DriftRelease, d.Kind, "the sync would release the coordinator's hold: %+v", d)
	}
}

// TestFleetUpClearsTheSyncsMark: releasing a member the sync held leaves no
// mark behind for a later hold to inherit.
func TestFleetUpClearsTheSyncsMark(t *testing.T) {
	t.Parallel()
	w := syncHeld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "release", Member: "m2", Who: "coordinator"}))
	ctl := w.s.MemberCtl("m2")
	require.Empty(t, ctl.F("held"), "m2 after fleet up: %v", ctl.Fields)
	require.Empty(t, ctl.F(FieldHeldBy), "m2 after fleet up: %v", ctl.Fields)
}

// TestTheSyncsReleaseClearsItsMark.
func TestTheSyncsReleaseClearsItsMark(t *testing.T) {
	t.Parallel()
	w := syncHeld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "sync", Who: "sync", Sync: []SyncMember{{"m1", DefaultWidth}, {"m2", DefaultWidth}}}))
	ctl := w.s.MemberCtl("m2")
	require.Empty(t, ctl.F("held"), "m2 after the sync's release: %v", ctl.Fields)
	require.Empty(t, ctl.F(FieldHeldBy), "m2 after the sync's release: %v", ctl.Fields)
}

// TestAHoldClearsAStaleMark: a member with a mark and no hold (a card written
// before fleet up cleared marks) is held by the coordinator without the
// mark, so the sync cannot take the hold for its own.
func TestAHoldClearsAStaleMark(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a")
	w.up("m1")
	ctl := w.s.MemberCtl("m1")
	w.must(Plan{Units: []Unit{{Key: CtlID("m1"), Changes: []Change{change(Fleet, setEntry(ctl, map[string]string{FieldHeldBy: HeldBySync}))}}}})
	require.Equal(t, HeldBySync, w.s.MemberCtl("m1").F(FieldHeldBy), "the stale mark is not set: %v", w.s.MemberCtl("m1").Fields)
	require.Empty(t, w.s.MemberCtl("m1").F("held"), "the stale mark is not set: %v", w.s.MemberCtl("m1").Fields)
	w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: "m1", Who: "coordinator"}))
	ctl = w.s.MemberCtl("m1")
	require.NotEmpty(t, ctl.F("held"), "m1 after the coordinator's hold: %v", ctl.Fields)
	require.Empty(t, ctl.F(FieldHeldBy), "m1 after the coordinator's hold: %v", ctl.Fields)
}

// TestALiveFinishedCardKeepsAMemberWithNoMachineRow: the finished work card of a
// primary still open (its reads and its merge read it) stays on the member, so a
// member with no machine row that holds one is held, never removed (memberKeeps);
// once the primary moves to another attempt the card is history, and the sync
// removes the member, its control card off the table, held by the sync.
func TestALiveFinishedCardKeepsAMemberWithNoMachineRow(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.up("m1")
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 1}))
	w.must(Deal(w.s, DealReq{}))
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1")}))
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1"), Failed: true, Report: "tests red"}))
	w.up("m2")
	sync := FleetReq{Op: "sync", Who: "sync", Sync: []SyncMember{{"m2", DefaultWidth}}, Machines: []string{"m2"}}
	require.Equal(t, []Drift{{Member: "m1", Kind: DriftHold}}, FleetDrift(w.s, sync.Sync, sync.Machines), "s1-1, in review, reads its failed card on m1")
	w.must(FleetStep(w.s, sync))
	require.NotNil(t, w.s.MemberCtl("m1"), "m1 is held, not removed")
	assert.Len(t, GoneHolding(w.s, sync.Sync, sync.Machines), 1)
	assert.Empty(t, FleetDrift(w.s, sync.Sync, sync.Machines), "held: a second sync writes nothing")
	w.must(Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "again"}))
	require.Equal(t, []Drift{{Member: "m1", Kind: DriftRemove}}, FleetDrift(w.s, sync.Sync, sync.Machines), "attempt 2 is on m2: s1-1.w1 is history")
	w.must(FleetStep(w.s, sync))
	assert.Nil(t, w.s.MemberCtl("m1"), "m1's control card is off the table")
	ctl := w.s.Fleet.Card(CtlID("m1"))
	require.NotNil(t, ctl, "its record is kept")
	assert.Equal(t, HeldBySync, ctl.F(FieldHeldBy), "held by the sync, so a return releases it")
	assert.Equal(t, []Drift{{Member: "m1", Kind: DriftRemove}}, FleetDrift(w.s, sync.Sync, sync.Machines), "its row is the verb's to delete")
}
