package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The friend stall ladder (docs/SPEC-SPRINT.md section friend-stall-ladder-r.w1;
// the model is tla/StallLadder.tla).
//
// Invariants verified:
//   NoCardHeldPastBound: no unstarted card is held past stall_after + 4*step (taken back at rung 4).
//   NoStartedRedealt: started cards stay with friend and finish; only unstarted cards are taken back.
//   ReleasedOnlyByActivity (ReleasedOnlyByEvidence and NeverDownWhileWorking): a friend marked down
//     for stall is released to up by any evidence of work after it (session activity, a beat naming
//     a running job, a finish or a collected report, a read she recorded, a progress stamp), and by
//     nothing else.

func TestFriendStallLadderClimbsAndTakesBackUnstarted(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend"), friendBrief("friend"))
	seats := []FriendSeat{{Name: "amy", Width: 2, Status: Up}}
	dealWith(w, seats...)

	wc1 := w.s.Fleet.Card("s1-1.w1")
	wc2 := w.s.Fleet.Card("s1-2.w1")
	require.NotNil(t, wc1)
	require.NotNil(t, wc2)
	require.Equal(t, Working, wc1.Col)
	require.Equal(t, Working, wc2.Col)

	// Friend amy starts s1-1 (progress stamped)
	wc1.Fields[FieldProgress] = stamp(w.s.Now)
	// s1-2 is left unstarted

	t0 := w.s.Now
	var woken []int
	wakeFn := func(friend string, rung int, d time.Duration) error {
		if friend == "amy" {
			woken = append(woken, rung)
		}
		return nil
	}

	// At t0: not stalled (< 20m)
	w.part(TickFriendStall, TickReq{WakeFriend: wakeFn, Friends: seats})
	rung, _ := w.s.Fleet.Prop(PropFriendStallRung("amy"))
	assert.Empty(t, rung, "rung 0 has no property")
	assert.Empty(t, woken)

	// Rung 1: at t0 + 21m (between 20m and 25m)
	w.s.Now = t0.Add(21 * time.Minute)
	w.part(TickFriendStall, TickReq{WakeFriend: wakeFn, Friends: seats})
	rung, _ = w.s.Fleet.Prop(PropFriendStallRung("amy"))
	assert.Equal(t, "1", rung)
	assert.Equal(t, []int{1}, woken)
	assert.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col)
	assert.Equal(t, Working, w.s.Fleet.Card("s1-2.w1").Col)

	// Rung 2: at t0 + 26m (between 25m and 30m)
	w.s.Now = t0.Add(26 * time.Minute)
	w.part(TickFriendStall, TickReq{WakeFriend: wakeFn, Friends: seats})
	rung, _ = w.s.Fleet.Prop(PropFriendStallRung("amy"))
	assert.Equal(t, "2", rung)
	assert.Equal(t, []int{1, 2}, woken)
	assert.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col)
	assert.Equal(t, Working, w.s.Fleet.Card("s1-2.w1").Col)

	// Rung 3: at t0 + 31m (between 30m and 35m)
	w.s.Now = t0.Add(31 * time.Minute)
	p3 := w.part(TickFriendStall, TickReq{WakeFriend: wakeFn, Friends: seats})
	rung, _ = w.s.Fleet.Prop(PropFriendStallRung("amy"))
	assert.Equal(t, "3", rung)
	var judged bool
	for _, n := range p3.Notes {
		if n.Kind == Judgment && n.Type == NStalled {
			judged = true
			assert.Contains(t, n.What, "friend amy stalled")
			assert.Contains(t, n.What, "two wakes unanswered")
		}
	}
	assert.True(t, judged, "judgment note for stall emitted at rung 3")

	// Rung 4: at t0 + 36m (between 35m and 40m)
	// Unstarted card s1-2.w1 should be taken back; started card s1-1.w1 stays!
	w.s.Now = t0.Add(36 * time.Minute)
	w.part(TickFriendStall, TickReq{WakeFriend: wakeFn, Friends: seats})
	rung, _ = w.s.Fleet.Prop(PropFriendStallRung("amy"))
	assert.Equal(t, "4", rung)
	assert.Equal(t, Withdrawn, w.s.Fleet.Card("s1-2.w1").Col, "unstarted card taken back")
	assert.Equal(t, Ready, w.s.Work.Card("s1-2").Col, "primary back to ready")
	assert.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "started card stays with friend")

	// Rung 5: at t0 + 41m (>= 40m)
	// Friend marked down
	w.s.Now = t0.Add(41 * time.Minute)
	p5 := w.part(TickFriendStall, TickReq{WakeFriend: wakeFn, Friends: seats})
	rung, _ = w.s.Fleet.Prop(PropFriendStallRung("amy"))
	assert.Equal(t, "5", rung)
	downStamp, hasDown := w.s.Fleet.Prop(PropFriendStallDown("amy"))
	assert.True(t, hasDown, "stall down property recorded")
	assert.NotEmpty(t, downStamp)
	require.NotNil(t, p5.Health)
	assert.Equal(t, "amy", p5.Health.Friend)
	assert.Equal(t, Down, p5.Health.Health.State)
	assert.Equal(t, "stalled", p5.Health.Health.Reason)
	assert.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "started card stays even when marked down")

	// Friend session activity arrives -> released to up and ladder resets to 0!
	w.s.Now = t0.Add(42 * time.Minute)
	beats := map[string]Beat{
		"amy": {
			Friend: &FriendReport{
				Active: w.s.Now,
			},
		},
	}
	pRel := w.part(TickFriendStall, TickReq{Beats: beats, Friends: seats})
	rung, _ = w.s.Fleet.Prop(PropFriendStallRung("amy"))
	assert.Empty(t, rung, "rung reset to 0")
	downStamp, _ = w.s.Fleet.Prop(PropFriendStallDown("amy"))
	assert.Empty(t, downStamp, "stall down cleared")
	require.NotNil(t, pRel.Health)
	assert.Equal(t, "amy", pRel.Health.Friend)
	assert.Equal(t, Up, pRel.Health.Health.State)
}

func TestFriendStallLadderResetsOnProgress(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend"))
	seats := []FriendSeat{{Name: "amy", Width: 1, Status: Up}}
	dealWith(w, seats...)

	t0 := w.s.Now
	// Advance to rung 2 (26m)
	w.s.Now = t0.Add(26 * time.Minute)
	w.part(TickFriendStall, TickReq{Friends: seats})
	rung, _ := w.s.Fleet.Prop(PropFriendStallRung("amy"))
	require.Equal(t, "2", rung)

	// Now friend stamps progress on the card
	w.s.Fleet.Card("s1-1.w1").Fields[FieldProgress] = stamp(w.s.Now)

	// Next tick sees the fresh progress: stall resets to rung 0!
	w.part(TickFriendStall, TickReq{Friends: seats})
	rung, _ = w.s.Fleet.Prop(PropFriendStallRung("amy"))
	assert.Empty(t, rung, "progress resets rung to 0")
}

func TestFriendStallLadderProgressReleasesDownFriend(t *testing.T) {
	t.Parallel()
	w := friendWorld(t, friendBrief("friend"))
	seats := []FriendSeat{{Name: "amy", Width: 1, Status: Up}}
	dealWith(w, seats...)

	// Started card stays through stall ladder
	w.s.Fleet.Card("s1-1.w1").Fields[FieldProgress] = stamp(w.s.Now)

	t0 := w.s.Now
	// Advance to rung 5 (41m)
	w.s.Now = t0.Add(41 * time.Minute)
	p5 := w.part(TickFriendStall, TickReq{Friends: seats})
	require.NotNil(t, p5.Health)
	assert.Equal(t, Down, p5.Health.Health.State)

	// A progress stamp after the stall-down is evidence of work: it releases her to up
	// (ReleasedOnlyByActivity, released by any evidence)
	w.s.Now = w.s.Now.Add(time.Minute)
	w.s.Fleet.Card("s1-1.w1").Fields[FieldProgress] = stamp(w.s.Now)
	pProg := w.part(TickFriendStall, TickReq{Friends: seats})
	require.NotNil(t, pProg.Health, "a progress stamp releases a down friend to up")
	assert.Equal(t, Up, pProg.Health.Health.State)
	downStamp, _ := w.s.Fleet.Prop(PropFriendStallDown("amy"))
	assert.Empty(t, downStamp, "stall down cleared on progress")
}

// The stall ladder counts every evidence of work the store has, not only session writes
// (docs/SPEC-SPRINT.md section friend-stall-ladder-r.w1, evidence of work; 2026-10-05, the
// sprint marked three working friends down as stalled within the hour: a one-shot lane
// runner whose beat names running jobs and sends no session activity, and the coordinator's
// own row, whose cards child agents run). A beat naming a running job, a finish or a report
// collected for her, a read she recorded and a progress stamp each reset her to rung 0 and
// release a stall-down; a friend with dealt cards and none of them still climbs
// (tla/StallLadder.tla, NeverDownWhileWorking and ReleasedOnlyByEvidence).
func TestAFriendRunningCardsInLanesIsNeverStalled(t *testing.T) {
	t.Parallel()

	rungOf := func(w *world, f string) string {
		rung, _ := w.s.Fleet.Prop(PropFriendStallRung(f))
		return rung
	}
	downOf := func(w *world, f string) string {
		down, _ := w.s.Fleet.Prop(PropFriendStallDown(f))
		return down
	}
	// stalledDown is amy at rung 5, marked down for stall at t0+41m: s1-1.w1 started
	// (a progress stamp at t0) and kept, s1-2.w1 taken back at rung 4.
	stalledDown := func(t *testing.T) (*world, []FriendSeat, time.Time) {
		w := friendWorld(t, friendBrief("friend"), friendBrief("friend"))
		seats := []FriendSeat{{Name: "amy", Width: 2, Status: Up}}
		dealWith(w, seats...)
		t0 := w.s.Now
		w.s.Fleet.Card("s1-1.w1").Fields[FieldProgress] = stamp(t0)
		for _, m := range []int{21, 26, 31, 36, 41} {
			w.s.Now = t0.Add(time.Duration(m) * time.Minute)
			w.part(TickFriendStall, TickReq{Friends: seats})
		}
		require.Equal(t, "5", rungOf(w, "amy"))
		require.NotEmpty(t, downOf(w, "amy"), "marked down for stall")
		require.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "the started card stays")
		return w, seats, t0
	}
	released := func(t *testing.T, w *world, p Plan, by string) {
		t.Helper()
		require.NotNil(t, p.Health, "released by %s", by)
		assert.Equal(t, Up, p.Health.Health.State)
		assert.Empty(t, downOf(w, "amy"), "the stall-down is cleared by %s", by)
		assert.Empty(t, rungOf(w, "amy"), "rung 0 after %s", by)
		var said bool
		for _, n := range p.Notes {
			if n.Type == NFriendStall && strings.Contains(n.What, "released to up") && strings.Contains(n.What, by) {
				said = true
			}
		}
		assert.True(t, said, "the release note names %s", by)
	}

	t.Run("a beat naming running jobs, with no session activity, is never stalled", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("friend"), friendBrief("friend"))
		seats := []FriendSeat{{Name: "freddy", Width: 2, Status: Up}}
		dealWith(w, seats...)
		t0 := w.s.Now
		var woken []int
		wake := func(f string, rung int, _ time.Duration) error { woken = append(woken, rung); return nil }
		// an hour of beats every 5 minutes, each naming a one-shot lane job running and
		// no session write; no progress stamp, no finish
		for m := 5; m <= 60; m += 5 {
			w.s.Now = t0.Add(time.Duration(m) * time.Minute)
			beats := map[string]Beat{"freddy": {At: w.s.Now, Friend: &FriendReport{Running: []string{"lane-3/job-" + itoa(m)}}}}
			p := w.part(TickFriendStall, TickReq{Beats: beats, Friends: seats, WakeFriend: wake})
			assert.Nil(t, p.Health, "minute %d: never marked down", m)
			assert.Empty(t, rungOf(w, "freddy"), "minute %d: rung 0", m)
		}
		assert.Empty(t, woken, "never woken")
		assert.Equal(t, Working, w.s.Fleet.Card("s1-1.w1").Col, "nothing taken back")
		assert.Equal(t, Working, w.s.Fleet.Card("s1-2.w1").Col, "nothing taken back")
		assert.Empty(t, downOf(w, "freddy"))
	})

	t.Run("a beat whose running list is empty is no evidence", func(t *testing.T) {
		t.Parallel()
		w := friendWorld(t, friendBrief("friend"))
		seats := []FriendSeat{{Name: "amy", Width: 1, Status: Up}}
		dealWith(w, seats...)
		w.s.Now = w.s.Now.Add(21 * time.Minute)
		zero := 0
		beats := map[string]Beat{"amy": {At: w.s.Now, Friend: &FriendReport{Working: &zero}}}
		w.part(TickFriendStall, TickReq{Beats: beats, Friends: seats})
		assert.Equal(t, "1", rungOf(w, "amy"), "awake is not working")
	})

	t.Run("a friend with dealt cards and no evidence still climbs to down", func(t *testing.T) {
		t.Parallel()
		w, _, _ := stalledDown(t)
		assert.Equal(t, Withdrawn, w.s.Fleet.Card("s1-2.w1").Col, "the unstarted card was taken back")
	})

	t.Run("a stalled-down friend is released by a finish", func(t *testing.T) {
		t.Parallel()
		w, seats, t0 := stalledDown(t)
		w.s.Now = t0.Add(42 * time.Minute)
		w.place(w.s.Fleet, "s1-1.w1", FriendRow("amy"), DoneOK)
		w.s.Fleet.Card("s1-1.w1").Fields["finished"] = stamp(w.s.Now)
		released(t, w, w.part(TickFriendStall, TickReq{Friends: seats}), "a finish")
	})

	t.Run("a stalled-down friend is released by a report collected for her", func(t *testing.T) {
		t.Parallel()
		w, seats, t0 := stalledDown(t)
		w.s.Now = t0.Add(43 * time.Minute)
		// friend sync collected her report at 42m: the card's reported stamp
		w.place(w.s.Fleet, "s1-1.w1", FriendRow("amy"), DoneFailed)
		w.s.Fleet.Card("s1-1.w1").Fields[FieldReported] = stamp(t0.Add(42 * time.Minute))
		released(t, w, w.part(TickFriendStall, TickReq{Friends: seats}), "a finish")
	})

	t.Run("a stalled-down friend is released by a read she recorded", func(t *testing.T) {
		t.Parallel()
		w, seats, t0 := stalledDown(t)
		w.s.Now = t0.Add(42 * time.Minute)
		// a frontier read she closed: retired off her row, the read stamp kept
		w.s.Fleet.Put(&Card{ID: "s9-1.r1.amy", Fields: map[string]string{"kind": "read", "reader": "amy", "verdict": "pass", "read": stamp(w.s.Now)}})
		released(t, w, w.part(TickFriendStall, TickReq{Friends: seats}), "a read")
	})

	t.Run("a stalled-down friend is released by a progress stamp", func(t *testing.T) {
		t.Parallel()
		w, seats, t0 := stalledDown(t)
		w.s.Now = t0.Add(42 * time.Minute)
		w.s.Fleet.Card("s1-1.w1").Fields[FieldProgress] = stamp(w.s.Now)
		released(t, w, w.part(TickFriendStall, TickReq{Friends: seats}), "a progress stamp")
	})

	t.Run("a stalled-down friend is released by a beat naming a running job", func(t *testing.T) {
		t.Parallel()
		w, seats, t0 := stalledDown(t)
		w.s.Now = t0.Add(42 * time.Minute)
		beats := map[string]Beat{"amy": {At: w.s.Now, Friend: &FriendReport{Running: []string{"s1-1.w1"}}}}
		released(t, w, w.part(TickFriendStall, TickReq{Beats: beats, Friends: seats}), "a running beat")
	})

	t.Run("evidence older than the stall-down releases nothing", func(t *testing.T) {
		t.Parallel()
		w, seats, t0 := stalledDown(t)
		w.s.Now = t0.Add(42 * time.Minute)
		// a read she recorded at t0, before the ladder climbed, and a beat from then
		w.s.Fleet.Put(&Card{ID: "s9-1.r1.amy", Fields: map[string]string{"kind": "read", "reader": "amy", "verdict": "pass", "read": stamp(t0)}})
		beats := map[string]Beat{"amy": {At: t0, Friend: &FriendReport{Running: []string{"s1-1.w1"}}}}
		p := w.part(TickFriendStall, TickReq{Beats: beats, Friends: seats})
		assert.Nil(t, p.Health, "old evidence releases nothing")
		assert.NotEmpty(t, downOf(w, "amy"), "still down")
	})
}
