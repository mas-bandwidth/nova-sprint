package store

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// presenceBeats is every named member beating fresh at now: the machines are
// alive. A held member reads held whatever its beat (MemberStatus).
func presenceBeats(h *harness, members ...string) map[string]sprint.Beat {
	beats := map[string]sprint.Beat{}
	for _, m := range members {
		beats[m] = sprint.Beat{At: h.st.Now()}
	}
	return beats
}

// presenceStep is the tick's presence part as a store step, with the given
// beats: the member statuses applied (sprint.TickPresence).
func presenceStep(h *harness, beats map[string]sprint.Beat) Step {
	var due int
	return TickPartStep("presence", sprint.TickPresence, sprint.TickReq{Beats: beats}, nil, nil, &due)
}

// A member the sync holds (held_by=sync) whose status field still reads Up
// keeps the sync's mark when presence takes it down: the machine places its
// fault hold only on a member with no hold (tla/DealFill.tla FaultHold, from
// held=none; PersonHoldStays, the sync's mark is never changed by the machine).
// Overwriting it with fault would hide the hold from the sync's release, so the
// member would never come back (docs/SPEC-SPRINT.md section 5, the fleet).
func TestADownFaultKeepsTheSyncsHoldMark(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)

	// the sync holds m1: held (status down), held_by=sync
	h.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m1", Who: "sync", HeldBy: sprint.HeldBySync}))
	require.NotEmpty(t, h.table().MemberCtl("m1").F("held"), "the sync's hold is down")
	require.Equal(t, sprint.HeldBySync, h.table().MemberCtl("m1").F(sprint.FieldHeldBy))

	// a fleet up brings the status field up without lifting the hold: the state
	// head 38c66ea7542f left a held member in
	h.must(FleetStep(sprint.FleetReq{Op: "up", Member: "m1", Probe: sprint.EnvProbePassed}))
	before := h.table().MemberCtl("m1")
	require.Equal(t, sprint.Up, before.F("status"), "the status field reads up: %v", before.Fields)
	require.NotEmpty(t, before.F("held"), "the hold is still on the member")

	// presence, with m1 beating, sees the sync's hold: m1 goes down, and its
	// hold and its mark must be the sync's still
	h.must(presenceStep(h, presenceBeats(h, "m1", "m2")))
	after := h.table().MemberCtl("m1")
	assert.Equal(t, sprint.Down, after.F("status"), "the held member still goes down: %v", after.Fields)
	assert.NotEmpty(t, after.F("held"), "the hold went: %v", after.Fields)
	assert.Equal(t, sprint.HeldBySync, after.F(sprint.FieldHeldBy), "the machine overwrote the sync's mark: %v", after.Fields)
	assert.Empty(t, after.F(sprint.FieldFaultSince), "the machine stamped a fault over the sync's hold: %v", after.Fields)
}

// A member the environment holds (held_by=environment) whose status field
// still reads Up keeps the environment's mark when presence takes it down: the
// machine's fault hold is placed only on a member with no hold, so the
// environment's own release (clearEnvFault, EnvHeld) still finds its mark
// (tla/DealFill.tla FaultHold, PersonHoldStays; docs/SPEC-SPRINT.md section 5).
// The hold is the state head 38c66ea7542f's envFaulted left: held with
// held_by=environment beside a status field that read up.
func TestADownFaultKeepsTheEnvironmentsHoldMark(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)

	h.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m1", Who: "machine", HeldBy: sprint.HeldByEnv}))
	require.Equal(t, sprint.HeldByEnv, h.table().MemberCtl("m1").F(sprint.FieldHeldBy))

	h.must(FleetStep(sprint.FleetReq{Op: "up", Member: "m1", Probe: sprint.EnvProbePassed}))
	require.Equal(t, sprint.Up, h.table().MemberCtl("m1").F("status"), "the status field reads up")

	h.must(presenceStep(h, presenceBeats(h, "m1", "m2")))
	after := h.table().MemberCtl("m1")
	assert.Equal(t, sprint.Down, after.F("status"), "the held member still goes down: %v", after.Fields)
	assert.NotEmpty(t, after.F("held"), "the hold went: %v", after.Fields)
	assert.Equal(t, sprint.HeldByEnv, after.F(sprint.FieldHeldBy), "the machine overwrote the environment's mark: %v", after.Fields)
	assert.Empty(t, after.F(sprint.FieldFaultSince), "the machine stamped a fault over the environment's hold: %v", after.Fields)
}

// A member the coordinator holds (no held_by mark) whose status field still
// reads Up gains no fault mark when presence takes it down: the machine writes
// its fault mark only on a member with no hold, and a hold the machine did not
// make is never changed by it (tla/DealFill.tla FaultHold, PersonHoldStays;
// docs/SPEC-SPRINT.md section 5, the fleet).
func TestADownFaultLeavesTheCoordinatorsHoldUnmarked(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(1)

	h.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m1", Who: "coordinator"}))
	require.Empty(t, h.table().MemberCtl("m1").F(sprint.FieldHeldBy), "the coordinator's hold carries no mark")

	h.must(FleetStep(sprint.FleetReq{Op: "up", Member: "m1", Probe: sprint.EnvProbePassed}))
	require.Equal(t, sprint.Up, h.table().MemberCtl("m1").F("status"), "the status field reads up")

	h.must(presenceStep(h, presenceBeats(h, "m1", "m2")))
	after := h.table().MemberCtl("m1")
	assert.Equal(t, sprint.Down, after.F("status"), "the held member still goes down: %v", after.Fields)
	assert.NotEmpty(t, after.F("held"), "the hold went: %v", after.Fields)
	assert.Empty(t, after.F(sprint.FieldHeldBy), "the machine put a mark on a person's hold: %v", after.Fields)
	assert.Empty(t, after.F(sprint.FieldFaultSince), "the machine stamped a fault on a person's hold: %v", after.Fields)
}
