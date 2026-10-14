package store

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
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

// A member a holder holds, whose status field still reads Up, keeps that
// holder's mark when presence takes it down: the machine places its fault
// hold only on a member with no hold, and a hold the machine did not make is
// never changed by it. Overwriting the sync's or the environment's mark with
// fault would hide the hold from its own release, so the member would never
// come back (tla/DealFill.tla FaultHold, PersonHoldStays; the sync's and the
// environment's cases are the state head 38c66ea7542f's held members;
// docs/SPEC-SPRINT.md section 5, the fleet). The rows: the sync's hold, the
// environment's hold, and the coordinator's hold, which carries no mark.
func TestADownFaultKeepsTheHoldersMark(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		who      string // the verb's Who: who holds m1
		mark     string // the held_by mark the hold carries and must keep
		heldDown bool   // the hold's status reads down beside its mark
	}{
		{"the syncs hold", "sync", sprint.HeldBySync, true},
		{"the environments hold", "machine", sprint.HeldByEnv, false},
		{"the coordinators hold", "coordinator", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.setup(1)
			h.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m1", Who: tc.who, HeldBy: tc.mark}))
			held := h.table().MemberCtl("m1")
			assert.Equal(t, tc.mark, held.F(sprint.FieldHeldBy), "the hold's mark: %v", held.Fields)
			if tc.heldDown {
				assert.NotEmpty(t, held.F("held"), "the hold is down")
			}
			// a fleet up brings the status field up without lifting the hold
			h.must(FleetStep(sprint.FleetReq{Op: "up", Member: "m1", Probe: sprint.EnvProbePassed}))
			before := h.table().MemberCtl("m1")
			assert.Equal(t, sprint.Up, before.F("status"), "the status field reads up: %v", before.Fields)
			if tc.heldDown {
				assert.NotEmpty(t, before.F("held"), "the hold is still on the member")
			}
			// presence, with m1 beating, sees the hold: m1 goes down, and its
			// hold and its mark must be its holder's still
			h.must(presenceStep(h, presenceBeats(h, "m1", "m2")))
			after := h.table().MemberCtl("m1")
			assert.Equal(t, sprint.Down, after.F("status"), "the held member still goes down: %v", after.Fields)
			assert.NotEmpty(t, after.F("held"), "the hold went: %v", after.Fields)
			assert.Equal(t, tc.mark, after.F(sprint.FieldHeldBy), "the machine changed the holder's mark: %v", after.Fields)
			assert.Empty(t, after.F(sprint.FieldFaultSince), "the machine stamped a fault over the hold: %v", after.Fields)
		})
	}
}
