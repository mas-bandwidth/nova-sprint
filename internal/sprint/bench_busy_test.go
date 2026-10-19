package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAOneSlotBenchAtLowLoadIsLeasable(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	w := newWorld(t, "reader-a")
	w.s.Now = now
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m", Width: 8}))

	beats := map[string]Beat{
		"m": {At: now, Load: 12, Cores: 8},
	}
	rooms := BenchRooms(w.s, beats, "SELF", now)
	require.Equal(t, []BenchRoom{{Machine: "m", Slots: 1, Busy: 0}}, rooms)

	lanes := Lanes{}
	next, answer := lanes.Pick("w1", rooms, now)
	require.True(t, answer.Granted)
	require.Equal(t, "m", answer.Machine)
	require.NotEqual(t, lanes, next)
}

func TestASixteenCoreBenchAtOnePercentReadsBusyZero(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	w := newWorld(t, "reader-a")
	w.s.Now = now
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m", Width: 8}))

	beats := map[string]Beat{
		"m": {At: now, Load: 1, Cores: 16},
	}
	rooms := BenchRooms(w.s, beats, "SELF", now)
	require.Equal(t, []BenchRoom{{Machine: "m", Slots: 2, Busy: 0}}, rooms)
}

func TestABenchAtFullLoadIsStillFull(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	w := newWorld(t, "reader-a")
	w.s.Now = now
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "full", Width: 8}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "free", Width: 8}))

	beats := map[string]Beat{
		"full": {At: now, Load: 50, Cores: 8},
		"free": {At: now, Load: 0, Cores: 8},
	}
	rooms := BenchRooms(w.s, beats, "SELF", now)
	require.Equal(t, []BenchRoom{
		{Machine: "full", Slots: 1, Busy: 1},
		{Machine: "free", Slots: 1, Busy: 0},
	}, rooms)

	fullRooms := BenchRooms(w.s, map[string]Beat{"full": {At: now, Load: 100, Cores: 8}}, "SELF", now)
	require.Equal(t, []BenchRoom{{Machine: "full", Slots: 1, Busy: 1}}, fullRooms)

	lanes := Lanes{}
	_, answer := lanes.Pick("w1", rooms, now)
	require.True(t, answer.Granted)
	require.Equal(t, "free", answer.Machine)
}
