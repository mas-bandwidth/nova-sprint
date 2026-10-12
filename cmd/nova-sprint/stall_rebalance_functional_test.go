//go:build functional

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- a burst of reads piled on members while another sat idle (the-rebalance-levels-reads) ---

// TestStallQueuedReadsMoveToAMemberThatComesUpOnTheStore is the v1.2.11 restart on a real
// store: the rebalance skipped every card that was not work, so a member idle when a burst of
// reads was dealt sat with none while the reads queued behind full lanes elsewhere, and fleet
// level moved 0. Two members of width 1 each hold a working card and a queued read of the
// other's finished card; a third member comes up idle; within two ticks it holds a read
// moved to it, taken off a full member.
func TestStallQueuedReadsMoveToAMemberThatComesUpOnTheStore(t *testing.T) {
	t.Parallel()
	r := newStallRig(t)
	r.do("init", "--readers", "reader-m1,reader-m2,reader-m3", "--members", "m1:1,m2:1,m3:1")
	r.do("set", "--read-cards", "on", "--reads", "1")
	r.do("reader", "set", "reader-m1", "reader-m2", "reader-m3", "--tiers", "all")
	r.do("add", "--stream", "s1", "--count", "4")
	r.beat("m1", "m2") // m3 is down: it never beat
	r.do("start")
	r.tick() // presence
	r.tick() // the deal: s1-1 and s1-2, one a member
	for _, m := range []string{"m1", "m2"} {
		ps := r.take(m)
		require.Len(t, ps, 1, "%s takes one: %+v", m, ps)
		r.finish(m, ps[0], stallHead(ps[0].Primary))
	}
	// the finishes drain to review, s1-3 and s1-4 are dealt to the free lanes and the reads
	// of s1-1 and s1-2 are asked crosswise; each member takes its work card alone (by id),
	// so its lane is full and the read waits queued behind it
	dealtWork := func(id string) (string, string) {
		v := r.card(id)
		if len(v.Work) == 0 {
			return "", ""
		}
		w := v.Work[len(v.Work)-1]
		return w.Row, w.ID + "@" + w.Fields["gen"]
	}
	for i := 0; i < 4; i++ {
		r.beat("m1", "m2")
		r.tick()
		if row3, _ := dealtWork("s1-3"); row3 != "" {
			if row4, _ := dealtWork("s1-4"); row4 != "" {
				break
			}
		}
	}
	for _, id := range []string{"s1-3", "s1-4"} {
		row, card := dealtWork(id)
		require.NotEmpty(t, row, "%s is dealt", id)
		r.cliOK(row, "take", "--as", row, card, "--epoch", "0")
	}
	readsOn := func(row string) int {
		n := 0
		for _, id := range []string{"s1-1", "s1-2", "s1-3", "s1-4"} {
			for _, rc := range r.card(id).Reads {
				if rc.Row == row {
					n++
				}
			}
		}
		return n
	}
	queuedOn := func(row string) int {
		n := 0
		for _, id := range []string{"s1-1", "s1-2", "s1-3", "s1-4"} {
			for _, rc := range r.card(id).Reads {
				if rc.Row == row && rc.Col == "ready" {
					n++
				}
			}
		}
		return n
	}
	for i := 0; i < 2 && queuedOn("m1")+queuedOn("m2") < 2; i++ {
		r.beat("m1", "m2")
		r.tick()
	}
	require.Equal(t, 2, queuedOn("m1")+queuedOn("m2"), "both reads are queued behind full lanes")

	r.beat("m1", "m2", "m3") // m3 comes up idle
	for i := 0; i < 2 && readsOn("m3") == 0; i++ {
		r.tick()
		r.beat("m1", "m2", "m3")
	}
	assert.GreaterOrEqual(t, readsOn("m3"), 1, "a queued read moves to the idle member")
	assert.Less(t, queuedOn("m1")+queuedOn("m2"), 2, "off a full member")
}
