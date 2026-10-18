package sprint_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestGateMembersAreHeldNotDealt pins the two filters apart: of four members in row order
// (up, held by the coordinator, held by the machine's fault mark, down and not held), the
// deal filter UpMembers keeps the up member alone (tla/DirtyTick.tla NeverDealtToDraining)
// and GateMembers keeps the up member and the coordinator-held one, in row order
// (tla/DirtyTick.tla LandGate, HeldMayGateNeverDealt). Time starts at 1000.0 seconds.
func TestGateMembersAreHeldNotDealt(t *testing.T) {
	t.Parallel()
	s := &sprint.Snapshot{Now: time.Unix(1000, 0), Work: sprint.NewTable(sprint.Work),
		Readers: sprint.NewTable(sprint.Readers), Merge: sprint.NewTable(sprint.Merge),
		Fleet: sprint.NewTable(sprint.Fleet), Coordinator: "coordinator"}
	members := []struct {
		name   string
		fields map[string]string
	}{
		{"m-up", map[string]string{"kind": "member", "status": sprint.Up}},
		{"m-held", map[string]string{"kind": "member", "status": sprint.Down, sprint.FieldHeld: "2026-01-01T00:00:00Z"}},
		{"m-fault", map[string]string{"kind": "member", "status": sprint.Down, sprint.FieldHeld: "2026-01-01T00:00:00Z", sprint.FieldHeldBy: sprint.HeldByFault}},
		{"m-down", map[string]string{"kind": "member", "status": sprint.Down}},
	}
	rows := make([]string, 0, len(members))
	for _, m := range members {
		rows = append(rows, m.name)
		s.Fleet.Put(&sprint.Card{ID: sprint.CtlID(m.name), Row: m.name, Col: sprint.Ctl, Fields: m.fields})
	}
	s.Fleet.SetRows(rows)
	assert.Equal(t, []string{"m-up"}, s.UpMembers(), "the deal filter keeps the up member alone")
	assert.Equal(t, []string{"m-up", "m-held"}, s.GateMembers(), "a gate may run on the up member and the coordinator-held one, in row order")

	// a coordinator hold that carries a fault stamp is the machine's, and no gate bench
	s.Fleet.Placed(sprint.CtlID("m-held")).Fields[sprint.FieldFaultSince] = "2026-01-01T00:00:00Z"
	assert.Equal(t, []string{"m-up"}, s.GateMembers(), "a held member with fault_since is left out")
	delete(s.Fleet.Placed(sprint.CtlID("m-held")).Fields, sprint.FieldFaultSince)

	// a coordinator's hold made with --return (fleet down) carries held_return = its held
	// stamp: the member is out of service and no gate bench; a held_return left from an
	// earlier hold (another stamp) marks nothing
	held := s.Fleet.Placed(sprint.CtlID("m-held"))
	held.Fields[sprint.FieldHeldReturn] = held.Fields[sprint.FieldHeld]
	assert.True(t, sprint.HeldToReturn(held), "the return carries the hold's stamp")
	assert.Equal(t, []string{"m-up"}, s.GateMembers(), "a returned member is left out")
	held.Fields[sprint.FieldHeldReturn] = "2025-01-01T00:00:00Z"
	assert.False(t, sprint.HeldToReturn(held), "a stale return marks nothing")
	assert.Equal(t, []string{"m-up", "m-held"}, s.GateMembers(), "a stale return leaves the hold a gate bench")
	assert.Equal(t, []string{"m-up"}, s.UpMembers(), "the deal filter is unchanged")
}

// TestAFleetDownCrossesAClearAndStillRunsNoGate: an epoch clear carries a fleet down's
// return mark (HeldCarried) and rewrites it to the new held stamp (RestoreShape), so the
// member is still no gate bench in the new epoch; a coordinator hold without --return
// stays a bench, and a stale mark (another hold's stamp) is not carried.
func TestAFleetDownCrossesAClearAndStillRunsNoGate(t *testing.T) {
	t.Parallel()
	tables := func(now int64) *sprint.Snapshot {
		return &sprint.Snapshot{Now: time.Unix(now, 0), Work: sprint.NewTable(sprint.Work),
			Readers: sprint.NewTable(sprint.Readers), Merge: sprint.NewTable(sprint.Merge),
			Fleet: sprint.NewTable(sprint.Fleet), Coordinator: "coordinator"}
	}
	old := tables(1000)
	heldAt := "2026-01-01T00:00:00Z"
	for _, m := range []struct {
		name string
		ret  string
	}{{"m-down", heldAt}, {"m-hold", ""}, {"m-stale", "2025-01-01T00:00:00Z"}} {
		f := map[string]string{"kind": "member", "status": sprint.Down, sprint.FieldHeld: heldAt}
		if m.ret != "" {
			f[sprint.FieldHeldReturn] = m.ret
		}
		old.Fleet.Put(&sprint.Card{ID: sprint.CtlID(m.name), Row: m.name, Col: sprint.Ctl, Fields: f})
	}
	old.Fleet.SetRows([]string{"m-down", "m-hold", "m-stale"})
	require.Equal(t, []string{"m-hold", "m-stale"}, old.GateMembers(), "before the clear the fleet-downed member is no bench")

	fresh := tables(2000)
	p := sprint.RestoreShape(fresh, sprint.ShapeOf(old))
	require.Empty(t, p.Refused)
	for _, u := range p.Units {
		for _, c := range u.Changes {
			if c.Table == sprint.Fleet {
				fresh.Fleet.Put(&sprint.Card{ID: c.Entry.ID, Row: c.Entry.Create.Row, Col: sprint.Ctl, Fields: c.Entry.Set})
			}
		}
	}
	fresh.Fleet.SetRows([]string{"m-down", "m-hold", "m-stale"})
	down := fresh.MemberCtl("m-down")
	require.NotNil(t, down)
	assert.NotEqual(t, heldAt, down.F(sprint.FieldHeld), "the clear stamps the hold again")
	assert.True(t, sprint.HeldToReturn(down), "the return mark follows the new held stamp")
	assert.Empty(t, fresh.MemberCtl("m-stale").F(sprint.FieldHeldReturn), "a stale mark is not carried")
	assert.Equal(t, []string{"m-hold", "m-stale"}, fresh.GateMembers(), "after the clear the fleet-downed member still runs no gate")
}

// TestRuleElevenCountsHeldGateBenches: check.go rule 11 names a merging stream with a
// queued card only when no member can run a land gate (Snapshot.GateMembers): a member
// the coordinator holds is a bench, one taken down with fleet down (held_return) is not.
func TestRuleElevenCountsHeldGateBenches(t *testing.T) {
	t.Parallel()
	s := &sprint.Snapshot{Now: time.Unix(1000, 0), Work: sprint.NewTable(sprint.Work),
		Readers: sprint.NewTable(sprint.Readers), Merge: sprint.NewTable(sprint.Merge),
		Fleet: sprint.NewTable(sprint.Fleet), Coordinator: "coordinator"}
	stamp := "2026-01-01T00:00:00Z"
	s.Fleet.Put(&sprint.Card{ID: sprint.CtlID("m1"), Row: "m1", Col: sprint.Ctl,
		Fields: map[string]string{"kind": "member", "status": sprint.Down, sprint.FieldHeld: stamp}})
	s.Fleet.SetRows([]string{"m1"})
	s.Merge.Put(&sprint.Card{ID: sprint.CtlID("s1"), Row: "s1", Col: sprint.Ctl,
		Fields: map[string]string{"state": sprint.StreamMerging}})
	s.Merge.Put(&sprint.Card{ID: "s1-1", Row: "s1", Col: sprint.Queued, Fields: map[string]string{}})
	s.Merge.SetRows([]string{"s1"})
	noGate := func() bool {
		for _, v := range sprint.Check(s, nil) {
			if v.Rule == 11 && strings.Contains(v.Detail, "no member can run a land gate") {
				return true
			}
		}
		return false
	}
	assert.False(t, noGate(), "a coordinator-held member is a gate bench: rule 11 is quiet")
	s.Fleet.Placed(sprint.CtlID("m1")).Fields[sprint.FieldHeldReturn] = stamp
	assert.True(t, noGate(), "a fleet-downed member is no gate bench: rule 11 names the stream")
}
