package store

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envFaultHeld brings m1 up with its probe passed, deals it one card and fails
// that card's finish at push for want of credentials: the member is held by its
// environment, the way internal/sprint's env_fault_test.go does (tla/ProviderBudget.tla).
func envFaultHeld(t *testing.T, h *harness) {
	t.Helper()
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 1}))
	h.must(AddStep(sprint.AddReq{Brief: proBrief, Stream: "s1", Count: 1}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	c := h.snap().Fleet.Card(h.snap().Work.Card("s1-1").F("work"))
	h.must(TakeStep(sprint.TakeReq{As: c.Row, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: 1}}))
	h.must(FinishStep(sprint.FinishReq{Failed: true, Report: "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled", Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: 1}}))
}

// envFaultOpen is the open member environment-fault judgments on the member.
func envFaultOpen(h *harness, member string) []sprint.Open {
	open, err := h.m.OpenNotes(h.ctx)
	require.NoError(h.t, err)
	var out []sprint.Open
	for _, o := range open {
		if o.Note.Type == sprint.NMemberEnvFault && o.Subject() == sprint.MemberSubject(member) {
			out = append(out, o)
		}
	}
	return out
}

// fleet up --probe passed releases a member held by an environment fault: the
// step is accepted, the hold's fields and the fault's record go, the member is
// up, and the open judgment is closed (tla/ProviderBudget.tla,
// FleetUpRefusesUncleared). The store's manifest check (rule ARGS) refused the
// step before the fix because the release path and EnvProbePatch each unset the
// hold's four fields once.
func TestFleetUpProbePassedReleasesAnEnvironmentHeldMember(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	envFaultHeld(t, h)

	ctl := h.snap().MemberCtl("m1")
	require.NotEmpty(t, ctl.F("held"), "the member is held: %v", ctl.Fields)
	require.Equal(t, sprint.HeldByEnv, ctl.F(sprint.FieldHeldBy), "held by its environment: %v", ctl.Fields)
	require.NotEmpty(t, envFaultOpen(h, "m1"), "an environment judgment is open")

	// what fleet up --probe passed sends (cmd/nova-sprint/fleet.go, fleetStep)
	res := h.run(FleetStep(sprint.FleetReq{Op: "release", Member: "m1", Probe: sprint.EnvProbePassed, Fresh: true}))
	require.Empty(t, res.Refused, "fleet up --probe passed was refused: %+v", res.Refused)

	ctl = h.snap().MemberCtl("m1")
	assert.Empty(t, ctl.F("held"), "the hold is gone: %v", ctl.Fields)
	assert.Empty(t, ctl.F(sprint.FieldHeldBy), "and its mark: %v", ctl.Fields)
	assert.Empty(t, ctl.F(sprint.FieldHeldReason), "and its reason: %v", ctl.Fields)
	assert.Empty(t, ctl.F(sprint.FieldHeldFinish), "and its finish marker: %v", ctl.Fields)
	assert.Empty(t, ctl.F(sprint.FieldEnvFault), "and the fault's record: %v", ctl.Fields)
	assert.Empty(t, ctl.F(sprint.FieldEnvFaultReason), "its reason: %v", ctl.Fields)
	assert.Empty(t, ctl.F(sprint.FieldEnvFaultAt), "its stamp: %v", ctl.Fields)
	assert.Empty(t, ctl.F(sprint.FieldEnvProbe), "its probe: %v", ctl.Fields)
	assert.Equal(t, string(sprint.Up), ctl.F("status"), "the member is up: %v", ctl.Fields)
	assert.Empty(t, envFaultOpen(h, "m1"), "the environment judgment is closed")
}

// fleet up --probe passed on a member held by the coordinator AND
// environment-faulted clears the fault's fields but keeps the coordinator's
// hold: a probe never lifts a person's hold (docs/SPEC-SPRINT.md section 5, the
// fleet; tla/ProviderBudget.tla, FleetUpRefusesUncleared).
func TestFleetUpProbePassedKeepsACoordinatorsHold(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 1}))
	h.must(AddStep(sprint.AddReq{Brief: proBrief, Stream: "s1", Count: 1}))
	h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1"}}}))
	c := h.snap().Fleet.Card(h.snap().Work.Card("s1-1").F("work"))
	h.must(TakeStep(sprint.TakeReq{As: c.Row, Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: 1}}))
	// the coordinator holds m1, letting its working card finish
	h.must(FleetStep(sprint.FleetReq{Op: "hold", Member: "m1", Who: "coordinator", Reason: "looking", Finish: true}))
	// then its card fails at push: the fault records beside the coordinator's hold
	h.must(FinishStep(sprint.FinishReq{Failed: true, Report: "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled", Sel: sprint.Sel{IDs: []string{c.ID}}, Gens: map[string]int{c.ID: 1}}))

	ctl := h.snap().MemberCtl("m1")
	require.NotEmpty(t, ctl.F("held"), "the coordinator's hold is on: %v", ctl.Fields)
	require.Empty(t, ctl.F(sprint.FieldHeldBy), "the coordinator's hold carries no mark: %v", ctl.Fields)
	require.NotEmpty(t, ctl.F(sprint.FieldEnvFault), "the fault is recorded: %v", ctl.Fields)

	h.must(FleetStep(sprint.FleetReq{Op: "release", Member: "m1", Probe: sprint.EnvProbePassed, Fresh: true}))

	ctl = h.snap().MemberCtl("m1")
	assert.Empty(t, ctl.F(sprint.FieldEnvFault), "the fault's fields go: %v", ctl.Fields)
	assert.Empty(t, ctl.F(sprint.FieldEnvFaultReason), "its reason: %v", ctl.Fields)
	assert.Empty(t, ctl.F(sprint.FieldEnvFaultAt), "its stamp: %v", ctl.Fields)
	assert.Empty(t, ctl.F(sprint.FieldEnvProbe), "its probe: %v", ctl.Fields)
	assert.NotEmpty(t, ctl.F("held"), "the coordinator's hold stays: %v", ctl.Fields)
	assert.Equal(t, "looking", ctl.F(sprint.FieldHeldReason), "with its reason: %v", ctl.Fields)
}
