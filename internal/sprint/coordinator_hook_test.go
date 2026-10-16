package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/hook"
)

// provenCoordinator is name's nova-hook record proven as the coordinator: both pushes of
// the role live and proven, the session's answer in at answered, the heartbeat's evidence
// standing.
func provenCoordinator(name string, answered time.Time) hook.Status {
	return hook.Status{Name: name, Role: hook.Coordinator, State: hook.StateProven, Epoch: 3, AnswerAt: answered,
		Pushes: []hook.PushStatus{{Push: hook.Bus, State: hook.PushProven}, {Push: hook.Sprint, State: hook.PushProven}}}
}

// The gate reads the nova-hook record alone (v1.3.6: the sprint's own seat hook is
// retired): a coordinator command runs only on a record hooked as the coordinator with
// every push of the role live and proven and the heartbeat's evidence standing; every other
// record refuses, the refusal leading with the owner's words and naming the nova-hook
// command that hooks the coordinator in, and never a retired sprint verb.
func TestTheCoordinatorGateReadsTheNovaHookRecord(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	proven := provenCoordinator("rowan", now.Add(-time.Minute))
	assert.Empty(t, CoordinatorGate("rowan", proven), "proven as the coordinator: it runs")

	sprintDown := proven
	sprintDown.State = hook.StatePartial
	sprintDown.Pushes = []hook.PushStatus{{Push: hook.Bus, State: hook.PushProven}, {Push: hook.Sprint, State: hook.PushDown}}
	busArmed := proven
	busArmed.State = hook.StateLive
	busArmed.Pushes = []hook.PushStatus{{Push: hook.Bus, State: hook.PushArmed}, {Push: hook.Sprint, State: hook.PushProven}}
	noAnswer := proven
	noAnswer.Down = hook.NoAnswerReason
	comms := hook.Status{Name: "rowan", Role: hook.Comms, State: hook.StateProven, Pushes: []hook.PushStatus{{Push: hook.Bus, State: hook.PushProven}}}

	for _, tc := range []struct {
		name string
		st   hook.Status
		want string
	}{
		{"never hooked", hook.Status{Name: "rowan", State: hook.StateNone}, "no hook record"},
		{"unhooked (two misses)", hook.Status{Name: "rowan", State: hook.StateUnhooked, Unhooked: now, Reason: "missed 2 challenges"}, "unhooked at"},
		{"the sprint push down: no server holds it", sprintDown, "partial as coordinator"},
		{"the bus push armed, the challenge not answered", busArmed, "live as coordinator"},
		{"proven, and no answer within the window", noAnswer, hook.NoAnswerReason},
		{"hooked as comms: the coordinator's verbs are not comms'", comms, "this act is only for coordinator"},
	} {
		why := CoordinatorGate("rowan", tc.st)
		require.NotEmpty(t, why, tc.name)
		assert.True(t, strings.HasPrefix(why, HookFirst+": "), "%s: %s", tc.name, why)
		assert.Contains(t, why, tc.want, tc.name)
		assert.Contains(t, why, "run: nova-hook hook --as rowan --role coordinator under a Monitor", tc.name)
		assert.NotContains(t, why, "nova-sprint"+" hook", "%s: the retired verb is never offered", tc.name)
		assert.NotContains(t, why, "\n", tc.name)
	}

	// the sprint push's holder is named as the server, in place of nova-tools' line
	why := CoordinatorGate("rowan", sprintDown)
	assert.Contains(t, why, "the sprint push of a coordinator is held by: "+SprintPushHeldBy)
	assert.Contains(t, why, "see it: nova-hook status --as rowan")

	// a record that cannot be read refuses (fail closed), naming why and the remedy
	unread := HookUnread("rowan", "dial tcp: refused")
	assert.True(t, strings.HasPrefix(unread, HookFirst+": the nova-hook record of rowan could not be checked (dial tcp: refused)"), unread)
	assert.Contains(t, unread, "nova-hook hook --as rowan --role coordinator")
	assert.Contains(t, HookUnread("", "no store"), "nova-hook status --as <seat>")
}

// The seat's status says the record in nova-hook's words, with the session's last answer.
func TestTheSeatStatusSaysTheNovaHookRecord(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
	assert.Equal(t, "state=proven role=coordinator epoch=3 bus=proven sprint=proven answered=1m0s_ago", HookSaid(provenCoordinator("rowan", now.Add(-time.Minute)), now))
	down := hook.Status{Name: "rowan", State: hook.StateNone, Down: hook.NeverRanReason}
	assert.Equal(t, "state=none down=hook_server_never_ran", HookSaid(down, now))
}

// The seat check says the hook: DOWN with the gate's line and the nova-hook remedy while
// the record does not let a coordinator command run; OK with the last answer when it does.
func TestTheSeatCheckSaysTheHook(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)
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
	l, ok := line(HookM{Measured: true, Holder: "rowan", Status: hook.Status{Name: "rowan", State: hook.StateNone}})
	require.True(t, ok)
	assert.False(t, l.Up)
	assert.Equal(t, "nova-hook hook --as rowan --role coordinator", l.Remedy)
	assert.Contains(t, strings.Join(l.Facts, " "), "holder=rowan state=none why=\""+HookFirst+": rowan is not hooked in")
	l, _ = line(HookM{Measured: true, Holder: "rowan", Status: provenCoordinator("rowan", now.Add(-2*time.Minute))})
	assert.True(t, l.Up, "%v", l.Facts)
	assert.Equal(t, "holder=rowan state=proven role=coordinator answered=2m0s ago", strings.Join(l.Facts, " "))
}

// The gate accepts a quiet, live coordinator (v1.3.17): nova-hook serve challenges a name only
// after its silence, so a coordinator proven at t and silent since has no recent challenge to
// answer, and the sprint, which reads the record through the pinned hook package's liveness rule
// (Served.Down), must run it at t+4m as nova-hook status says proven. A record the hook says
// expired (silent past the rule's bound) or unhooked still refuses. Time is an argument; no
// store, no clock, no socket.
func TestTheGateAcceptsASilentProvenCoordinatorUntilTheHookSaysOtherwise(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC)
	const every = 30 * time.Second
	// a heartbeat server that passed a moment before each read, reading at now
	serving := func(now time.Time) hook.Served {
		return hook.Served{OK: true, At: now.Add(-time.Second), Now: now, Every: every, DownAfter: 2 * time.Minute}
	}
	read := func(st hook.Status, now time.Time) hook.Status {
		st.Down = serving(now).Down(st)
		return st
	}
	proven := provenCoordinator("rowan", t0)

	assert.Empty(t, CoordinatorGate("rowan", read(proven, t0.Add(30*time.Second))), "just proven: it runs")
	assert.Empty(t, CoordinatorGate("rowan", read(proven, t0.Add(4*time.Minute))),
		"proven at t, silent with no challenge since: it still runs at t+4m")

	expired := read(proven, t0.Add(2*time.Hour))
	assert.Equal(t, hook.NoAnswerReason, expired.Down, "the hook reads a name silent for hours down")
	assert.Contains(t, CoordinatorGate("rowan", expired), hook.NoAnswerReason)

	unhooked := hook.Status{Name: "rowan", State: hook.StateUnhooked, Unhooked: t0.Add(4 * time.Minute), Reason: "missed 2 challenges"}
	assert.Contains(t, CoordinatorGate("rowan", read(unhooked, t0.Add(4*time.Minute))), "unhooked at")
}
