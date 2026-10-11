package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// The gate over both pushes (docs/SPEC-SPRINT.md, "The two pushes"; SeatHook.tla, Gate
// with the bus push): the sprint push is the server's record, the bus push its own
// record. Every state but both live and proven refuses, naming the first push that is
// not.
func TestTheGateWantsBothPushesLive(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 11, 20, 0, 0, 0, time.UTC)
	seat := HookProven(HookRecord{Name: "rowan", Session: "s1", State: HookHooked}, now.Add(-time.Minute))
	armed := HookShared{Identity: "rowan", Role: HookRoleCoordinator, Pushes: HookCoordinatorPushes, ArmedAt: now.Add(-2 * time.Minute)}
	proven := armed
	proven.ProvedAt = now.Add(-time.Minute)
	crumb := func(push string) string { return HookBreadcrumb("rowan", push, "") }

	for _, tc := range []struct {
		name   string
		call   HookCall
		refuse string
	}{
		{"none", HookCall{Identity: "rowan"}, crumb(HookPushSprint)},
		{"sprint only", HookCall{Identity: "rowan", Seat: seat, SeatOK: true}, crumb(HookPushBus)},
		{"sprint and the bus armed, unproven", HookCall{Identity: "rowan", Seat: seat, SeatOK: true, Shared: armed}, crumb(HookPushBus)},
		{"bus only", HookCall{Identity: "rowan", Shared: proven}, crumb(HookPushSprint)},
		{"both, proven", HookCall{Identity: "rowan", Seat: seat, SeatOK: true, Shared: proven}, ""},
		{"unreadable", HookCall{Identity: "rowan", Seat: seat, SeatOK: true, Shared: proven, Unread: "the bus store is down"}, HookBreadcrumb("rowan", "", "the bus store is down")},
	} {
		assert.Equal(t, tc.refuse, HookGateCall(tc.call, now), tc.name)
	}

	// the breadcrumb: the rule first, then the push, the role, and the exact commands; no
	// (none), and no command that does not ship
	b := crumb(HookPushBus)
	assert.True(t, strings.HasPrefix(b, HookFirst+" --actor rowan (the bus push is not live;"), b)
	for _, want := range []string{"the coordinator rowan wants the sprint push and the bus push live", "nova-sprint hook --prove <challenge> --actor rowan", "nova-sprint unhook --actor rowan"} {
		assert.Contains(t, b, want)
	}
	for _, not := range []string{"\n", "(none)", "nova-hook"} {
		assert.NotContains(t, b, not)
		assert.NotContains(t, HookBreadcrumb("", "", "the store did not open"), not)
	}
	assert.Contains(t, HookBreadcrumb("", "", "the store did not open"), "--actor <seat> (the store did not open;")
}

// The bus push's record (sprint-hook:<identity> in the bus store): its fields round
// trip; the push is live only armed for the coordinator, proven since it was armed, not
// unhooked since, and within its bound; a time that does not read is never.
func TestTheSharedRecordSaysEachPush(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 11, 20, 0, 0, 0, time.UTC)
	h := HookShared{Identity: "rowan", Role: HookRoleCoordinator, Pushes: []string{HookPushSprint, HookPushBus}, ArmedAt: now.Add(-time.Hour), ProvedAt: now.Add(-time.Minute)}
	f := h.Fields()
	assert.Equal(t, map[string]string{"role": "coordinator", "pushes": "sprint,bus", "armed_at": "2026-10-11T19:00:00Z", "proved_at": "2026-10-11T19:59:00Z", "unhooked_at": ""}, f)
	back, ok := HookSharedOf("rowan", f)
	require.True(t, ok)
	assert.Equal(t, h.Pushes, back.Pushes)
	assert.True(t, back.PushLive(HookPushBus, now))
	assert.Equal(t, "sprint-hook:rowan", HookSharedKey("rowan"))

	_, ok = HookSharedOf("rowan", map[string]string{})
	assert.False(t, ok, "no role is no record")

	for _, tc := range []struct {
		name string
		h    func(HookShared) HookShared
		at   time.Time
	}{
		{"another role", func(h HookShared) HookShared { h.Role = "friend"; return h }, now},
		{"the push not armed", func(h HookShared) HookShared { h.Pushes = []string{HookPushSprint}; return h }, now},
		{"never armed", func(h HookShared) HookShared { h.ArmedAt = time.Time{}; return h }, now},
		{"never proven", func(h HookShared) HookShared { h.ProvedAt = time.Time{}; return h }, now},
		{"proven before it was armed again", func(h HookShared) HookShared { h.ArmedAt = now; return h }, now},
		{"unhooked since", func(h HookShared) HookShared { h.UnhookedAt = now.Add(-time.Second); return h }, now},
		{"past its bound", func(h HookShared) HookShared { return h }, h.ProvedAt.Add(HookSharedBound + time.Second)},
		{"a time that does not read", func(h HookShared) HookShared {
			r, _ := HookSharedOf("rowan", map[string]string{"role": "coordinator", "pushes": "sprint,bus", "armed_at": "2026-10-11T19:00:00Z", "proved_at": "yesterday"})
			return r
		}, now},
	} {
		assert.False(t, tc.h(h).PushLive(HookPushBus, tc.at), tc.name)
	}
	// unhooked before the last arm does not count against it; the last instant of the bound is live
	h.UnhookedAt = h.ArmedAt.Add(-time.Second)
	assert.True(t, h.PushLive(HookPushBus, h.ProvedAt.Add(HookSharedBound)))
}

// The waiting-judgment warning (the owner, 2026-10-11: "Any card waiting for judgement
// should alert you via warning every second once it has been in this state for 10
// seconds or longer."): a judgment open 9 s is not warned about; at 10 s it is; still
// open at 11 s, again; answered (no longer in the inbox), none and forgotten. A judgment
// computed at read time (its oldest is always now) is warned about from when the hook
// first saw it. Notes that are not judgments are never warned about.
func TestAJudgmentWaitingTenSecondsIsWarnedEverySecond(t *testing.T) {
	t.Parallel()
	opened := time.Date(2026, 10, 11, 20, 0, 0, 0, time.UTC)
	j := Group{ID: "n-1", Alias: "j7", Kind: Judgment, Oldest: opened, Members: []string{"s1-1", "s1-2"},
		Commands: []Command{{Decision: "accept", Lines: []string{"nova-sprint accept --group n-1 --expect 2"}}}}
	happened := Group{ID: "n-2", Kind: Happened, Oldest: opened.Add(-time.Hour)}
	inbox := []Group{happened, j}
	seen := map[string]time.Time{}

	assert.Empty(t, JudgmentWarnings(inbox, opened.Add(9*time.Second), seen), "9 s: no warning")
	at10 := JudgmentWarnings(inbox, opened.Add(10*time.Second), seen)
	require.Len(t, at10, 1, "10 s: warned")
	assert.Equal(t, JudgmentWarning{Alias: "j7", ID: "n-1", Cards: []string{"s1-1", "s1-2"}, Waited: 10 * time.Second,
		Answer: []string{"accept: nova-sprint accept --group n-1 --expect 2"}}, at10[0])
	at11 := JudgmentWarnings(inbox, opened.Add(11*time.Second), seen)
	require.Len(t, at11, 1, "11 s: warned again")
	assert.Equal(t, 11*time.Second, at11[0].Waited)
	assert.Empty(t, JudgmentWarnings([]Group{happened}, opened.Add(time.Hour), seen), "answered: none")
	assert.Empty(t, seen, "an answered judgment is forgotten")

	// read time: its oldest is the read's own now, every look
	stopped := func(at time.Time) []Group {
		return []Group{{ID: "machine:stopped", Kind: Judgment, Oldest: at}}
	}
	seen = map[string]time.Time{}
	assert.Empty(t, JudgmentWarnings(stopped(opened), opened, seen))
	assert.Empty(t, JudgmentWarnings(stopped(opened.Add(9*time.Second)), opened.Add(9*time.Second), seen))
	ws := JudgmentWarnings(stopped(opened.Add(10*time.Second)), opened.Add(10*time.Second), seen)
	require.Len(t, ws, 1, "a read-time judgment first seen 10 s ago is warned about")
	assert.Equal(t, 10*time.Second, ws[0].Waited)

	// one warning a look, every judgment in it; its one line, and its full text
	k := j
	k.ID, k.Alias, k.Commands, k.Oldest = "n-3", "j8", nil, opened.Add(-time.Minute)
	ws = JudgmentWarnings([]Group{j, k}, opened.Add(10*time.Second), map[string]time.Time{})
	require.Len(t, ws, 2)
	assert.Equal(t, "WARNING 2 judgment(s) waiting on the coordinator 10s or longer: j7 j8", JudgmentWarningSet(ws))
	text := JudgmentWarningText(ws)
	assert.True(t, strings.HasPrefix(text, JudgmentWarningSet(ws)+"; answer each"), text)
	assert.Contains(t, text, "j7 n-1 cards=s1-1,s1-2 waited=10s\n  accept: nova-sprint accept --group n-1 --expect 2\n")
	assert.Contains(t, text, "j8 n-3 cards=s1-1,s1-2 waited=1m10s\n  read it: nova-sprint inbox --open n-3\n")
	assert.Contains(t, text, "an ack of this push does not silence it")
}
