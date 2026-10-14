package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// friendDriftInput is the doctor's read of one friend, name, with two cards working on her row
// (p1.w1 and p2.w1, taken taken ago) and the beat given, 5 seconds old at the world's now.
func friendDriftInput(t *testing.T, name string, taken time.Duration, beat Beat) (DoctorInput, *world) {
	t.Helper()
	w := doctorWorld(t)
	row := FriendRow(name)
	w.s.Fleet.SetRows(append(w.s.Fleet.Rows(), row))
	for _, p := range []string{"p1", "p2"} {
		putWorkCard(w, p, row, Working, 1, map[string]string{"taken": stamp(w.s.Now.Add(-taken)), "dealt": stamp(w.s.Now.Add(-taken))})
	}
	in := healthyInput(w)
	in.Req.Friends = []FriendSeat{{Name: name, Status: Up, Width: 4}}
	beat.At = w.s.Now.Add(-5 * time.Second)
	in.FriendBeats = map[string]Beat{name: beat}
	return in, w
}

func driftsOn(cs []DoctorCheck) []DoctorCheck {
	var out []DoctorCheck
	for _, c := range cs {
		if c.Check == CheckFriends && c.Status == DoctorDrift {
			out = append(out, c)
		}
	}
	return out
}

// A friend who works by hand, or a one-shot daemon, beats with no running list and no live set
// while her row holds working cards: that is her normal state, never a drift (the doctor of
// 2026-10-14 called johnny, stella and zhi DRIFT on "beat running and row working differ" for it).
func TestDoctorFriendsAHandWorkingFriendWithNoLiveSetIsNotDrift(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"johnny", "stella", "zhi"} {
		in, _ := friendDriftInput(t, name, time.Hour, Beat{Friend: &FriendReport{}})
		cs := DoctorFriends(in)
		assert.Empty(t, driftsOn(cs), "%s: %s", name, lines(cs))
		assert.Empty(t, failsOn(cs, CheckFriends), "%s: %s", name, lines(cs))
		assert.Contains(t, lines(cs), "DOCTOR OK friends up", name)
	}
}

// Her --running words are not her row's cards (a job name, a primary): a running list shorter or
// longer than her working cards is no drift when her live set names them.
func TestDoctorFriendsRunningWordsAreNotComparedToTheRow(t *testing.T) {
	t.Parallel()
	now := t0
	beat := Beat{Friend: &FriendReport{Running: []string{"one-job-for-both"}}, LiveKnown: true, LiveSince: now.Add(-time.Hour),
		Live: []string{"p1.w1@2", "p2.w1@2:held"}}
	in, _ := friendDriftInput(t, "stella", time.Hour, beat)
	cs := DoctorFriends(in)
	assert.Empty(t, driftsOn(cs), lines(cs))
}

// A fresh live set that does not name a working card long past the grace and the settle is the
// drift: the evidence names the card and how long, the remedy is the reconcile that returns it.
func TestDoctorFriendsAnUnnamedWorkingCardPastTheSettleIsDrift(t *testing.T) {
	t.Parallel()
	beat := Beat{Friend: &FriendReport{}, LiveKnown: true, LiveSince: t0.Add(-time.Hour), Live: []string{"p1.w1@2"}}
	in, _ := friendDriftInput(t, "zhi", 10*time.Minute, beat)
	cs := DoctorFriends(in)
	d := driftsOn(cs)
	require.Len(t, d, 1, lines(cs))
	assert.Equal(t, "zhi", d[0].Subject)
	assert.Contains(t, d[0].Evidence, "p2.w1@2 (10m0s)")
	assert.NotContains(t, d[0].Evidence, "p1.w1@2", "the card her set names is not drifted")
	assert.Equal(t, "nova-sprint friend reconcile zhi", d[0].Remedy)
}

// The settling window: a card taken moments ago, or unnamed for less than LiveGrace + the settle,
// is not drifted, and a beat that is no longer fresh (past BeatDeadline) is no word that a run is
// gone, as the tick's own reconcile reads it (LiveReturns).
func TestDoctorFriendsSettlesBeforeCallingDrift(t *testing.T) {
	t.Parallel()
	live := func() Beat {
		return Beat{Friend: &FriendReport{}, LiveKnown: true, LiveSince: t0.Add(-time.Hour), Live: []string{}}
	}
	in, _ := friendDriftInput(t, "johnny", LiveGrace+DoctorLiveSettle-time.Second, live())
	assert.Empty(t, driftsOn(DoctorFriends(in)), "unnamed for less than the grace and the settle")
	in, _ = friendDriftInput(t, "johnny", LiveGrace+DoctorLiveSettle+time.Second, live())
	assert.Len(t, driftsOn(DoctorFriends(in)), 1, "unnamed past it")
	in, w := friendDriftInput(t, "johnny", time.Hour, live())
	b := in.FriendBeats["johnny"]
	b.At = w.s.Now.Add(-(BeatDeadline + time.Second))
	in.FriendBeats["johnny"] = b
	assert.Empty(t, driftsOn(DoctorFriends(in)), "a beat past BeatDeadline names nothing")
}

// The doctor and the tick read one instant: a card the doctor calls drifted is a card LiveReturns
// returns (the same liveAbsentSince), so the remedy is never for a case the tick does not know.
func TestDoctorFriendsDriftIsWhatLiveReturnsWouldReturn(t *testing.T) {
	t.Parallel()
	beat := Beat{Friend: &FriendReport{}, LiveKnown: true, LiveSince: t0.Add(-time.Hour), Live: []string{"p1.w1@2"}}
	in, w := friendDriftInput(t, "zhi", 10*time.Minute, beat)
	require.Len(t, driftsOn(DoctorFriends(in)), 1)
	p := LiveReturns(w.s, in.FriendBeats, false)
	require.Len(t, p.Units, 1)
	assert.Equal(t, "p2.w1", p.Units[0].Key)
}
