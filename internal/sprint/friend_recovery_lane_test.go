package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The lane boundaries of automatic Down recovery (docs/SPEC-SPRINT.md section 1,
// "Automatic Down recovery" and one-lane-per-card.w1; the cold read of nova-sprint #176,
// findings B1 and S1). The primary keep case, a friend Down by evidence whose live lane runs
// the card inside its cap and keeps her Working card, is the author's own witness and is not
// repeated here: these pin the edges of that rule, so the repair keeps exactly a live lane's
// begun card and nothing more, and the deal's one-lane-per-card rule once she is up again.
//
// The lane and beat interface these tests assume, as read at PR head 0c3ebb7b:
//   - FriendSeat.Status is the derived status (Up, Down, Held); FriendSeat.Running is what her
//     last beat names running (work card ids); FriendSeat.Beat is her last stored beat.
//   - Beat.At is the beat's time; Beat.Friend.Running (FriendReport) the lanes it names;
//     Beat.SaysDown() is true when Beat.Friend.Until is set (her daemon's word she is down).
//   - LiveLane(s, r, c) (friend_deadline.go) is true when the card is Ready or Working on her
//     row and either r.Beats[<friend name>] is within FriendLaneLive (2m) of s.Now and names
//     it, or the r.Friends seat of her name names it with Beat.At zero or within FriendLaneLive,
//     and the card has run less than unfinishedLimit(c) (FieldFriendDeadline, else
//     DeadlineUnfinished = 2h) on WorkDeadline's clock (its first take, else its deal).
//   - FriendBeatBound (30m) is how stale a beat may be before her daemon is treated as
//     stopped (FriendEvidence and live.go friendStoppedReturn).
//   - TickStart runs PartFriendRecover, then PartLive, PartRecoveryHeads, PartLevel,
//     PartLevelReads; TickFriendRecover reads s.Friends, else r.Friends.
//   - recoveredLease(wc) is a Withdrawn card whose FieldRecoveredFrom is its row and whose
//     FieldTakenBack starts "taken back from down friend "; laneRunsIt(s, seats, c, wc)
//     returns the friend whose Running names the primary, its work card or the job, "" if none.

// recoveryTick runs the registered TickStart parts with the request as the binding builds
// it: the seats as the roster reads them, and each friend's last beat under her name.
func recoveryTick(w *world, seats []FriendSeat, beats map[string]Beat) {
	w.t.Helper()
	w.s.Friends = seats
	for _, part := range TickStart {
		p, _ := part.Fn(w.s, TickReq{Friends: seats, Beats: beats})
		w.must(Applied(w.s, p))
	}
}

// amyDownByEvidence is amy Down with no session evidence (FriendEvidence: no pong, no
// proof, no finish) while her daemon's beat, taken at `at`, names `running` in its lanes;
// bob is down beside her so no receiver is waiting.
func amyDownByEvidence(at time.Time, running ...string) ([]FriendSeat, map[string]Beat) {
	b := Beat{At: at, Friend: &FriendReport{Running: running}}
	why := "no session evidence: no wake ping answered within 10m0s, no session proof on the beat within 15m0s, no card finished within 30m0s; beat is not evidence"
	seat := FriendSeat{Name: "amy", Width: 1, Status: Down, Class: "flash", Running: running, Beat: b, Evidence: why, Why: why}
	return []FriendSeat{seat, {Name: "bob", Width: 1, Status: Down}}, map[string]Beat{"amy": b}
}

// withdrawnByRecovery is the card as automatic Down recovery leaves it: withdrawn on her
// row at the next generation, recovered from her, with its head debt recorded.
func withdrawnByRecovery(t *testing.T, w *world, id string) {
	t.Helper()
	c := w.s.Fleet.Card(id)
	require.Equal(t, Withdrawn, c.Col, "%s must be withdrawn", id)
	assert.Equal(t, 2, c.Int("gen"), "%s: the generation is revoked", id)
	assert.Equal(t, FriendRow("amy"), c.F(FieldRecoveredFrom), "%s: recovered from amy", id)
	assert.NotEmpty(t, c.F(FieldRecoveryPending), "%s: its old branch's head is owed", id)
}

// Production rule (TickFriendRecover, friend_recovery.go): a Down friend's begun card is kept
// only while a live lane holds it (LiveLane true and her beat not SaysDown); every other
// Working card of hers, and every Ready card, is withdrawn at the next generation.
// Each case here is green on the PR head, which withdraws every card of a Down friend; each
// is the reversed witness of a repair that overreaches (one that keeps a Down friend's card
// on her beat alone, past its cap, on a lane naming another card, or a card never taken).
func TestRecoveryKeepsRunningOnlyWhileLaneIsLive(t *testing.T) {
	t.Parallel()
	// takeWorld: amy at width 1, s1-1.w1 working and s1-2.w1 ready behind it on her row,
	// both taken at t0; her lane has run 25 minutes of its cap at each tick below but one.
	const begun, queued = "s1-1.w1", "s1-2.w1"
	ran := 25 * time.Minute
	cases := []struct {
		name string
		tick func(w *world) time.Duration
		lane func(w *world) ([]FriendSeat, map[string]Beat)
		lost string // the card that must be withdrawn
	}{
		{
			name: "her beat says down: a fresh lane naming the card is overridden",
			tick: func(w *world) time.Duration { return ran },
			lane: func(w *world) ([]FriendSeat, map[string]Beat) {
				seats, beats := amyDownByEvidence(w.s.Now, begun)
				seats[0].Beat.Friend.Until, seats[0].Beat.Friend.Reason = w.s.Now.Add(time.Hour), "usage limit"
				seats[0].Evidence, seats[0].Why = beatSaysDownWhy(seats[0].Beat), beatSaysDownWhy(seats[0].Beat)
				beats["amy"] = seats[0].Beat
				return seats, beats
			},
			lost: begun,
		},
		{
			name: "the lane has run past its cap: late, whatever the beat names",
			tick: func(w *world) time.Duration { return unfinishedLimit(w.s.Fleet.Card(begun)) + time.Minute },
			lane: func(w *world) ([]FriendSeat, map[string]Beat) { return amyDownByEvidence(w.s.Now, begun) },
			lost: begun,
		},
		{
			name: "her lane names another card: no lane holds this one",
			tick: func(w *world) time.Duration { return ran },
			lane: func(w *world) ([]FriendSeat, map[string]Beat) { return amyDownByEvidence(w.s.Now, "other-9.w1") },
			lost: begun,
		},
		{
			name: "her beat names no lane",
			tick: func(w *world) time.Duration { return ran },
			lane: func(w *world) ([]FriendSeat, map[string]Beat) { return amyDownByEvidence(w.s.Now) },
			lost: begun,
		},
		{
			name: "her daemon stopped beating past FriendBeatBound: no lane is live",
			tick: func(w *world) time.Duration { return ran },
			lane: func(w *world) ([]FriendSeat, map[string]Beat) {
				return amyDownByEvidence(w.s.Now.Add(-FriendBeatBound-time.Second), begun)
			},
			lost: begun,
		},
		{
			name: "a ready card never taken is withdrawn whatever her lanes hold",
			tick: func(w *world) time.Duration { return ran },
			lane: func(w *world) ([]FriendSeat, map[string]Beat) { return amyDownByEvidence(w.s.Now, begun) },
			lost: queued,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := takeWorld(t)
			w.tick(tc.tick(w))
			seats, beats := tc.lane(w)
			if tc.lost == begun {
				// the boundary this case stands on: no live lane holds the card, or her beat
				// says down, so no repair that keeps a live lane's card may keep this one
				_, live := LiveLane(w.s, TickReq{Friends: seats, Beats: beats}, w.s.Fleet.Card(begun))
				require.True(t, !live || beats["amy"].SaysDown(), "the lane is not live, or her beat says down")
			}
			gens := gensOf(w.s, tc.lost)
			recoveryTick(w, seats, beats)
			withdrawnByRecovery(t, w, tc.lost)
			late := Finish(w.s, FinishReq{Sel: Sel{IDs: []string{tc.lost}}, As: FriendRow("amy"), Gens: gens, Head: strings.Repeat("a", 40)})
			require.NotEmpty(t, late.Refused, "the revoked generation cannot finish")
			assert.Empty(t, late.Units)
		})
	}
}

// Production rule (laneRunsIt, friend_deal.go): the recovered-from friend's Running is skipped
// only while her status is Down; up again with the lane still running, her lane is a lane,
// and the deal places the card on no row while it runs (one-lane-per-card.w1).
// Red on the PR head: laneRunsIt skips her Running for any recovered lease, so the deal
// places the card while her lane still runs it.
func TestLaneRunsItSkipsFormerOwnerOnlyWhileDown(t *testing.T) {
	t.Parallel()
	w := takeWorld(t)
	const id = "s1-1.w1"
	repo := "https://github.com/mas-bandwidth/nova-tools.git"
	// the same production brief parsing gives the source branch its repository
	w.s.Work.Card("s1-1").Fields["brief"] = "Task.\nREPO: " + repo + "\nWHO: friend\n\nTask."
	old := w.s.Fleet.Card(id)
	branch := BranchOf(w.s.Prefix, w.s.Epoch, old.ID, old.Int("gen"))
	request := RecoveryHeadRequest{Card: id, Gen: old.Int("gen"), CurrentGen: old.Int("gen") + 1, Epoch: w.s.Epoch, Repo: repo, Branch: branch}
	// amy goes down with her lane quiet: her card is recovered, and its head debt resolved
	// on the next tick, so nothing but the lane rule can keep the deal off it
	down := FriendSeat{Name: "amy", Width: 1, Status: Down, Class: "flash"}
	recoveryStart(w, []FriendSeat{down})
	wc := w.s.Fleet.Card(id)
	require.Equal(t, Withdrawn, wc.Col)
	require.True(t, recoveredLease(wc))
	recoveryStart(w, []FriendSeat{down}, RecoveryHead{RecoveryHeadRequest: request, Head: strings.Repeat("b", 40)})
	wc = w.s.Fleet.Card(id)
	require.Empty(t, wc.F(FieldRecoveryPending))
	pr := w.s.Work.Card("s1-1")

	// her stored beat still names the card while she is down: not a lane, so the card can
	// be dealt elsewhere (the PR's rule, kept)
	down.Running = []string{id}
	assert.Equal(t, "", laneRunsIt(w.s, []FriendSeat{down}, pr, wc), "while she is down her stored beat is no lane")

	// up again with the same lane still running: her lane is a lane
	up := down
	up.Status = Up
	bob := FriendSeat{Name: "bob", Width: 1, Status: Up, Class: "flash"}
	assert.Equal(t, "amy", laneRunsIt(w.s, []FriendSeat{up, bob}, pr, wc), "up again, her running lane is a lane")

	// and the deal places the card on no row while it runs: not on bob's (a second lane),
	// not back on hers (her old lane would read as the new generation's start)
	dealStarted(w, up, bob)
	wc = w.s.Fleet.Card(id)
	assert.Equal(t, Withdrawn, wc.Col, "the card waits while her lane runs it")
	assert.Equal(t, Ready, w.s.StateOf("s1-1"))
	assert.Empty(t, w.s.Fleet.Cell(FriendRow("bob"), Ready), "no second row while a lane runs it")
	assert.Empty(t, w.s.Fleet.Cell(FriendRow("bob"), Working), "no second row while a lane runs it")
	assert.Equal(t, 2, wc.Int("gen"), "no new generation launched over a running lane")

	// her lane stops naming it: dealt as any card is
	up.Running = nil
	dealStarted(w, up, bob)
	assert.NotEqual(t, Withdrawn, w.s.Fleet.Card(id).Col, "once her beat stops naming it, the card is dealt")
}
