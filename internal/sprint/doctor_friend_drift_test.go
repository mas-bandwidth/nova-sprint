package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Her --running words are not her row's cards (a job name, a primary): a running list shorter or
// longer than her working cards is no drift when her live set names them.
func TestDoctorFriendsRunningWordsAreNotComparedToTheRow(t *testing.T) {
	t.Parallel()
	beat := Beat{Friend: &FriendReport{Running: []string{"one-job-for-both"}}, LiveKnown: true, LiveSince: t0.Add(-time.Hour),
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
	assert.NotContains(t, d[0].Evidence, "p1.w1@2 (", "the card her set names is not drifted")
	assert.Equal(t, "nova-sprint friend reconcile zhi", d[0].Remedy)
}

// The settling window: a card unnamed for less than LiveGrace + the settle is not drifted, and a
// beat that is no longer fresh (past BeatDeadline) is no word that a run is gone, as the tick's
// own reconcile reads it (LiveReturns).
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
