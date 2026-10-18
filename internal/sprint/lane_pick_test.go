package sprint

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBenchLeasePickTakesTheLeastCommittedBenchAndNeverPilesOn(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	rooms := []BenchRoom{
		{Machine: "a", Slots: 8},
		{Machine: "b", Slots: 2},
		{Machine: "c", Slots: 2, Busy: 2},
	}

	pickTen := func(t *testing.T) Lanes {
		t.Helper()
		lanes := Lanes{}
		for i := 1; i <= 10; i++ {
			next, answer := lanes.Pick("w"+strconv.Itoa(i), rooms, now)
			require.True(t, answer.Granted, "worker %d was not granted: %+v", i, answer)
			lanes = next
			counts := map[string]int{}
			for machine, lane := range lanes {
				counts[machine] = len(lane.Holders)
				require.LessOrEqual(t, len(lane.Holders), roomSlots(rooms, machine), "%s exceeded slots", machine)
			}
			if i == 10 {
				require.Equal(t, map[string]int{"a": 8, "b": 2}, counts)
			}
		}
		return lanes
	}

	t.Run("spread and full wait", func(t *testing.T) {
		t.Parallel()
		lanes := pickTen(t)
		next, answer := lanes.Pick("w11", rooms, now)
		require.False(t, answer.Granted)
		require.Equal(t, []BenchRoom{rooms[0], rooms[1], rooms[2]}, answer.Full)
		require.True(t, answer.RetryAt.Equal(now.Add(LaneWaitFor)), "RetryAt = %s", answer.RetryAt)
		require.Equal(t, lanes, next, "a wait writes nothing")
	})

	t.Run("renewal", func(t *testing.T) {
		t.Parallel()
		lanes := pickTen(t)
		renewAt := now.Add(time.Second)
		next, answer := lanes.Pick("w1", rooms, renewAt)
		require.True(t, answer.Granted)
		require.True(t, answer.Renewed)
		require.Equal(t, "a", answer.Machine)
		require.Equal(t, renewAt, next["a"].Holders[0].Seen)
	})

	t.Run("expiry", func(t *testing.T) {
		t.Parallel()
		lanes := pickTen(t)
		expireAt := now.Add(LaneHoldFor + time.Second)
		next, answer := lanes.Pick("w11", rooms, expireAt)
		require.True(t, answer.Granted)
		require.ElementsMatch(t, []string{"w1", "w2", "w3", "w4", "w5", "w6", "w7", "w8", "w9", "w10"}, answer.Released)
		require.Len(t, next[answer.Machine].Holders, 1)
	})

	t.Run("give all", func(t *testing.T) {
		t.Parallel()
		lanes := pickTen(t)
		freed, gave := lanes.GiveAll("w2", now.Add(time.Second))
		require.True(t, gave)
		require.NotContains(t, holderNames(freed["b"]), "w2")
		next, answer := freed.Pick("w11", rooms, now.Add(2*time.Second))
		require.True(t, answer.Granted)
		require.Equal(t, "b", answer.Machine, "the pick takes the freed slot")
		require.Len(t, next["b"].Holders, 2)
	})
}

func roomSlots(rooms []BenchRoom, machine string) int {
	for _, room := range rooms {
		if room.Machine == machine {
			return room.Slots
		}
	}
	return 0
}

func holderNames(l Lane) []string {
	names := make([]string, 0, len(l.Holders))
	for _, hold := range l.Holders {
		names = append(names, hold.Who)
	}
	return names
}

func TestBenchLeaseRoomsComeFromTheFleetTable(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	w := newWorld(t, "reader-a")
	w.s.Now = now
	members := []struct {
		name  string
		width int
	}{
		{"a16", 8},
		{"b64", 8},
		{"small", 8},
		{"held", 8},
		{"quiet", 8},
		{"noroom", 8},
		{"zero", 0},
		{"self", 8},
		{"stale", 8},
		{"down", 8},
	}
	for _, member := range members {
		w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: member.name, Width: member.width}))
	}
	w.s.MemberCtl("held").Fields["status"] = Held
	w.s.MemberCtl("zero").Fields[FieldWidth] = DrainWidth
	w.s.MemberCtl("down").Fields["status"] = Down
	w.s.Fleet.SetProp(QuietPropPrefix+"quiet", (Quiet{
		Member: "quiet", Until: now.Add(time.Hour), At: now,
	}).value())
	w.s.NoRoom = map[string]string{"noroom": "no free space"}

	beats := map[string]Beat{
		"a16":    {At: now, Load: 99.9, Cores: 16},
		"b64":    {At: now, Load: 24.2, Cores: 64},
		"small":  {At: now, Cores: 4},
		"quiet":  {At: now, Cores: 16},
		"noroom": {At: now, Cores: 16},
		"zero":   {At: now, Cores: 16},
		"self":   {At: now, Cores: 16},
		"stale":  {At: now.Add(-BeatDeadline - time.Second), Cores: 16},
		"down":   {At: now, Cores: 16},
	}
	rooms := BenchRooms(w.s, beats, "SELF", now)
	require.Equal(t, []BenchRoom{
		{Machine: "a16", Slots: 2, Busy: 2},
		{Machine: "b64", Slots: 8, Busy: 2},
		{Machine: "small", Slots: 1, Busy: 0},
	}, rooms)
}
