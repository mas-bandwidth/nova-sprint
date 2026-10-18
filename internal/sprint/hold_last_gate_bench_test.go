package sprint_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// holdGateState says a member's state: "up" is up, "coord" is held by the coordinator
// (status down, held stamped, no held_by), "down" is held by the machine (status down,
// held_by=fault). Time starts at 1000.0 seconds.
func holdGateSnapshot(state map[string]string) *sprint.Snapshot {
	s := &sprint.Snapshot{Now: time.Unix(1000, 0), Work: sprint.NewTable(sprint.Work),
		Readers: sprint.NewTable(sprint.Readers), Merge: sprint.NewTable(sprint.Merge),
		Fleet: sprint.NewTable(sprint.Fleet), Coordinator: "coordinator"}
	rows := make([]string, 0, len(state))
	for m, st := range state {
		rows = append(rows, m)
		fields := map[string]string{"kind": "member", "status": sprint.Down}
		switch st {
		case "up":
			fields["status"] = sprint.Up
		case "coord":
			fields[sprint.FieldHeld] = "2026-01-01T00:00:00Z"
		case "ret": // the coordinator took it down with fleet down (hold --return)
			fields[sprint.FieldHeld] = "2026-01-01T00:00:00Z"
			fields[sprint.FieldHeldReturn] = "2026-01-01T00:00:00Z"
		default: // "down": a machine holds it (a fault), no recoverable bench
			fields[sprint.FieldHeld] = "2026-01-01T00:00:00Z"
			fields[sprint.FieldHeldBy] = sprint.HeldByFault
		}
		s.Fleet.Put(&sprint.Card{ID: sprint.CtlID(m), Row: m, Col: sprint.Ctl, Fields: fields})
	}
	s.Fleet.SetRows(rows)
	s.Merge.SetRows([]string{"s1"})
	s.Merge.Put(&sprint.Card{ID: sprint.CtlID("s1"), Row: "s1", Col: sprint.Ctl, Fields: map[string]string{"state": sprint.StreamMerging}})
	s.Merge.Put(&sprint.Card{ID: "s1-1", Row: "s1", Col: sprint.Queued, Fields: map[string]string{"primary": "s1-1"}})
	return s
}

func noGateViolations(vs []sprint.Violation) []sprint.Violation {
	var out []sprint.Violation
	for _, v := range vs {
		if strings.Contains(v.Detail, "no member can run a land gate") {
			out = append(out, v)
		}
	}
	return out
}

// TestCheckFlagsMergingCardsWithNoGateBench is the check's half of the guard: a stream
// merging with a queued card while no member is up is named, and one member up is enough
// to leave it unsaid. The check asks for a gate bench (Snapshot.GateMembers): a member
// the coordinator holds is one, a member taken down with fleet down (hold --return) is not.
func TestCheckFlagsMergingCardsWithNoGateBench(t *testing.T) {
	t.Parallel()
	coord := holdGateSnapshot(map[string]string{"m1": "coord", "m2": "coord"})
	assert.Empty(t, noGateViolations(sprint.Check(coord, nil)), "a coordinator-held member is a gate bench: %v", sprint.Check(coord, nil))
	held := holdGateSnapshot(map[string]string{"m1": "ret", "m2": "ret"})
	vs := noGateViolations(sprint.Check(held, nil))
	require.Len(t, vs, 1, "check does not name it: %v", sprint.Check(held, nil))
	assert.Contains(t, vs[0].Detail, "stream s1 has 1 merging card(s) and no member can run a land gate: nova-sprint fleet unhold <member>")

	up := holdGateSnapshot(map[string]string{"m1": "up", "m2": "coord"})
	assert.Empty(t, noGateViolations(sprint.Check(up, nil)), "one member up leaves a gate bench: %v", sprint.Check(up, nil))
}

// TestHoldRefusesTheLastGateBench is hold's own guard: holding m1 leaves m2; a machine
// holds m1 (no recoverable bench) so holding m2 is refused naming it; a coordinator-held
// m1 is still a recoverable bench, so holding m2 is allowed; a member a machine already
// holds is never the hold refused.
func TestHoldRefusesTheLastGateBench(t *testing.T) {
	t.Parallel()
	s := holdGateSnapshot(map[string]string{"m1": "up", "m2": "up"})
	p := sprint.HoldNames(s, sprint.HoldReq{Names: []string{"m1"}, Reason: "r"})
	assert.Empty(t, p.Refused, "holding m1 leaves m2 up: %v", p.Refused)

	// m1 held by the machine: holding m2 (the last up member) leaves no bench and is
	// refused naming m2
	machineHeld := holdGateSnapshot(map[string]string{"m1": "down", "m2": "up"})
	p = sprint.HoldNames(machineHeld, sprint.HoldReq{Names: []string{"m2"}, Reason: "r"})
	require.Len(t, p.Refused, 1, "holding m2 with m1 machine-held leaves no bench: %+v", p)
	assert.Contains(t, p.Refused[0].Why, "holding m2 leaves no gate bench")
	assert.Contains(t, p.Refused[0].Why, "--force-last holds it anyway")

	// a coordinator-held m1 is a recoverable bench: holding m2 leaves it, so it is
	// not refused
	coordHeld := holdGateSnapshot(map[string]string{"m1": "coord", "m2": "up"})
	p = sprint.HoldNames(coordHeld, sprint.HoldReq{Names: []string{"m2"}, Reason: "r"})
	assert.Empty(t, p.Refused, "m1 is a recoverable coordinator hold: %v", p.Refused)

	down := holdGateSnapshot(map[string]string{"m1": "down", "m2": "down"})
	p = sprint.HoldNames(down, sprint.HoldReq{Names: []string{"m1"}, Reason: "r"})
	assert.Empty(t, p.Refused, "m1 is already machine-held: %v", p.Refused)
}

// TestHoldForceLastRecordsTheOverride pins the override on the happened note: --force-last
// holds the last bench (m1 is machine-held, m2 is the last up member) and says so on the
// hold's own note.
func TestHoldForceLastRecordsTheOverride(t *testing.T) {
	t.Parallel()
	s := holdGateSnapshot(map[string]string{"m1": "down", "m2": "up"})
	p := sprint.HoldNames(s, sprint.HoldReq{Names: []string{"m2"}, Reason: "r", ForceLast: true})
	require.Empty(t, p.Refused, "the override holds it anyway: %+v", p.Refused)
	var line string
	for _, n := range p.Notes {
		if strings.Contains(n.What, "member m2 held") {
			line = n.What
		}
	}
	require.NotEmpty(t, line, "the hold's note names m2: %+v", p.Notes)
	assert.True(t, strings.HasSuffix(line, "--force-last: no gate bench left"), line)
}

// TestAPlantThatRemovesTheLastGateBenchGuardGoesRed is the plant: with the guard
// switched off the hold TestHoldRefusesTheLastGateBench forbids succeeds, so a guard
// removed from HoldNames is caught.
func TestAPlantThatRemovesTheLastGateBenchGuardGoesRed(t *testing.T) {
	s := holdGateSnapshot(map[string]string{"m1": "down", "m2": "up"})
	p := sprint.HoldNames(s, sprint.HoldReq{Names: []string{"m2"}, Reason: "r"})
	require.Len(t, p.Refused, 1, "the guard refuses the hold that leaves no bench: %+v", p)
	assert.Contains(t, p.Refused[0].Why, "holding m2 leaves no gate bench")
	assert.Equal(t, "m2", p.Refused[0].Key, "it names the last member that could run a gate")

	p = sprint.HoldNames(s, sprint.HoldReq{Names: []string{"m2"}, Reason: "r", NoLastGateGuard: true})
	assert.Empty(t, p.Refused, "with the guard removed the hold succeeds, which TestHoldRefusesTheLastGateBench forbids")
}
