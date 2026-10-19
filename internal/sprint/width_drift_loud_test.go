package sprint

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var widthClock = time.Unix(1000, 0).UTC()

// A drift is held at the take but loud (the owner, 2026-10-18: "every fleet machine and every
// friend follows the one width definition in nova-config, with no exceptions"; tla/WidthHard.tla
// OneSource): the tick raises one judgment per drifting row, once per drift, and clears it when
// the row agrees again; the doctor reports a friend's width drift as it does a member's; and a
// clear carries a drained row's width_from=config mark.

func TestTickJudgesEachDriftingRow(t *testing.T) {
	// it sets the package's width read, so it does not run in parallel
	w := newWorld(t, "reader-a", "reader-b")
	w.s.Now = widthClock
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 8}))
	w.s.Friends = []FriendSeat{workerSeatFixture(FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash"})}

	SetWidthConfigRead(func(context.Context) (map[string]int, map[string]int, bool) {
		return map[string]int{"m1": 4}, map[string]int{"amy": 4}, true
	})
	defer SetWidthConfigRead(nil)
	p, _ := tickWidthDrift(w.s, TickReq{Who: MachineActor})
	var raised []Note
	for _, n := range p.Notes {
		if n.Type == NWidthDrift {
			raised = append(raised, n)
		}
	}
	require.Len(t, raised, 2, "one judgment per drifting row: %v", raised)
	var subs []string
	for _, n := range raised {
		subs = append(subs, n.What)
	}
	assert.Contains(t, strings.Join(subs, "\n"), "m1: sprint width 8, nova-config width 4")
	assert.Contains(t, strings.Join(subs, "\n"), "amy: sprint width 2, nova-config width 4")
	w.must(p) // the judgments are now open

	// the same drift, the next tick: no new judgment (once per drift, not one per tick)
	p2, _ := tickWidthDrift(w.s, TickReq{Who: MachineActor})
	var again []Note
	for _, n := range p2.Notes {
		if n.Type == NWidthDrift {
			again = append(again, n)
		}
	}
	assert.Empty(t, again, "no re-raise while the rows still drift")

	// the rows agree again: the judgments close
	SetWidthConfigRead(func(context.Context) (map[string]int, map[string]int, bool) {
		return map[string]int{"m1": 8}, map[string]int{"amy": 2}, true
	})
	p3, _ := tickWidthDrift(w.s, TickReq{Who: MachineActor})
	require.Len(t, p3.Closes, 2, "the drift judgments close when the rows agree: %v", p3.Closes)
}

func TestDoctorReportsFriendWidthDrift(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.s.Now = widthClock
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 8}))
	w.s.Friends = []FriendSeat{workerSeatFixture(FriendSeat{Name: "amy", Width: 2, Status: Up, Class: "flash"})}

	in := DoctorInput{Snap: w.s, Req: TickReq{Friends: w.s.Friends}, FriendWidths: map[string]int{"amy": 4}}
	var fails []DoctorCheck
	for _, c := range DoctorFriends(in) {
		if c.Status == DoctorFail {
			fails = append(fails, c)
		}
	}
	require.Len(t, fails, 1, "a friend's width drift is a FAIL: %v", fails)
	assert.Contains(t, fails[0].Evidence, "width: amy: sprint width 2, nova-config width 4")

	// her row agrees again: no fail
	in.FriendWidths = map[string]int{"amy": 2}
	fails = nil
	for _, c := range DoctorFriends(in) {
		if c.Status == DoctorFail {
			fails = append(fails, c)
		}
	}
	assert.Empty(t, fails, "no fail when her row agrees")
}

func TestDrainWidthMarkSurvivesClear(t *testing.T) {
	t.Parallel()
	w := newWorld(t, "reader-a", "reader-b")
	w.s.Now = widthClock
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 0})) // drains
	ctl := w.s.MemberCtl("m1")
	require.NotNil(t, ctl)
	// the config-drain mark, as the width refresh writes a draining member
	ctl.Fields[FieldWidthFrom] = WidthFromConfig

	sh := ShapeOf(w.s)
	require.Equal(t, WidthFromConfig, sh.WidthFrom["m1"], "the clear's shape carries the config-drain mark")

	// a restored epoch's control card carries it too
	w2 := newWorld(t, "reader-a", "reader-b")
	w2.s.Now = widthClock
	p := RestoreShape(w2.s, sh)
	w2.must(p)
	ctl2 := w2.s.MemberCtl("m1")
	require.NotNil(t, ctl2)
	assert.Equal(t, WidthFromConfig, ctl2.F(FieldWidthFrom), "a drained row stays marked as following the config across a clear")
}
