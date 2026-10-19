package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The bench lease keeps to the member's facts (docs/SPEC-SPRINT.md section 18;
// tla/BenchLease.tla, GrantOnlyWhereTheFactsAre). Time starts at 1000.0 seconds and no
// real time, socket or host fact is read.

func benchNeedWorld(t *testing.T, now time.Time) *world {
	t.Helper()
	w := newWorld(t, "reader-a")
	w.s.Now = now
	for _, member := range []string{"darwin", "linux"} {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: member, Width: 8}))
	}
	w.s.MemberCtl("darwin").Fields[FieldFacts] = "darwin,go"
	w.s.MemberCtl("linux").Fields[FieldFacts] = "linux,go"
	return w
}

func TestALeasePickKeepsToBenchesWhoseFactsMeetTheNeeds(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	w := benchNeedWorld(t, now)
	beats := map[string]Beat{
		"darwin": {At: now, Load: 0, Cores: 64},
		"linux":  {At: now, Load: 0, Cores: 8},
	}

	require.Equal(t, []BenchRoom{{Machine: "linux", Slots: 1, Busy: 0}},
		BenchRooms(w.s, beats, "SELF", now, "linux"),
		"the linux take's rooms are the benches whose facts carry linux")

	lanes, answer := Lanes{}.Pick("alice", BenchRooms(w.s, beats, "SELF", now, "linux"), now)
	require.True(t, answer.Granted)
	require.Equal(t, "linux", answer.Machine, "the empty darwin bench is never granted to a linux take")
	require.Empty(t, lanes["darwin"].Holders, "no lease lands on the darwin bench")
	require.Len(t, lanes["linux"].Holders, 1)
}

func TestALeasePickWithNoNeedsIsUnchanged(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	w := benchNeedWorld(t, now)
	beats := map[string]Beat{
		"darwin": {At: now, Load: 0, Cores: 64},
		"linux":  {At: now, Load: 0, Cores: 8},
	}
	rooms := BenchRooms(w.s, beats, "SELF", now)
	require.Equal(t, []BenchRoom{
		{Machine: "darwin", Slots: 8, Busy: 0},
		{Machine: "linux", Slots: 1, Busy: 0},
	}, rooms)

	_, answer := Lanes{}.Pick("alice", rooms, now)
	require.True(t, answer.Granted)
	require.Equal(t, "darwin", answer.Machine, "with no needs the emptiest bench is still the pick")
}

func TestALeaseNobodyMeetsWaitsWithTheNeedNamed(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	w := benchNeedWorld(t, now)
	beats := map[string]Beat{
		"darwin": {At: now, Load: 0, Cores: 64},
		"linux":  {At: now, Load: 0, Cores: 8},
	}
	all := BenchRooms(w.s, beats, "SELF", now)
	rooms := BenchRooms(w.s, beats, "SELF", now, "windows", "tla")

	lanes, answer := Lanes{}.Pick("alice", rooms, now)
	require.False(t, answer.Granted, "no bench meets the needs, so the answer is the lease's WAIT")
	require.Empty(t, answer.Full)
	require.Equal(t, Lanes{}, lanes, "a wait writes nothing")

	why := BenchNeedsWhy([]string{"windows", "tla"}, all, w.s)
	require.Contains(t, why, "GATE-NEEDS windows", "the wait names the need")
	require.Contains(t, why, "darwin (facts: darwin,go)", "and the facts the benches carry")
	require.Contains(t, why, "linux (facts: linux,go)")
}
