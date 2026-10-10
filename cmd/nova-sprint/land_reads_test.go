package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The lander lands no card short of its reads (tla/Land.tla NoLandWithoutReads): a card
// accepted on one read, the setting raised to two, is refused by name with its count and
// nothing reaches origin; the next tick sends it back to review for the read it lacks. On
// the Studio sprint of 2026-10-10 two cards landed on one read of two.
func TestLandRefusesACardShortOfItsReads(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.ok("add --stream s1 --count 1 --one")
	head := r.head("s1-1", "main", "s1-1.txt", "one\n")
	r.queued(map[string]string{"s1-1": head}, "s1-1")
	r.ok("set --reads 2")
	code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
	assert.Equal(t, 1, code, "a refusal is reported")
	assert.Contains(t, out+errs, "s1-1 has ok reads at head "+head+" from 1 of the 2 readers it needs")
	assert.Equal(t, []string{"base"}, r.mainLog(), "nothing pushed")
	assert.Equal(t, map[string]string{"s1-1": "merging/queued"}, r.places("s1-1"))
	// a running machine's pump (a stopped one ticks nothing)
	tick := r.ok("start") + r.ok("tick")
	assert.Equal(t, map[string]string{"s1-1": "review/returned"}, r.places("s1-1"), "back to review for its second read: %s", tick)
}

// Through the store's own tick: a merging card with the reads it needs is never sent back,
// and the lander lands it.
func TestATickLeavesAMergingCardWithItsReadsAndLandLandsIt(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.ok("add --stream s1 --count 1 --one")
	head := r.head("s1-1", "main", "s1-1.txt", "one\n")
	r.queued(map[string]string{"s1-1": head}, "s1-1")
	tick := r.ok("start") + r.ok("tick")
	assert.Equal(t, map[string]string{"s1-1": "merging/queued"}, r.places("s1-1"), "its reads stand: %s", tick)
	out := r.ok("land --repo-dir " + r.clone + " --base main")
	assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=main")
	// on a running machine the landing is recorded in merge, and the tick moves the work card
	r.ok("tick")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged"}, r.places("s1-1"))
}

// Between the lander's check and its report the card is marked landing (sprint.MarkLanding,
// before the push): a setting raised there and a tick do not send it back to review, the
// push and the report complete the landing (tla/Land.tla PushedNeverSentBack). Before the
// mark, a tick in that window took a pushed card back to review.
func TestATickUnderThePushLeavesAMarkedLanding(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.ok("add --stream s1 --count 1 --one")
	head := r.head("s1-1", "main", "s1-1.txt", "one\n")
	r.queued(map[string]string{"s1-1": head}, "s1-1")
	r.ok("start")
	var under string
	r.a.beforePush = func(int) {
		r.ok("set --reads 2")
		r.ok("tick")
		under = r.places("s1-1")["s1-1"]
	}
	out := r.ok("land --repo-dir " + r.clone + " --base main")
	assert.Equal(t, "merging/queued", under, "marked landing: never back to review under the push")
	assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=main")
	r.ok("tick")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged"}, r.places("s1-1"))
}
