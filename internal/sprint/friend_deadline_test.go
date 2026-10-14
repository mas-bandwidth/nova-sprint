package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A friend's card's working deadline is the larger of DeadlineUnfinished and
// FriendDeadlineK times her median run wall over her last ok attempts (her take to her
// report), set on the card as she takes it, as nova-tools#5300 gives a machine's card its
// member's when it is dealt.
func TestAFriendsCardsDeadlineIsThreeTimesHerMedianWall(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend amy"), friendBrief("friend amy"), friendBrief("friend amy"), friendBrief("friend amy"))
	dealStarted(w, FriendSeat{Name: "amy", Width: 3, Status: Up, Class: "flash,pro"})
	amy := FriendRow("amy")
	_, n := FriendMedianWall(w.s, "amy")
	require.Zero(t, n)
	assert.Empty(t, w.s.Fleet.Card("s1-1.w1").F(FieldFriendDeadline), "no ok attempt yet: the fleet's")
	assert.Equal(t, DeadlineUnfinished, unfinishedLimit(w.s.Fleet.Card("s1-1.w1")))

	// she reports s1-1 after 60 minutes, s1-2 after 70, s1-3 after 80, each collected ten
	// minutes later: her median run wall is 70 minutes, never the collection's
	t0 := w.s.Now
	for i, at := range []time.Duration{60 * time.Minute, 70 * time.Minute, 80 * time.Minute} {
		id := WorkCardID("s1-"+itoa(i+1), 1)
		reported := t0.Add(at)
		w.s.Now = reported.Add(10 * time.Minute)
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{id}}, As: amy, Gens: gensOf(w.s, id), Head: "abc", Reported: reported}))
	}
	median, n := FriendMedianWall(w.s, "amy")
	assert.Equal(t, 3, n)
	assert.Equal(t, 4200.0, median)

	// s1-4, ready behind them, was taken by her first finish (at 70 minutes, with no ok
	// attempt before it: the fleet's); taken back and dealt to her again, it takes hers
	wc := w.s.Fleet.Card("s1-4.w1")
	require.Equal(t, Working, wc.Col)
	assert.Equal(t, DeadlineUnfinished, unfinishedLimit(wc))
	w.must(FriendTake(w.s, FriendTakeReq{Friend: "amy", IDs: []string{"s1-4"}, Hold: true}))
	dealStarted(w, FriendSeat{Name: "amy", Width: 3, Status: Up, Class: "flash,pro"})
	wc = w.s.Fleet.Card("s1-4.w1")
	require.Equal(t, Working, wc.Col)
	assert.Equal(t, "12600", wc.F(FieldFriendDeadline), "three times 70 minutes")
	t1 := w.s.Now
	w.s.Now = t1.Add(2*time.Hour + time.Minute)
	p, _ := TickFriendRedeal(w.s, TickReq{})
	assert.Empty(t, p.Units, "under its deadline it is not redealt")
	w.s.Now = t1.Add(3*time.Hour + 31*time.Minute)
	w.part(TickFriendRedealFn, TickReq{})
	assert.Equal(t, Redealt, w.s.Fleet.Card("s1-4.w1").Col, "past three and a half it is redealt")
}

func TestReadTierRule(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	finished(w, "s1-1", false)
	w.must(Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	note := Note{Kind: Judgment, Type: NRaiseReadTier, Stream: "s1", Tier: "pro", What: "two broken reads"}

	// rule turned off or missing stream control card returns ok=false
	w.s.RulesOff = []string{RuleReadTier}
	u, q, ok := ReadTierRule(w.s, note)
	assert.False(t, ok)
	assert.Nil(t, u)
	assert.Empty(t, q.Why)
	w.s.RulesOff = nil

	_, _, ok = ReadTierRule(w.s, Note{Stream: "no-stream", Tier: "pro"})
	assert.False(t, ok)

	// no disagreement yet -> kept at its tier (raise == nil, ok == true)
	u, q, ok = ReadTierRule(w.s, note)
	require.True(t, ok)
	assert.Nil(t, u)
	assert.Contains(t, q.Why, "kept at its tier")

	// one reader says ok, another says broken with a finding -> raises stream read tier and closes open NRaiseReadTier
	reads := readsAt(w.s, w.s.Work.Card("s1-1"), 1)
	require.Len(t, reads, 2)
	w.must(Read(w.s, ReadReq{Usage: "input=100 output=10", As: reads[0].Row, Verdict: "ok", Sel: Sel{IDs: []string{reads[0].ID}}}))
	w.must(Read(w.s, ReadReq{Usage: "input=100 output=10", As: reads[1].Row, Verdict: "broken", Finding: "real bug in a.go:10", Sel: Sel{IDs: []string{reads[1].ID}}}))

	openRaise := judgment(NRaiseReadTier, "s1", w.s.Now, 0)
	openRaise.StreamLevel = true
	w.note(openRaise)

	u, q, ok = ReadTierRule(w.s, note)
	require.True(t, ok)
	require.NotNil(t, u)
	assert.Contains(t, q.Why, "two substantive findings disagree on s1-1 attempt 1")
	assert.Len(t, u.Closes, 1)
	w.must(Plan{Units: []Unit{*u}})
	assert.Equal(t, "pro", w.s.StreamCtl("s1").F(FieldReadTier))
}
