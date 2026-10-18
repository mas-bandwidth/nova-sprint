package sprint_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestGateMembersAreHeldNotDealt pins the two filters apart: of six members in row order
// (up, held by the coordinator, taken down by fleet down, held with --return, held by the
// machine's fault mark, down and not held), the
// deal filter UpMembers keeps the up member alone (tla/DirtyTick.tla NeverDealtToDraining)
// and GateMembers keeps the up member and the plainly held one, in row order
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
		{"m-held", map[string]string{"kind": "member", "status": sprint.Down, sprint.FieldHeld: "2026-01-01T00:00:00Z", sprint.FieldHeldReason: "red", sprint.FieldHeldFinish: "2026-01-01T00:00:00Z"}},
		// fleet down (hold --return): no reason; a member up when downed keeps its finish marker
		{"m-fleetdown", map[string]string{"kind": "member", "status": sprint.Down, sprint.FieldHeld: "2026-01-01T00:00:00Z", sprint.FieldHeldFinish: "2026-01-01T00:00:00Z"}},
		// hold --reason --return of a member already down: no finish marker
		{"m-returned", map[string]string{"kind": "member", "status": sprint.Down, sprint.FieldHeld: "2026-01-01T00:00:00Z", sprint.FieldHeldReason: "red"}},
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
	assert.Equal(t, []string{"m-up", "m-held"}, s.GateMembers(), "a gate may run on the up member and the plainly held one, in row order, never a fleet-downed or --return one")

	// a coordinator hold that carries a fault stamp is the machine's, and no gate bench
	s.Fleet.Placed(sprint.CtlID("m-held")).Fields[sprint.FieldFaultSince] = "2026-01-01T00:00:00Z"
	assert.Equal(t, []string{"m-up"}, s.GateMembers(), "a held member with fault_since is left out")
}
