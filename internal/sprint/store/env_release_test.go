package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// pushNoCredential is a finish's report of a push the machine could not make: no
// push credential in the environment (sprint.EnvPush). The fault is the member's
// and holds it until its probe passes or a card of it pushes
// (tla/ProviderBudget.tla, FleetUpRefusesUncleared).
const pushNoCredential = "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled"

// envHeldM1 brings m1 up with its probe passed, deals it one card and has the card
// finish refused at push for want of credentials: m1 is held by its environment
// (sprint.HeldByEnv) with the open member judgment, as fleet up --probe passed must
// release.
func (h *harness) envHeldM1() {
	h.t.Helper()
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 4}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 1, Brief: proBrief}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	h.failTake("s1-1.w1", pushNoCredential)
	ctl := h.snap().MemberCtl("m1")
	require.NotNil(h.t, ctl, "m1's control card")
	require.Equal(h.t, sprint.HeldByEnv, ctl.F(sprint.FieldHeldBy), "m1 is held by its environment: %v", ctl.Fields)
	require.Len(h.t, h.openOf(sprint.NMemberEnvFault), 1, "one open member environment judgment")
}

// fleet up --probe passed (FleetReq{Op: "release", Probe: EnvProbePassed}) releases an
// environment-held member through the twin store's manifest check: the step is accepted,
// the hold and the fault's fields go, the member is up and the judgment closes. Before
// the fix the plan unsets the four hold fields twice in one entry and the store refuses
// the whole step (table fleet: ARGS) (tla/ProviderBudget.tla, FleetUpRefusesUncleared).
func TestFleetUpProbeReleasesAnEnvironmentHeldMember(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.envHeldM1()

	res := h.run(FleetStep(sprint.FleetReq{Op: "release", Member: "m1", Probe: sprint.EnvProbePassed, Fresh: true}))
	require.Empty(t, res.Refused, "fleet up --probe passed is accepted: %v", res.Refused)

	ctl := h.snap().MemberCtl("m1")
	require.NotNil(t, ctl, "m1's control card")
	for _, f := range []string{"held", sprint.FieldHeldBy, sprint.FieldHeldReason, sprint.FieldHeldFinish,
		sprint.FieldEnvFault, sprint.FieldEnvFaultReason, sprint.FieldEnvFaultAt, sprint.FieldEnvProbe} {
		assert.Empty(t, ctl.F(f), "%s is cleared: %v", f, ctl.Fields)
	}
	assert.Equal(t, sprint.Up, ctl.F("status"), "m1 is up: %v", ctl.Fields)
	assert.Empty(t, h.openOf(sprint.NMemberEnvFault), "the member environment judgment is closed")
}

// A passed probe never lifts a person's hold: m1 held by the coordinator (fleet hold,
// no held_by environment) and environment-faulted, then fleet up --probe passed clears
// the fault's fields and leaves the coordinator's hold for a person to lift
// (tla/ProviderBudget.tla, FaultClearsOnPush).
func TestFleetUpProbeLeavesACoordinatorsHold(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 4}))
	h.must(AddStep(sprint.AddReq{Stream: "s1", Count: 1, Brief: proBrief}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	wc := h.snap().Fleet.Card("s1-1.w1")
	require.NotNil(t, wc, "s1-1.w1")
	require.Equal(t, "m1", wc.Row, "the one card is dealt to the one member up")
	gens := map[string]int{wc.ID: wc.Int("gen")}
	h.must(TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: gens, Who: wc.Row}))
	// the coordinator holds m1, letting its working card finish
	h.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m1", Who: "coordinator", Reason: "looking", Finish: true}))
	heldAt := h.snap().MemberCtl("m1").F("held")
	require.NotEmpty(t, heldAt, "the coordinator's hold is on: %v", h.snap().MemberCtl("m1").Fields)
	// its card fails at push: the fault is recorded BESIDE the coordinator's hold
	h.must(FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: gens, Failed: true, Report: pushNoCredential, Who: wc.Row}))
	ctl := h.snap().MemberCtl("m1")
	require.Equal(t, sprint.EnvPush, ctl.F(sprint.FieldEnvFault), "the fault is recorded: %v", ctl.Fields)
	require.Equal(t, heldAt, ctl.F("held"), "the coordinator's hold stamp is untouched: %v", ctl.Fields)
	require.Empty(t, ctl.F(sprint.FieldHeldBy), "the fault did not take the coordinator's hold: %v", ctl.Fields)

	res := h.run(FleetStep(sprint.FleetReq{Op: "release", Member: "m1", Probe: sprint.EnvProbePassed, Fresh: true}))
	require.Empty(t, res.Refused, "fleet up --probe passed is accepted: %v", res.Refused)
	ctl = h.snap().MemberCtl("m1")
	for _, f := range []string{sprint.FieldEnvFault, sprint.FieldEnvFaultReason, sprint.FieldEnvFaultAt, sprint.FieldEnvProbe} {
		assert.Empty(t, ctl.F(f), "the fault's %s is cleared: %v", f, ctl.Fields)
	}
	assert.Equal(t, heldAt, ctl.F("held"), "the coordinator's hold stays: %v", ctl.Fields)
	assert.Equal(t, "looking", ctl.F(sprint.FieldHeldReason), "with its reason: %v", ctl.Fields)
	assert.Empty(t, h.openOf(sprint.NMemberEnvFault), "the member environment judgment is closed")
}
