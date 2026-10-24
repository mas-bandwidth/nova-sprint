package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The friend redeal never takes an attempt whose worker is alive and moving: 2026-10-24, the
// 2h redeal fired on cards whose workers had posted progress minutes before. An attempt with
// progress inside RuleProgressWindow stays; one whose progress is older is redealt, and the
// head its holder last said it pushed (progress --head) is carried: the next attempt's packet
// starts from it (BaseOf, PushedHead), which friend stage writes into JOB.md as CARRIED.
// tla/FriendRedealProgress.tla. The clock is the world's, moved by hand.
func TestTheFriendRedealSkipsAnAttemptThatIsProgressing(t *testing.T) {
	t.Parallel()
	const pushed = "89abcdef0123456789abcdef0123456789abcdef"
	w, bob := friendDealt(t, "s1-1.w1")
	wc := w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, Working, wc.Col)
	require.True(t, friendLate(w.s, TickReq{}, wc), "past her deadline")

	// progress a minute ago, naming the head pushed so far: alive, not redealt
	w.must(Progress(w.s, ProgressReq{Sel: Sel{IDs: []string{wc.ID}}, As: wc.Row, Head: pushed}))
	w.tick(time.Minute)
	p, _ := TickFriendRedeal(w.s, TickReq{})
	assert.Empty(t, p.Units, "progress inside the window: the worker is alive and moving")

	// a head that is not a full commit is refused, and nothing is stamped
	assert.NotEmpty(t, Progress(w.s, ProgressReq{Sel: Sel{IDs: []string{wc.ID}}, As: wc.Row, Head: "abc"}).Refused)

	// silent past the window: redealt, the pushed head carried
	w.tick(RuleProgressWindow)
	p = w.part(TickFriendRedeal, TickReq{})
	wc = w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, Redealt, wc.Col, "no progress inside the window: redealt")
	assert.Equal(t, pushed, PushedHead(wc), "the redealt attempt's pushed head is the one its progress named")
	var what string
	for _, u := range p.Units {
		for _, n := range u.Notes {
			what += n.What
		}
	}
	assert.True(t, strings.Contains(what, "its pushed head "+pushed+" is carried"), "the redeal says what it carries: %s", what)

	dealStarted(w, bob)
	next := w.s.Fleet.Card("s1-1.w2")
	require.NotNil(t, next, "the next attempt is dealt")
	pk := PacketOf("", 1, next, w.s.Work.Card("s1-1"), []*Card{w.s.Fleet.Card("s1-1.w1")}, nil)
	assert.Equal(t, pushed, pk.BaseHead, "the next attempt starts from the carried head")
	assert.Equal(t, 1, pk.BaseAttempt)
}

// A redealt attempt with no progress head carries nothing: the next attempt starts from BASE,
// and a working attempt's progress head is never a pushed head while it runs.
func TestARedealWithNoProgressHeadCarriesNothing(t *testing.T) {
	t.Parallel()
	w, _ := friendDealt(t, "s1-1.w1")
	wc := w.s.Fleet.Card("s1-1.w1")
	wc.Fields[FieldProgressHead] = "89abcdef0123456789abcdef0123456789abcdef"
	assert.Empty(t, PushedHead(wc), "working: in flight, no pushed head")
	delete(wc.Fields, FieldProgressHead)
	w.part(TickFriendRedeal, TickReq{})
	wc = w.s.Fleet.Card("s1-1.w1")
	require.Equal(t, Redealt, wc.Col)
	assert.Empty(t, PushedHead(wc))
}
