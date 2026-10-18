package sprint_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestTheGuardAndRuleElevenCountOnlyBenchesTheLandLoopUses pins the count both the hold
// guard (HoldNames) and rule 11 use on the benches the land loop runs on
// (Snapshot.LandBenches), not every gate-capable member: the server's own machine is no
// bench to the land loop, so a fleet whose only gate-capable member is the server's is
// one landing would have refused. Members studio and m1, both up, Self = "studio":
// LandBenches is [m1]; holding m1 is refused by the guard; with m1 held --return and a
// stream merging with a queued card, rule 11 names the stream; with Self empty both
// behave as GateMembers. Time starts at 1000.0 seconds.
func TestTheGuardAndRuleElevenCountOnlyBenchesTheLandLoopUses(t *testing.T) {
	t.Parallel()
	s := &sprint.Snapshot{Now: time.Unix(1000, 0), Work: sprint.NewTable(sprint.Work),
		Readers: sprint.NewTable(sprint.Readers), Merge: sprint.NewTable(sprint.Merge),
		Fleet: sprint.NewTable(sprint.Fleet), Coordinator: "coordinator"}
	for _, m := range []string{"studio", "m1"} {
		s.Fleet.Put(&sprint.Card{ID: sprint.CtlID(m), Row: m, Col: sprint.Ctl,
			Fields: map[string]string{"kind": "member", "status": sprint.Up}})
	}
	s.Fleet.SetRows([]string{"studio", "m1"})
	s.Merge.Put(&sprint.Card{ID: sprint.CtlID("s1"), Row: "s1", Col: sprint.Ctl,
		Fields: map[string]string{"state": sprint.StreamMerging}})
	s.Merge.Put(&sprint.Card{ID: "s1-1", Row: "s1", Col: sprint.Queued, Fields: map[string]string{"primary": "s1-1"}})
	s.Merge.SetRows([]string{"s1"})
	stamp := "2026-01-01T00:00:00Z"
	quiet := func() bool {
		for _, v := range sprint.Check(s, nil) {
			if v.Rule == 11 && strings.Contains(v.Detail, "no member the land loop can use") {
				return false
			}
		}
		return true
	}

	// An empty self behaves as GateMembers: both members are benches, nothing is refused
	// and rule 11 is quiet.
	assert.Equal(t, []string{"studio", "m1"}, s.LandBenches(""), "with no self every gate-capable member is a bench")
	p := sprint.HoldNames(s, sprint.HoldReq{Names: []string{"m1"}, Reason: "r"})
	assert.Empty(t, p.Refused, "with no self holding m1 leaves studio: %v", p.Refused)
	assert.True(t, quiet(), "with no self rule 11 is quiet: %v", sprint.Check(s, nil))

	// Self = "studio": the server's own machine is no bench, so the land loop can use m1
	// alone, and holding m1 leaves no usable bench: the guard refuses it.
	s.Self = "studio"
	assert.Equal(t, []string{"m1"}, s.LandBenches("studio"), "the server's own machine is no bench")
	p = sprint.HoldNames(s, sprint.HoldReq{Names: []string{"m1"}, Reason: "r", Self: "studio"})
	require.Len(t, p.Refused, 1, "holding the last usable bench is refused: %+v", p.Refused)
	assert.Contains(t, p.Refused[0].Why, "leaves no gate bench")

	// With m1 taken down with fleet down (hold --return) and a stream merging with a
	// queued card, rule 11 has no bench the land loop can use and names the stream.
	m1 := s.Fleet.Placed(sprint.CtlID("m1"))
	m1.Fields["status"] = sprint.Down
	m1.Fields[sprint.FieldHeld] = stamp
	m1.Fields[sprint.FieldHeldReturn] = stamp
	assert.False(t, quiet(), "rule 11 fires with no bench the land loop can use: %v", sprint.Check(s, nil))
}
