//go:build functional

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestNeedsSetKeepsAHoldOnTheRealStore is the port-fixed-* case of 17:31Z on a real
// store (the test's own redis): a card added held needs a spike that has landed and a
// chain that has not; a needs set to the spike alone, every new need landed, used to
// move it waiting -> ready with its hold stamp still on it, and the deal could hand it
// out. A needs change never releases a hold: the card stays waiting and held through
// two ticks with a member up, is dealt nothing, and the coordinator's release moves it
// to ready, where the next tick deals it (tla/Holds.tla NeedsSetH and DealH).
func TestNeedsSetKeepsAHoldOnTheRealStore(t *testing.T) {
	t.Parallel()
	r := newUJRig(t)
	r.do("init --readers reader-a --members m1 --owner glenn")
	// the cards are admitted on a stopped machine, so each is on the table at once
	r.do("add --stream s1 --count 1 --one")                          // s1-1, the spike
	r.do("add --stream s1 --count 1 --one --needs s1-1")             // s1-2, the chain
	r.do("add --stream s2 --count 1 --one --needs s1-1,s1-2 --held") // s2-1, the held card
	mnMove(t, r, "s1-1", "landed")                                   // the spike has landed
	require.Equal(t, "waiting", r.col("s2-1"))

	before, err := r.store().Load(r.ctx, []string{sprint.Work}, nil)
	require.NoError(t, err)
	require.True(t, sprint.IsHeld(before.Work.Placed("s2-1")), "the card is admitted held")
	r.do("needs set s2-1 --needs s1-1") // every new need has landed
	r.beat()
	r.do("fleet up m1 --width 4")
	r.beat()
	r.do("start")
	for range 2 {
		_, err := r.store().Tick(r.ctx)
		require.NoError(t, err)
		r.beat()
	}
	view := r.do("card s2-1")
	assert.Equal(t, "waiting", r.col("s2-1"), "a held card stays waiting through a needs change:\n%s", view)
	snap, err := r.store().Load(r.ctx, []string{sprint.Work, sprint.Fleet}, nil)
	require.NoError(t, err)
	pr := snap.Work.Placed("s2-1")
	require.NotNil(t, pr)
	assert.True(t, sprint.IsHeld(pr), "the hold field is still on it")
	assert.Equal(t, "s1-1", pr.F("needs"), "its needs are as asked")
	for _, c := range snap.Fleet.Column("ready") {
		assert.NotEqual(t, "s2-1", c.F("primary"), "no work card was cut for the held card")
	}

	r.do("release s2-1 --reason test")
	for range 2 { // the release is drained, then the deal hands the card out
		_, err = r.store().Tick(r.ctx)
		require.NoError(t, err)
		r.beat()
	}
	assert.Equal(t, "working", r.col("s2-1"), "released, it is dealt")
	snap, err = r.store().Load(r.ctx, []string{sprint.Work}, nil)
	require.NoError(t, err)
	assert.False(t, sprint.IsHeld(snap.Work.Placed("s2-1")), "the release cleared the hold")
}
