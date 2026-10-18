package sprint_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gateUpgradeStore is a store on the twin (store.Mem) with a clock starting at
// 1000.0 seconds and no members yet.
func gateUpgradeStore(t *testing.T) *store.Store {
	t.Helper()
	now := time.Unix(1000, 0)
	n := 0
	st := &store.Store{B: store.NewMem(), Names: sprint.Names{Prefix: "t-"}, Actor: "coordinator",
		Now: func() time.Time { return now }, NewID: func() string { n++; return fmt.Sprint(n) }, Sleep: func(time.Duration) {}}
	require.NoError(t, st.Init(context.Background()))
	return st
}

func mustRun(t *testing.T, st *store.Store, ctx context.Context, step store.Step) {
	t.Helper()
	_, err := st.Run(ctx, step)
	require.NoError(t, err, step.Verb)
}

func gateUpgradeSnap(t *testing.T, st *store.Store) *sprint.Snapshot {
	t.Helper()
	s, err := st.Load(context.Background(), store.All, nil)
	require.NoError(t, err)
	return s
}

func gateUpgradeMember(t *testing.T, st *store.Store, ctx context.Context, m string) {
	t.Helper()
	zero := 0.0
	_, err := st.Beat(ctx, m, &zero, hostload.Source{})
	require.NoError(t, err)
	res, err := st.Run(ctx, store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: m}))
	require.NoError(t, err)
	require.Empty(t, res.Refused, "up %s refused: %v", m, res.Refused)
}

// TestAV16HeldMemberIsNoGateBenchAfterUpgrade: a v1.6 store writes a coordinator
// hold with held and held_reason and no held_gate and no held_return. Such a held
// member is never a gate bench (gateCapable needs held_gate to equal held), so
// GateMembers is the up member alone and holding it is refused by the last-gate
// guard (the v1.6-held member is no bench).
func TestAV16HeldMemberIsNoGateBenchAfterUpgrade(t *testing.T) {
	t.Parallel()
	s := &sprint.Snapshot{Now: time.Unix(1000, 0), Work: sprint.NewTable(sprint.Work),
		Readers: sprint.NewTable(sprint.Readers), Merge: sprint.NewTable(sprint.Merge),
		Fleet: sprint.NewTable(sprint.Fleet), Coordinator: "coordinator"}
	stamp := "2026-01-01T00:00:00Z"
	s.Fleet.Put(&sprint.Card{ID: sprint.CtlID("space"), Row: "space", Col: sprint.Ctl,
		Fields: map[string]string{"kind": "member", "status": sprint.Down, sprint.FieldHeld: stamp, sprint.FieldHeldReason: "fleet down"}})
	s.Fleet.Put(&sprint.Card{ID: sprint.CtlID("amd-1"), Row: "amd-1", Col: sprint.Ctl,
		Fields: map[string]string{"kind": "member", "status": sprint.Up}})
	s.Fleet.SetRows([]string{"space", "amd-1"})

	assert.Equal(t, []string{"amd-1"}, s.GateMembers(), "a v1.6 held member is no gate bench")

	ref := sprint.HoldNames(s, sprint.HoldReq{Names: []string{"amd-1"}, Reason: "filled", Who: "c"})
	require.Len(t, ref.Refused, 1, "holding amd-1 leaves no gate bench: %+v", ref)
	assert.Contains(t, ref.Refused[0].Why, "leaves no gate bench")
}

// TestAPlainHoldMarksItsGateAndFleetDownDoesNot: a coordinator's hold without
// --return writes held_gate equal to held and the member gates; a later hold
// --return unsets held_gate, sets held_return, and the member no longer gates; a
// release unsets both.
func TestAPlainHoldMarksItsGateAndFleetDownDoesNot(t *testing.T) {
	t.Parallel()
	st := gateUpgradeStore(t)
	ctx := context.Background()
	gateUpgradeMember(t, st, ctx, "m1")
	gateUpgradeMember(t, st, ctx, "m2")

	s := gateUpgradeSnap(t, st)
	assert.Equal(t, []string{"m1", "m2"}, s.GateMembers(), "two up members gate")

	_, err := st.Hold(ctx, sprint.HoldReq{Names: []string{"m1"}, Reason: "r", Who: "coordinator"})
	require.NoError(t, err)
	s = gateUpgradeSnap(t, st)
	m1 := s.MemberCtl("m1")
	held := m1.F(sprint.FieldHeld)
	require.NotEmpty(t, held, "the hold stamps held")
	assert.Equal(t, held, m1.F(sprint.FieldHeldGate), "the plain hold marks the gate with its own stamp")
	assert.Empty(t, m1.F(sprint.FieldHeldReturn), "no return on a plain hold")
	assert.True(t, sprint.HeldToGate(m1), "the held member is a gate bench")
	assert.Contains(t, s.GateMembers(), "m1", "the marked held member gates")

	_, err = st.Hold(ctx, sprint.HoldReq{Names: []string{"m1"}, Reason: "r", Who: "coordinator", Return: true})
	require.NoError(t, err)
	s = gateUpgradeSnap(t, st)
	m1 = s.MemberCtl("m1")
	held = m1.F(sprint.FieldHeld)
	assert.Empty(t, m1.F(sprint.FieldHeldGate), "fleet down clears the gate mark")
	assert.Equal(t, held, m1.F(sprint.FieldHeldReturn), "the return mark carries the held stamp")
	assert.NotContains(t, s.GateMembers(), "m1", "the fleet-downed member does not gate")

	_, err = st.Hold(ctx, sprint.HoldReq{Names: []string{"m1"}, Who: "coordinator", Release: true})
	require.NoError(t, err)
	s = gateUpgradeSnap(t, st)
	m1 = s.MemberCtl("m1")
	assert.Empty(t, m1.F(sprint.FieldHeldGate), "a release unsets the gate mark")
	assert.Empty(t, m1.F(sprint.FieldHeldReturn), "a release unsets the return mark")
}
