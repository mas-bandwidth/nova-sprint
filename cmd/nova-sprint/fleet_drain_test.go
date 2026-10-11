package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fleet up <m> --width 0 drains the member: no new deal reaches it, its working
// card finishes where it is, and its untaken ready card is levelled away only
// onto a member with a free lane (the level never moves a card onto a member
// with no free lane; round.levelTo, nextLane), where fleet down would deal it
// again (the comfort list of 2026-10-03, item 5: holding a machine meant fleet
// down, which redeals its cards). A width is set again by fleet up --width <n>.
func TestFleetUpWidthZeroDrainsAMember(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1:1,m2:4")
	ta.ok("add --stream s1 --count 6")
	ta.ok("start")
	ta.ok("tick")
	ta.ok("take --as m1 --limit 1")
	var q struct {
		Cards []queueCard
		Width int
	}
	ta.json("queue --as m1", &q)
	require.Len(t, q.Cards, 2, "m1 holds its width working and as many again ready: %+v", q.Cards)
	working := ""
	for _, x := range q.Cards {
		if x.Col == "working" {
			working = x.ID
		}
	}
	require.NotEmpty(t, working, "the take left no working card: %+v", q.Cards)

	out := ta.ok("fleet up m1 --width 0")
	assert.Contains(t, out, "MOVED m1 up width=0 (drains: no new deals, its working cards finish)")
	ta.ok("tick")
	ta.json("queue --as m1", &q)
	// m2 is at its width (held 4, width 4): it has no free lane, so the level
	// moves the untaken ready card nowhere and it stays on m1 (a card moves only
	// onto a member with a free lane; round.levelTo, nextLane).
	assert.Len(t, q.Cards, 2, "the working card and the untaken ready card stay: m2 has no free lane: %+v", q.Cards)
	stillWorking := false
	for _, x := range q.Cards {
		if x.ID == working && x.Col == "working" {
			stillWorking = true
		}
	}
	assert.True(t, stillWorking, "the working card stays on m1 and finishes where it is: %+v", q.Cards)
	assert.Equal(t, 0, q.Width)
	ta.json("queue --as m2", &q)
	assert.Len(t, q.Cards, 4, "no card moved onto m2: it is at its width, so it has no free lane: %+v", q.Cards)

	ta.ok("finish --as m1 " + working + "@1 --head 9f3c2e1 --report 'tests green'")
	ta.ok("tick")
	ta.json("queue --as m1", &q)
	assert.Len(t, q.Cards, 1, "a drained member is dealt nothing new: only its untaken ready card remains: %+v", q.Cards)

	ta.ok("fleet up m1 --width 2")
	ta.ok("tick")
	ta.json("queue --as m1", &q)
	assert.NotEmpty(t, q.Cards, "a width set again takes deals")
}
