package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
// 2026-10-14 called johnny, stella and zhi DRIFT on "beat running and row working differ" for
// it). This test uses only the surface from before the change, so it runs on the code before it
// and fails there.
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
