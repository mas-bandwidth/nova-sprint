package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The regression test of 2026-10-11 19:28Z: the running server's read cards were off after
// a clear (the default was off), so the tick never dealt a read: 75 cards sat in review with
// 81 of 140 fleet slots idle until the seat ran ask by hand. A cleared sprint with one card
// worked to review, ticked with no seat action at all, has the card's reads dealt as read
// cards to the other members and taken by them.
func TestAClearedSprintDealsAndTakesItsReadsWithNoSeatAction(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.live = []string{"m1", "m2", "m3"}
	ta.ok("init --readers reader-m1,reader-m2,reader-m3 --members m1:2,m2:2,m3:2")
	ta.ok("set --read-cards off --alarm-review 7 --alarm-fleet 11")
	ta.ok("clear --confirm sprint")
	ta.ok("reader add reader-m1 reader-m2 reader-m3")
	ta.ok("add --stream s1 --one --brief-file " + proBrief(t, "s1-1"))
	ta.ok("start")
	ta.ok("tick")
	ta.ok("tick")
	ta.ok("take --as m1 --limit 1")
	ta.ok("finish --as m1 s1-1.w1@1 --report done")
	// no seat verb from here: the machine's ticks alone
	for range 3 {
		ta.ok("tick")
	}
	var card cardView
	ta.json("card s1-1", &card)
	require.Equal(t, sprint.Review, card.Column, "the fixture: s1-1 in review")
	require.Len(t, card.Reads, 2, "a pro card's two reads are dealt by the tick, no ask")
	rows := map[string]bool{}
	for _, r := range card.Reads {
		assert.Equal(t, "1", r.F(sprint.FieldReadCard), "%s is a read card", r.ID)
		assert.NotEqual(t, "m1", r.Row, "never read by its own worker")
		rows[r.Row] = true
	}
	for row := range rows {
		ta.ok("take --as " + row + " --limit 1")
	}
	ta.json("card s1-1", &card)
	for _, r := range card.Reads {
		assert.Equal(t, sprint.Working, r.Col, "%s is taken by %s", r.ID, r.Row)
	}
	ta.clean()
}

// warnSprint is hookSprint at the working defaults, its one member wide enough for both
// cards.
func warnSprint(t *testing.T, name string) *testApp {
	t.Helper()
	ta := newTestApp(t)
	env := map[string]string{"NOVA_SPRINT_REDIS": "mem:0", "NOVA_SPRINT_ACTOR": name}
	var mu sync.Mutex
	ta.a.getenv = func(k string) string { mu.Lock(); defer mu.Unlock(); return env[k] }
	hookTests.Store(name, true)
	t.Cleanup(func() { hookTests.Delete(name) })
	ta.ok("init --readers reader-a,reader-b --members m1:4 --owner glenn")
	ta.a.serveAddr = "mem:0"
	return ta
}

// pushes is every push the hook delivers on one look of the server, after a tick, each
// acknowledged as the seat does once all are read.
func (c *hookClient) pushes(ta *testApp) []hookMsg {
	c.t.Helper()
	ta.lined(func() { ta.ok("tick") })
	s := ta.hookSession()
	require.NotNil(c.t, s)
	s.look(context.Background())
	var out []hookMsg
	for done := false; !done; {
		select {
		case m, ok := <-c.lines:
			if !ok {
				done = true
			} else if m.T == "push" {
				out = append(out, m)
			}
		case <-time.After(300 * time.Millisecond):
			done = true
		}
	}
	for _, m := range out {
		r := c.say(hookMsg{T: "ack", ID: m.ID})
		require.Equal(c.t, "ok", r.T, r.Why)
	}
	return out
}

// warned is the pushes of the warning type (sprint.NSettingsOff, NReviewOffReadCards).
func warned(ps []hookMsg, typ string) []hookMsg {
	var out []hookMsg
	for _, p := range ps {
		if strings.Contains(p.Text, typ) {
			out = append(out, p)
		}
	}
	return out
}

// pushLines is the push as the seat's hook prints it on its stream (hookSay): the numbered
// PUSH line, then its text.
func pushLines(name string, m hookMsg) string {
	var b bytes.Buffer
	hookSay(&b, name, "mem:0", "test", m)
	return b.String()
}

// A hooked coordinator is pushed, as a numbered PUSH line, every warning the tick writes:
// a setting turned off, naming it and the set that restores it; off, on, off again is pushed
// twice; a clear and nothing else pushes nothing; the same off after the clear is pushed
// again; and with read cards off every card that enters review, each once, a card back in
// review after a clear included. The where view says the settings off too, for a seat that
// is not hooked.
func TestTheHookIsPushedEveryWarning(t *testing.T) {
	t.Parallel()
	const name = "hook-w"
	ta := warnSprint(t, name)
	c := ta.hookOn(name)
	c.next("hello")
	chal := c.next("challenge").Challenge
	ta.waitHook(sprint.HookHooked)
	r := c.say(hookMsg{T: "prove", Challenge: chal})
	require.Equal(t, "ok", r.T, r.Why)
	ta.lined(func() { ta.ok("start") })

	ps := c.pushes(ta)
	require.Empty(t, warned(ps, sprint.NSettingsOff), "every setting at its working value: no warning: %+v", ps)

	ta.lined(func() { ta.ok("set --read-cards off") })
	ps = c.pushes(ta)
	w := warned(ps, sprint.NSettingsOff)
	require.Len(t, w, 1, "set --read-cards off is pushed as a warning: %+v", ps)
	assert.Contains(t, w[0].Text, "WARNING: ")
	assert.Contains(t, w[0].Text, "--read-cards is off, working value on (restore: nova-sprint set --read-cards on)")
	line := pushLines(name, w[0])
	assert.Contains(t, line, fmt.Sprintf("PUSH %d ", w[0].ID), "a numbered PUSH line on the hook")
	assert.Contains(t, line, "read it, then acknowledge: nova-sprint hook --ack "+fmt.Sprint(w[0].ID))
	var where whereView
	ta.lined(func() { ta.json("where", &where) })
	assert.Contains(t, where.SettingsOff, "--read-cards is off", "where says it too, for a seat with no hook")

	ta.lined(func() { ta.ok("set --read-cards on") })
	require.Empty(t, warned(c.pushes(ta), sprint.NSettingsOff), "back on: nothing to say")
	ta.lined(func() { ta.ok("set --read-cards off") })
	require.Len(t, warned(c.pushes(ta), sprint.NSettingsOff), 1, "off, on, off: the second off is pushed again")

	ta.lined(func() { ta.ok("reader add reader-a reader-b") })
	ta.lined(func() { ta.ok("add --stream s1 --count 2") })
	ta.lined(func() { ta.ok("tick") })
	ta.lined(func() { ta.ok("tick") })
	ta.lined(func() { ta.ok("take --as m1 --limit 2") })
	ta.lined(func() { ta.ok("finish --as m1 s1-1.w1@1 --report done") })
	ps = c.pushes(ta)
	w = warned(ps, sprint.NReviewOffReadCards)
	require.Len(t, w, 1, "s1-1 into review with read cards off is pushed: %+v", ps)
	assert.Contains(t, w[0].Text, "s1-1@1 entered review")
	assert.Contains(t, pushLines(name, w[0]), fmt.Sprintf("PUSH %d ", w[0].ID))

	ta.lined(func() { ta.ok("finish --as m1 s1-2.w1@1 --report done") })
	w = warned(c.pushes(ta), sprint.NReviewOffReadCards)
	require.Len(t, w, 1, "s1-2 into review is pushed too, s1-1 not again")
	assert.Contains(t, w[0].Text, "s1-2@1 entered review (1 more already in review")

	// a clear, then nothing else: every setting back at its working value, nothing pushed;
	// the same off in the new epoch, and the same card id back in review, are pushed again
	ta.lined(func() { ta.ok("clear --confirm sprint") })
	ta.lined(func() { ta.ok("start") })
	require.Empty(t, warned(c.pushes(ta), sprint.NSettingsOff), "a clear and nothing else: no warning")
	ta.lined(func() { ta.ok("set --read-cards off") })
	require.Len(t, warned(c.pushes(ta), sprint.NSettingsOff), 1, "the same off after a clear is pushed again")
	ta.lined(func() { ta.ok("reader add reader-a reader-b") })
	ta.lined(func() { ta.ok("add --stream s1 --count 1 --one") })
	ta.lined(func() { ta.ok("tick") })
	ta.lined(func() { ta.ok("tick") })
	ta.lined(func() { ta.ok("take --as m1 --limit 1") })
	ta.lined(func() { ta.ok("finish --as m1 s1-1.w1@1 --report done") })
	w = warned(c.pushes(ta), sprint.NReviewOffReadCards)
	require.Len(t, w, 1, "s1-1 in review in the new epoch is pushed again")
	assert.Contains(t, w[0].Text, "s1-1@1 entered review")
}
