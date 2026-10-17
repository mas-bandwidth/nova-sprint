package sprint_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// holdGateSnapshot is a sprint with the members named (true is up, false is a
// coordinator's hold: status down, held stamped, no held_by) and one stream merging
// with one card queued. Time starts at 1000.0 seconds.
func holdGateSnapshot(members map[string]bool) *sprint.Snapshot {
	s := &sprint.Snapshot{Now: time.Unix(1000, 0), Work: sprint.NewTable(sprint.Work),
		Readers: sprint.NewTable(sprint.Readers), Merge: sprint.NewTable(sprint.Merge),
		Fleet: sprint.NewTable(sprint.Fleet), Coordinator: "coordinator"}
	rows := make([]string, 0, len(members))
	for m, up := range members {
		rows = append(rows, m)
		fields := map[string]string{"kind": "member", "status": sprint.Down}
		if up {
			fields["status"] = sprint.Up
		} else {
			fields["held"] = "2026-01-01T00:00:00Z"
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
// merging with a queued card while every member is held is named, and one member up
// is enough to leave it unsaid.
func TestCheckFlagsMergingCardsWithNoGateBench(t *testing.T) {
	t.Parallel()
	held := holdGateSnapshot(map[string]bool{"m1": false, "m2": false})
	vs := noGateViolations(sprint.Check(held, nil))
	require.Len(t, vs, 1, "check does not name it: %v", sprint.Check(held, nil))
	assert.Contains(t, vs[0].Detail, "stream s1 has 1 merging card(s) and no member can run a land gate: nova-sprint fleet unhold <member>")

	up := holdGateSnapshot(map[string]bool{"m1": true, "m2": false})
	assert.Empty(t, noGateViolations(sprint.Check(up, nil)), "one member up leaves a gate bench: %v", sprint.Check(up, nil))
}

// TestHoldRefusesTheLastGateBench is hold's own guard: holding m1 leaves m2, holding
// m2 then leaves no bench and is refused naming it, and a member already down is never
// the hold refused.
func TestHoldRefusesTheLastGateBench(t *testing.T) {
	t.Parallel()
	s := holdGateSnapshot(map[string]bool{"m1": true, "m2": true})
	p := sprint.HoldNames(s, sprint.HoldReq{Names: []string{"m1"}, Reason: "r"})
	assert.Empty(t, p.Refused, "holding m1 leaves m2 up: %v", p.Refused)

	after := holdGateSnapshot(map[string]bool{"m1": false, "m2": true})
	p = sprint.HoldNames(after, sprint.HoldReq{Names: []string{"m2"}, Reason: "r"})
	require.Len(t, p.Refused, 1, "holding m2 after m1 leaves no bench: %+v", p)
	assert.Contains(t, p.Refused[0].Why, "holding m2 leaves no gate bench")
	assert.Contains(t, p.Refused[0].Why, "--force-last holds it anyway")

	down := holdGateSnapshot(map[string]bool{"m1": false, "m2": false})
	p = sprint.HoldNames(down, sprint.HoldReq{Names: []string{"m1"}, Reason: "r"})
	assert.Empty(t, p.Refused, "m1 is already down: %v", p.Refused)
}

// TestAPlantThatRemovesTheLastGateBenchGuardGoesRed is the plant: with the guard
// switched off the hold TestHoldRefusesTheLastGateBench forbids succeeds, so a guard
// removed from HoldNames is caught.
func TestAPlantThatRemovesTheLastGateBenchGuardGoesRed(t *testing.T) {
	s := holdGateSnapshot(map[string]bool{"m1": true, "m2": true})
	p := sprint.HoldNames(s, sprint.HoldReq{Names: []string{"m1", "m2"}, Reason: "r"})
	require.Len(t, p.Refused, 1, "the guard refuses the hold that leaves no bench: %+v", p)
	assert.Contains(t, p.Refused[0].Why, "holding m2 leaves no gate bench")
	assert.Equal(t, "m2", p.Refused[0].Key, "it names the last member that could run a gate")

	p = sprint.HoldNames(s, sprint.HoldReq{Names: []string{"m1", "m2"}, Reason: "r", NoLastGateGuard: true})
	assert.Empty(t, p.Refused, "with the guard removed the hold succeeds, which TestHoldRefusesTheLastGateBench forbids")
}
