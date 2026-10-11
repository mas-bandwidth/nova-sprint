package main

import (
	"bytes"
	"context"
	"fmt"
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
	ta.defaults = true
	ta.live = []string{"m1", "m2", "m3"}
	ta.ok("init --readers reader-m1,reader-m2,reader-m3 --members m1:2,m2:2,m3:2")
	ta.ok("set --read-cards off --alarm-review off --alarm-fleet off")
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

// warnSprint is hookSprint at the working defaults: the sprint init makes keeps read cards on.
func warnSprint(t *testing.T, name string) *testApp {
	t.Helper()
	ta := newTestApp(t)
	ta.defaults = true
	env := map[string]string{"NOVA_SPRINT_REDIS": "mem:0", "NOVA_SPRINT_ACTOR": name}
	var mu sync.Mutex
	ta.a.getenv = func(k string) string { mu.Lock(); defer mu.Unlock(); return env[k] }
	hookTests.Store(name, true)
	t.Cleanup(func() { hookTests.Delete(name) })
	ta.ok("init --readers reader-a,reader-b --members m1:4 --owner glenn")
	ta.a.serveAddr = "mem:0"
	return ta
}

// pushes is every push the hook delivers on one look of the server, after a tick.
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
	// each acknowledged, as the seat does, once all are read
	for _, m := range out {
		r := c.say(hookMsg{T: "ack", ID: m.ID})
		require.Equal(c.t, "ok", r.T, r.Why)
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

// warned is the pushes of the warning (hookWarnSettings, hookWarnReview).
func warned(ps []hookMsg, what string) []hookMsg {
	var out []hookMsg
	for _, p := range ps {
		if p.What == what {
			out = append(out, p)
		}
	}
	return out
}

// A hooked coordinator is pushed a WARNING over the hook when a design setting is turned
// off, naming it and the set that restores it, and one for every card that enters review
// while read cards are off; a clear with nothing else pushes no warning (every setting is
// back at its working default).
func TestTheHookIsPushedAWarningForEverySettingOffAndEveryCardIntoReviewWithReadCardsOff(t *testing.T) {
	t.Parallel()
	const name = "hook-w"
	ta := warnSprint(t, name)
	c := ta.hookOn(name)
	c.next("hello")
	chal := c.next("challenge").Challenge
	ta.waitHook(sprint.HookHooked)
	r := c.say(hookMsg{T: "prove", Challenge: chal})
	require.Equal(t, "ok", r.T, r.Why)

	ta.lined(func() { ta.ok("set --alarm-ready off") })
	ta.lined(func() { ta.ok("clear --confirm sprint") })
	ps := c.pushes(ta)
	assert.Empty(t, warned(ps, hookWarnSettings), "a clear and nothing else: every setting at its default, no warning: %+v", ps)

	ta.lined(func() { ta.ok("set --read-cards off") })
	ps = c.pushes(ta)
	w := warned(ps, hookWarnSettings)
	require.Len(t, w, 1, "set --read-cards off is pushed as a warning: %+v", ps)
	assert.Contains(t, w[0].Text, "WARNING: ")
	assert.Contains(t, w[0].Text, "--read-cards is off, working default on (restore: nova-sprint set --read-cards on)")
	line := pushLines(name, w[0])
	assert.Contains(t, line, fmt.Sprintf("PUSH %d %s: read it, then acknowledge: nova-sprint hook --ack %d", w[0].ID, hookWarnSettings, w[0].ID), "a numbered PUSH line on the hook")
	assert.Contains(t, line, "\n  WARNING: design settings off their working defaults")

	ta.lined(func() { ta.ok("reader add reader-a reader-b") })
	ta.lined(func() { ta.ok("add --stream s1 --count 2") })
	ta.lined(func() { ta.ok("start") })
	ta.lined(func() { ta.ok("tick") })
	ta.lined(func() { ta.ok("tick") })
	ta.lined(func() { ta.ok("take --as m1 --limit 2") })
	ta.lined(func() { ta.ok("finish --as m1 s1-1.w1@1 --report done") })
	ps = c.pushes(ta)
	w = warned(ps, hookWarnReview)
	require.Len(t, w, 1, "s1-1 into review with read cards off is pushed: %+v", ps)
	assert.Contains(t, w[0].Text, "s1-1@1 entered review")
	assert.Contains(t, pushLines(name, w[0]), fmt.Sprintf("PUSH %d %s: read it, then acknowledge", w[0].ID, hookWarnReview), "a numbered PUSH line on the hook")
	assert.Contains(t, w[0].Text, "nova-sprint set --read-cards on")

	ta.lined(func() { ta.ok("finish --as m1 s1-2.w1@1 --report done") })
	ps = c.pushes(ta)
	w = warned(ps, hookWarnReview)
	require.Len(t, w, 1, "s1-2 into review is pushed too, after s1-1's was acknowledged: %+v", ps)
	assert.Contains(t, w[0].Text, "s1-2@1 entered review")
	assert.NotContains(t, w[0].Text, "s1-1", "only the card new to review")
}
