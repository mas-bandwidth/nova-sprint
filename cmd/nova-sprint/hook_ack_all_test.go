package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// hook --ack-all acknowledges every push the running hook has sent up to now, in
// the session it holds (SeatHook.tla, Ack: the same cumulative acknowledgement
// as --ack <last id> --session <its session>): three pushes, then one ack-all
// over the in-process hook leaves none open.
func TestHookAckAllAcknowledgesEveryPushUpToNow(t *testing.T) {
	t.Parallel()
	const name = "hook-ack-all"
	ta, _ := hookSprint(t, name)
	c := ta.hookOn(name)
	c.next("hello")
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
	ta.busProven(name)

	ta.lined(func() { ta.ok("add --stream s1 --count 1 --one") })
	ta.lined(func() { ta.ok("add --stream s2 --count 1 --one") })
	ta.lined(func() { ta.ok("add --stream s3 --count 1 --one") })
	ta.deal(3)
	ta.ok("start")
	for _, id := range []string{"s1-1", "s2-1", "s3-1"} {
		card := ta.fleetCard(id + ".w1")
		require.NotNil(t, card)
		ta.ok("take --as " + card.Row + " " + card.ID + "@1")
		ta.ok("finish --as " + card.Row + " " + card.ID + "@1")
	}
	ta.ok("ask")
	for _, id := range []string{"s1-1", "s2-1", "s3-1"} {
		ta.hookCIRedOnce(id, "run-1")
	}
	s := ta.hookSession()
	require.NotNil(t, s)
	pushes := lookAndCollect(t, s, c)
	require.Len(t, pushes, 3, "three pushes before ack-all: %+v", pushes)
	var last uint64
	for _, p := range pushes {
		require.Equal(t, c.session, p.Session, "push carries its session: %+v", p)
		if p.ID > last {
			last = p.ID
		}
	}
	rec, ok := ta.hookRecord()
	require.True(t, ok)
	require.Equal(t, 3, rec.Unacked, "three open before ack-all")

	r := c.say(hookMsg{T: "ack-all"})
	require.Equal(t, "ok", r.T, r.Why)
	assert.Equal(t, 3, r.Acked, "acked count: %+v", r)
	assert.Equal(t, last, r.ID, "up-to is the last push id: %+v", r)
	assert.Equal(t, c.session, r.Session, "session is the one the hook holds: %+v", r)
	rec, ok = ta.hookRecord()
	require.True(t, ok)
	assert.Equal(t, 0, rec.Unacked, "none open after ack-all")
	assert.Nil(t, rec.Oldest)

	// with no hook process on this machine, --ack-all is refused
	code, _, errs := ta.do("hook --ack-all --actor " + name)
	assert.Equal(t, 1, code, errs)
	assert.Contains(t, errs, "no hook is running on this machine for "+name)
	// the CLI refuses a token or id beside --ack-all before any hook is asked
	code, _, errs = ta.do("hook --ack-all --session some-token --actor " + name)
	assert.Equal(t, 2, code, errs)
	assert.Contains(t, errs, "--session")
	code, _, errs = ta.do("hook --ack-all --ack 1 --actor " + name)
	assert.Equal(t, 2, code, errs)
}

// On a RUNNING machine the waiting-judgment warning repeats no more often than
// once a minute while its set is unchanged, and at once when a judgment joins
// the set (sprint.JudgmentWarnDue): ten simulated minutes give at most eleven
// warning pushes for one waiting judgment.
func TestTheWarningIsQuietWhileItsSetHoldsOnARunningMachine(t *testing.T) {
	t.Parallel()
	const name = "hook-warn-running"
	ta, _ := hookSprint(t, name)
	c := ta.hookOn(name)
	c.next("hello")
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
	ta.busProven(name)
	s := ta.hookSession()
	require.NotNil(t, s)

	// RUNNING: started, never stopped, with one judgment waiting
	ta.lined(func() { ta.ok("add --stream s1 --count 2") })
	ta.lined(func() { ta.ok("start") })
	ta.lined(func() { ta.ok("remind --in 1m --note look-here --for " + name) })
	ta.step(2 * time.Minute)
	ta.lined(func() { ta.ok("tick") })
	first := lookAndCollect(t, s, c)
	require.Len(t, first, 1, "%+v", first)
	require.NotEqual(t, hookWarnWhat, first[0].What)

	var warnAt []time.Duration
	for i := 1; i <= 600; i++ {
		ta.step(time.Second)
		if i%60 == 0 {
			keepalive(t, c)
		}
		if i%10 == 0 {
			ta.lined(func() { ta.ok("tick") })
		}
		ws := warnings(lookAndCollect(t, s, c))
		require.LessOrEqual(t, len(ws), 1, "one warning a look at most")
		if len(ws) == 1 {
			warnAt = append(warnAt, time.Duration(i)*time.Second)
		}
	}
	require.NotEmpty(t, warnAt)
	assert.Equal(t, sprint.JudgmentWarnAfter, warnAt[0], "first warning at 10s: %v", warnAt)
	assert.LessOrEqual(t, len(warnAt), 11, "ten minutes, at most eleven warnings: %v", warnAt)
	for k := 1; k < len(warnAt); k++ {
		assert.GreaterOrEqual(t, warnAt[k]-warnAt[k-1], sprint.JudgmentWarnEvery, "warnings %d and %d", k-1, k)
	}

	// a second judgment joins: one warning at once, not at the minute.
	// Re-align to a fresh warning first, so the join is well inside its minute.
	aligned := false
	for range 70 {
		ta.step(time.Second)
		if len(warnings(lookAndCollect(t, s, c))) == 1 {
			aligned = true
			break
		}
	}
	require.True(t, aligned, "no warning within 70s to align to")
	require.Equal(t, "ok", c.say(hookMsg{T: "ack-all"}).T)
	// a second, distinct judgment joins (a CI-red judgment, not the timer's)
	ta.deal(1)
	card := ta.fleetCard("s1-1.w1")
	require.NotNil(t, card)
	ta.ok("take --as " + card.Row + " " + card.ID + "@1")
	ta.ok("finish --as " + card.Row + " " + card.ID + "@1")
	ta.ok("ask")
	ta.hookCIRedOnce("s1-1", "run-2")
	// the joining judgment itself is pushed, not warned about yet
	_ = lookAndCollect(t, s, c)
	var joined []hookMsg
	for range 15 {
		ta.step(time.Second)
		joined = warnings(lookAndCollect(t, s, c))
		if len(joined) > 0 {
			break
		}
	}
	require.Len(t, joined, 1, "joining judgment warns at once, within seconds not a minute: %+v", joined)
	assert.Contains(t, joined[0].Text, "WARNING 2 judgment(s)", joined[0].Text)
}
