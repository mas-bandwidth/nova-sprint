package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A friend's presence is one last-heard time (FriendPhase; tla/FriendHeard.tla): up under
// FriendHeardUp since the last valid event of hers, deaf for FriendDeafHold more, then down.
// The owner, 2026-10-13: "any activity from the friend that proves their session is live =
// timeout resets ... just like in a network protocol, any valid packet from that client
// resets the timeout accum"; "only show up/down -- if you are deaf, but up, that's fine".
// Every test here passes its time in: no clock, no store.

var heardT0 = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

// The phase by the age of her last event, and the word the tables show for it: deaf shows up.
func TestFriendPhaseByTheAgeOfHerLastEvent(t *testing.T) {
	t.Parallel()
	const grace = FriendHeardUp + FriendDeafHold
	for _, c := range []struct {
		name   string
		age    time.Duration
		phase  string
		status string
	}{
		{"just now", 0, Up, Up},
		{"a second short of the up window", FriendHeardUp - time.Second, Up, Up},
		{"exactly the up window", FriendHeardUp, Deaf, Up},
		{"deaf, a minute in", FriendHeardUp + time.Minute, Deaf, Up},
		{"a second short of the grace", grace - time.Second, Deaf, Up},
		{"exactly the grace", grace, Down, Down},
		{"an hour", time.Hour, Down, Down},
	} {
		f := FriendPresence{Heard: heardT0}
		phase, evidence := FriendPhase(f, heardT0.Add(c.age))
		assert.Equal(t, c.phase, phase, c.name)
		status, shown := FriendEvidence(f, heardT0.Add(c.age))
		assert.Equal(t, c.status, status, c.name)
		assert.Equal(t, evidence, shown, c.name+": the same evidence")
		assert.Equal(t, c.status, FriendStatus(f, heardT0.Add(c.age)), c.name)
		assert.Equal(t, c.status == Up, FriendDownWhy(f, heardT0.Add(c.age)) == "", c.name+": a take is refused only for down")
	}
}

// UP to DEAF on silence; DEAF to UP on any valid event; DEAF to DOWN when the grace runs out; and
// UP to DOWN only through DEAF, which lasts FriendDeafHold and no longer: walked a second at a
// time over an hour (tla/FriendHeard.tla NoUpToDown, DeafBounded).
func TestFriendGoesDownOnlyThroughDeafAndDeafIsBounded(t *testing.T) {
	t.Parallel()
	f := FriendPresence{Heard: heardT0}
	prev, deaf := "", time.Duration(0)
	var seen []string
	for age := time.Duration(0); age <= time.Hour; age += time.Second {
		phase, _ := FriendPhase(f, heardT0.Add(age))
		if phase != prev {
			seen = append(seen, phase)
		}
		require.False(t, prev == Up && phase == Down, "up to down at %s, with no deaf between", age)
		if phase == Deaf {
			deaf += time.Second
		}
		prev = phase
	}
	assert.Equal(t, []string{Up, Deaf, Down}, seen)
	assert.Equal(t, FriendDeafHold, deaf, "deaf lasts the hold and no longer")

	// an event in the deaf window: up again, and the hold starts over from it
	at := heardT0.Add(FriendHeardUp + 3*time.Minute)
	phase, _ := FriendPhase(f, at)
	require.Equal(t, Deaf, phase)
	f.Heard = at
	phase, evidence := FriendPhase(f, at)
	assert.Equal(t, Up, phase, "deaf to up on any valid event")
	assert.Equal(t, "heard 0s ago", evidence)
	phase, _ = FriendPhase(f, at.Add(FriendHeardUp+FriendDeafHold-time.Second))
	assert.Equal(t, Deaf, phase, "the grace runs from her last event, not her first")
	// down, then an event: up at once
	late := heardT0.Add(2 * time.Hour)
	phase, _ = FriendPhase(f, late)
	require.Equal(t, Down, phase)
	f.Heard = late
	phase, _ = FriendPhase(f, late)
	assert.Equal(t, Up, phase, "a down friend comes up on her next valid event")
}

// The row says what deaf is and when she goes down; asleep and down say what is missing, and every
// word the tables get is up, down or held and nothing else.
func TestDeafAndAsleepAreNeverShown(t *testing.T) {
	t.Parallel()
	now := heardT0.Add(time.Hour)
	beat := Beat{At: now.Add(-time.Second)}
	daemon := FriendHealth{State: DaemonPong, Seen: now.Add(-time.Minute), Generation: 2}
	for _, f := range []FriendPresence{
		{Generation: 2},
		{Generation: 2, Heard: now},
		{Generation: 2, Heard: now.Add(-FriendHeardUp - time.Minute)},
		{Generation: 2, Heard: now.Add(-FriendHeardUp - FriendDeafHold - time.Minute)},
		{Generation: 2, Beat: beat},
		{Generation: 2, Beat: beat, Heard: now.Add(-FriendHeardUp - time.Minute)},
		{Generation: 2, Health: daemon},
		{Generation: 2, Held: true, Beat: beat, Heard: now},
		{Generation: 2, Beat: Beat{At: now.Add(-time.Hour)}, Heard: now.Add(-time.Minute)},
		{Generation: 2, Beat: Beat{At: beat.At, Friend: &FriendReport{Until: now.Add(time.Hour), Reason: "limit"}}, Heard: now},
	} {
		word, evidence := FriendEvidence(f, now)
		assert.Contains(t, []string{Up, Down, Held}, word, "%+v", f)
		assert.NotEqual(t, Deaf, word)
		assert.NotEqual(t, Asleep, word)
		assert.Equal(t, word, FriendStatus(f, now))
		phase, _ := FriendPhase(f, now)
		switch phase {
		case Deaf:
			assert.Equal(t, Up, word)
			assert.True(t, strings.HasPrefix(evidence, "deaf: last heard "), evidence)
			assert.Contains(t, evidence, "any valid event brings the friend up at once")
		case Asleep:
			assert.Equal(t, Down, word)
			assert.True(t, strings.HasPrefix(evidence, "asleep: "), evidence)
		}
	}
}

// ASLEEP is a down whose daemon answers and whose session does not: a fresh beat, or a daemon pong
// under the seat now. It is down, with no deal and her cards back; a wake she answers is a valid
// event and brings her straight up. A beat alone is never an event.
func TestAsleepIsADownWhoseDaemonAnswers(t *testing.T) {
	t.Parallel()
	now := heardT0
	fresh := Beat{At: now.Add(-time.Second)}
	phase, evidence := FriendPhase(FriendPresence{Beat: fresh}, now)
	assert.Equal(t, Asleep, phase, "a daemon beating and a session silent")
	assert.Contains(t, evidence, "the friend's daemon answers and the friend's session does not")
	assert.Contains(t, evidence, "no session evidence: nothing heard from the friend within 20m0s")
	assert.Contains(t, evidence, "beat 1s ago is not evidence")
	assert.Equal(t, Down, FriendStatus(FriendPresence{Beat: fresh}, now), "asleep is down")

	pong := FriendHealth{State: DaemonPong, Seen: now.Add(-time.Minute), Generation: 2}
	phase, _ = FriendPhase(FriendPresence{Health: pong, Generation: 2}, now)
	assert.Equal(t, Asleep, phase, "a daemon pong under this seat")
	phase, _ = FriendPhase(FriendPresence{Health: pong, Generation: 3}, now)
	assert.Equal(t, Down, phase, "a daemon pong under an old seat is not an answer")
	old := pong
	old.Seen = now.Add(-FriendHeardUp - FriendDeafHold - time.Second)
	phase, _ = FriendPhase(FriendPresence{Health: old, Generation: 2}, now)
	assert.Equal(t, Down, phase, "a daemon pong from long ago")
	phase, _ = FriendPhase(FriendPresence{}, now)
	assert.Equal(t, Down, phase, "nothing answers: down, not asleep")

	// her beat for hours changes nothing; the wake she answers is up
	for _, d := range []time.Duration{time.Minute, FriendHeardUp, FriendHeardUp + FriendDeafHold, time.Hour} {
		f := FriendPresence{Beat: Beat{At: now.Add(d - time.Second)}}
		phase, _ = FriendPhase(f, now.Add(d))
		assert.Equal(t, Asleep, phase, "beating %s and never heard", d)
	}
	answered := FriendPresence{Beat: fresh, Health: FriendHealth{State: Up, Seen: now, Generation: 2}, Generation: 2}
	phase, evidence = FriendPhase(answered, now)
	assert.Equal(t, Up, phase, "the wake she answers goes straight to up")
	assert.Equal(t, "session pong 0s ago", evidence)
	f := FriendPresence{Beat: fresh, Heard: now}
	phase, _ = FriendPhase(f, now)
	assert.Equal(t, Up, phase, "any valid event is the answer")
}

// A hold and a beat that says down win over her last-heard time, as they did over her evidence.
func TestAHoldAndABeatThatSaysDownWinOverHerLastHeard(t *testing.T) {
	t.Parallel()
	now := heardT0
	f := FriendPresence{Heard: now, Generation: 2}
	require.Equal(t, Up, FriendStatus(f, now))
	f.Held = true
	phase, evidence := FriendPhase(f, now)
	assert.Equal(t, Held, phase)
	assert.Equal(t, "held", evidence)
	f.Held = false
	f.Beat = Beat{At: now.Add(-time.Second), Friend: &FriendReport{Until: now.Add(time.Hour), Reason: "usage limit"}}
	phase, evidence = FriendPhase(f, now)
	assert.Equal(t, Down, phase, "her harness at its limit is down whatever she said last")
	assert.Contains(t, evidence, "beat says down until")
	assert.Contains(t, evidence, "usage limit")
}

// The beat's staleness is the old daemon's: it drops the proof or finish a stopped daemon carried, as
// before, and it never drops a valid event of her own (a friend who works by hand after her old daemon
// stopped is up), her answer to the seat's wake ping included: the pong is the seat's observation of
// her session, under the seat's generation, not her daemon's word (the cold read of PR 267).
func TestAStaleBeatDoesNotOverrideAFreshHeard(t *testing.T) {
	t.Parallel()
	now := heardT0
	stale := Beat{At: now.Add(-FriendBeatBound - time.Minute)}
	pong := FriendHealth{State: Up, Seen: now.Add(-time.Minute), Generation: 2}

	phase, evidence := FriendPhase(FriendPresence{Beat: stale, Health: pong, Generation: 2}, now)
	assert.Equal(t, Up, phase, "her answer to the seat's wake ping is hers, whatever her old daemon did")
	assert.Equal(t, "session pong 1m0s ago", evidence)
	phase, evidence = FriendPhase(FriendPresence{Beat: stale, Health: pong, Generation: 3}, now)
	assert.Equal(t, Down, phase, "a pong under an old seat is none")
	assert.Contains(t, evidence, "daemon stopped beating")
	assert.Contains(t, evidence, "down since")
	old := pong
	old.Seen = now.Add(-FriendHeardUp - FriendDeafHold - time.Minute)
	phase, evidence = FriendPhase(FriendPresence{Beat: stale, Health: old, Generation: 2}, now)
	assert.Equal(t, Down, phase, "a pong past the grace")
	assert.Contains(t, evidence, "daemon stopped beating")
	phase, _ = FriendPhase(FriendPresence{Beat: stale, Finished: now.Add(-time.Minute)}, now)
	assert.Equal(t, Down, phase, "nor is a finish")

	phase, evidence = FriendPhase(FriendPresence{Beat: stale, Heard: now.Add(-time.Minute)}, now)
	assert.Equal(t, Up, phase, "a friend heard a minute ago is up whatever her old daemon did")
	assert.Equal(t, "heard 1m0s ago", evidence)
	phase, _ = FriendPhase(FriendPresence{Beat: stale, Heard: now.Add(-FriendHeardUp - time.Minute)}, now)
	assert.Equal(t, Deaf, phase)
	phase, evidence = FriendPhase(FriendPresence{Beat: stale, Heard: now.Add(-time.Hour)}, now)
	assert.Equal(t, Down, phase)
	assert.Contains(t, evidence, "daemon stopped beating")
}

// The sources of one last-heard time, the latest of them, worded by the event; a time after now is none.
func TestHerLastHeardIsTheLatestOfHerEvents(t *testing.T) {
	t.Parallel()
	now := heardT0
	beat := Beat{At: now.Add(-time.Second), Proof: now.Add(-3 * time.Minute)}
	pong := FriendHealth{State: Up, Seen: now.Add(-2 * time.Minute), Generation: 2}
	for _, c := range []struct {
		name     string
		f        FriendPresence
		evidence string
	}{
		{"heard alone", FriendPresence{Heard: now.Add(-time.Minute)}, "heard 1m0s ago"},
		{"a pong", FriendPresence{Health: pong, Generation: 2}, "session pong 2m0s ago"},
		{"a proof", FriendPresence{Beat: beat}, "session proof 3m0s ago"},
		{"a finish", FriendPresence{Finished: now.Add(-4 * time.Minute)}, "finish 4m0s ago"},
		{"the later of heard and a pong", FriendPresence{Heard: now.Add(-time.Minute), Health: pong, Generation: 2}, "heard 1m0s ago"},
		{"the later of a pong and heard", FriendPresence{Heard: now.Add(-5 * time.Minute), Health: pong, Generation: 2}, "session pong 2m0s ago"},
		{"a tie is worded by the event", FriendPresence{Heard: pong.Seen, Health: pong, Generation: 2}, "session pong 2m0s ago"},
		{"a proof the beat has stopped under", FriendPresence{Beat: Beat{At: now.Add(-time.Minute), Proof: now.Add(-3 * time.Minute)}, Heard: now.Add(-9 * time.Minute)}, "heard 9m0s ago"},
	} {
		word, evidence := FriendEvidence(c.f, now)
		assert.Equal(t, Up, word, c.name)
		assert.Equal(t, c.evidence, evidence, c.name)
	}
	for _, c := range []struct {
		name string
		f    FriendPresence
	}{
		{"heard in the future", FriendPresence{Heard: now.Add(time.Second)}},
		{"a pong under an old seat", FriendPresence{Health: pong, Generation: 3}},
		{"a pong the daemon answered", FriendPresence{Health: FriendHealth{State: DaemonPong, Seen: now, Generation: 2}, Generation: 2}},
	} {
		phase, _ := FriendPhase(c.f, now)
		assert.NotEqual(t, Up, phase, c.name)
		assert.NotEqual(t, Deaf, phase, c.name)
	}
}

// A take reads her presence: heard a second ago and never beaten, she takes (the deadlock: with no
// daemon, nothing she did was evidence, and her take was refused for want of evidence); deaf, she
// takes and keeps her cards; past the grace she is refused, the refusal naming the silence and how
// she comes up.
func TestATakeByAFriendReadsHerLastHeard(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)

	t.Run("heard a second ago, no beat, no pong, no finish: admitted", func(t *testing.T) {
		t.Parallel()
		w := readyForAmy(t)
		f := FriendPresence{Heard: now.Add(-time.Second)}
		require.Equal(t, Up, FriendStatus(f, now))
		w.must(takeForAmy(w, FriendSeat{Name: "amy", Width: 3, Status: FriendStatus(f, now), Class: "flash,pro"}))
		assert.Equal(t, Working, w.s.Fleet.Card("s1-3.w1").Col)
	})

	t.Run("deaf: admitted, and nothing in her row is returned", func(t *testing.T) {
		t.Parallel()
		w := readyForAmy(t)
		f := FriendPresence{Heard: now.Add(-FriendHeardUp - time.Minute)}
		phase, _ := FriendPhase(f, now)
		require.Equal(t, Deaf, phase)
		require.Equal(t, Up, FriendStatus(f, now), "deaf shows and behaves as up")
		w.must(takeForAmy(w, FriendSeat{Name: "amy", Width: 3, Status: FriendStatus(f, now), Class: "flash,pro"}))
		assert.Equal(t, Working, w.s.Fleet.Card("s1-3.w1").Col)
		// her daemon stopped long ago: the tick's stopped-beat return leaves a friend shown up alone
		stale := Beat{At: now.Add(-FriendBeatBound - time.Minute)}
		w.s.Friends = []FriendSeat{{Name: "amy", Width: 3, Status: Up, Phase: Deaf, Heard: f.Heard}}
		got := LiveReturns(w.s, map[string]Beat{"amy": stale}, false)
		assert.Empty(t, got.Units, "cards come back only from down: %+v", got.Units)
		assert.True(t, w.s.Fleet.Card("s1-1.w1").Placed())
	})

	t.Run("past the grace: refused, naming the silence and how she comes up", func(t *testing.T) {
		t.Parallel()
		w := readyForAmy(t)
		f := FriendPresence{Heard: now.Add(-FriendHeardUp - FriendDeafHold - time.Minute)}
		st := FriendStatus(f, now)
		require.Equal(t, Down, st)
		p := takeForAmy(w, FriendSeat{Name: "amy", Width: 3, Status: st, Why: FriendDownWhy(f, now)})
		require.Len(t, p.Refused, 1)
		why := p.Refused[0].Why
		assert.Contains(t, why, "friend amy is down: no session evidence: nothing heard from the friend within 20m0s")
		assert.Contains(t, why, "last heard 21m0s ago")
		assert.Contains(t, why, "any valid event brings the friend up at once: nova-bus send or recv as the friend, nova-sprint take or finish as the friend")
		assert.Equal(t, Ready, w.s.Fleet.Card("s1-3.w1").Col)
	})

	t.Run("down, her daemon stopped, and her cards come back", func(t *testing.T) {
		t.Parallel()
		w := readyForAmy(t)
		stale := Beat{At: now.Add(-FriendBeatBound - time.Minute)}
		w.s.Friends = []FriendSeat{{Name: "amy", Width: 3, Status: Down, Phase: Down}}
		got := LiveReturns(w.s, map[string]Beat{"amy": stale}, false)
		assert.NotEmpty(t, got.Units, "her cards come back from down")
	})
}
