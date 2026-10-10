package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// The gate (tla/SeatHook.tla, Gate): only a record the server wrote for a live
// subscription, proven over it and within its challenge's bound, lets a coordinator
// command run, and then only while no delivered push waits past its bound.
func TestTheHookGateRefusesEverythingButALiveProvenHook(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 23, 0, 0, 0, time.UTC)
	hooked := HookRecord{Name: "rowan", Session: "s1", State: HookHooked, Since: now.Add(-time.Minute)}
	proven := HookProven(hooked, now.Add(-time.Minute))

	for _, tc := range []struct {
		name string
		rec  HookRecord
		ok   bool
		at   time.Time
	}{
		{"no record", HookRecord{}, false, now},
		{"hooked, never proven (CommandSafe: noproof)", hooked, true, now},
		{"another seat's hook", HookRecord{Name: "stella", Session: "s1", State: HookHooked, Proven: now, Live: now.Add(time.Hour)}, true, now},
		{"away", HookRecord{Name: "rowan", Session: "s1", State: HookAway, Proven: now, Live: now.Add(time.Hour)}, true, now},
		{"gone", HookRecord{Name: "rowan", Session: "s1", State: HookGone, Proven: now, Live: now.Add(time.Hour)}, true, now},
		{"past its bound: the server died or missed (nomiss)", proven, true, proven.Live.Add(time.Second)},
		{"no subscription id", HookRecord{Name: "rowan", State: HookHooked, Proven: now, Live: now.Add(time.Hour)}, true, now},
	} {
		assert.Equal(t, HookFirst, HookGate(tc.rec, tc.ok, "rowan", tc.at), tc.name)
	}
	assert.Equal(t, "You must hook in first: run nova-sprint hook", HookFirst, "the owner's words, exactly")

	// live and proven: it runs, up to the last instant of its bound
	assert.Empty(t, HookGate(proven, true, "rowan", now))
	assert.Empty(t, HookGate(proven, true, "rowan", proven.Live))
	assert.Equal(t, now.Add(-time.Minute).Add(HookEvery+HookAnswerBound), proven.Live)

	// a push delivered and not acknowledged blocks once past its bound, naming it
	// (AckedOrBlocking)
	pushed := proven
	pushed.Unacked, pushed.Oldest = 3, &HookItem{ID: 7, What: "j-123", At: now}
	assert.Empty(t, HookGate(pushed, true, "rowan", now.Add(HookAckBound)), "within its bound")
	why := HookGate(pushed, true, "rowan", now.Add(HookAckBound+time.Second))
	assert.True(t, strings.HasPrefix(why, "You must acknowledge push 7 first (j-123, delivered 2026-10-10T23:00:00Z"), why)
	assert.Contains(t, why, "run: nova-sprint hook --ack 7")

	assert.Equal(t, "state=none proven=- unacked=0", HookSaid(HookRecord{}, false, "rowan", now))
	assert.Equal(t, "state=unproven proven=- unacked=0", HookSaid(hooked, true, "rowan", now))
	assert.Equal(t, "state=hooked proven=2026-10-10T22:59:00Z unacked=3", HookSaid(pushed, true, "rowan", now))
}

// The seat check says the hook: DOWN with the gate's line and the remedy while there is
// no live, proven hook; OK with its age when there is.
func TestTheSeatCheckSaysTheHook(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 23, 0, 0, 0, time.UTC)
	line := func(h HookM) (SeatCheckLine, bool) {
		for _, l := range JudgeSeatCheck(SeatCheckMeasures{Hook: h, Errs: map[string]string{}}, now).Lines {
			if l.Thing == SeatCheckHook {
				return l, true
			}
		}
		return SeatCheckLine{}, false
	}
	_, ok := line(HookM{})
	assert.False(t, ok, "a check that did not read the hook says no line")
	l, ok := line(HookM{Measured: true, Holder: "rowan"})
	assert.True(t, ok)
	assert.False(t, l.Up)
	assert.Equal(t, "nova-sprint hook", l.Remedy)
	assert.Contains(t, strings.Join(l.Facts, " "), "holder=rowan state=none unacked=0 proven=- why=\""+HookFirst+"\"")
	rec := HookProven(HookRecord{Name: "rowan", Session: "s1", State: HookHooked}, now.Add(-2*time.Minute))
	l, _ = line(HookM{Measured: true, Holder: "rowan", Record: rec, Recorded: true})
	assert.True(t, l.Up, "%v", l.Facts)
	assert.Contains(t, strings.Join(l.Facts, " "), "state=hooked unacked=0 proven=")
}
