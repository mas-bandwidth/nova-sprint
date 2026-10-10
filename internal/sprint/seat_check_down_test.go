package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Down is not an error when they are actually down (Glenn, 2026-10-10): a member that
// does not beat on an offline host is down; FAULT only when the host is reachable.
func TestMemberVerdictSaysDownOnlyWhenTheHostIsOffline(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		m    MemberM
		want string
	}{
		{"offline host, never beat", MemberM{Name: "captain", Status: "down", HostOffline: true}, MemberDown},
		{"offline host, stale beat", MemberM{Name: "hulk", Status: "down", Beaten: true, Age: time.Hour, HostOffline: true}, MemberDown},
		{"reachable host, never beat", MemberM{Name: "m1", Status: "down"}, MemberFault},
		{"reachable host, stale beat", MemberM{Name: "m1", Status: "up", Beaten: true, Age: time.Hour}, MemberFault},
		{"reachable host, beating", MemberM{Name: "m1", Status: "up", Beaten: true, Age: 3 * time.Second}, MemberOK},
		{"held is held, offline or not", MemberM{Name: "m2", Status: "held", HostOffline: true}, MemberHeld},
		{"beating is ok whatever the probe said", MemberM{Name: "m1", Status: "up", Beaten: true, Age: time.Second, HostOffline: true}, MemberOK},
	} {
		assert.Equal(t, c.want, MemberVerdict(c.m), c.name)
	}
}

func TestSeatCheckFleetLineKeepsAnOfflineMemberUpAndNamesIt(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	m := upMeasures()
	m.Fleet = append(m.Fleet, MemberM{Name: "captain", Status: "down", HostOffline: true})
	r := JudgeSeatCheck(m, now)
	assert.Equal(t, 0, r.ExitCode)
	assert.Contains(t, r.Text(), "MACHINERY fleet OK up=1 held=1 offline=captain down=0")
	// the same member on a reachable host is a fault
	m.Fleet[len(m.Fleet)-1].HostOffline = false
	r = JudgeSeatCheck(m, now)
	assert.Equal(t, 1, r.ExitCode)
	assert.Contains(t, r.Text(), "MACHINERY fleet DOWN up=1 held=1 down=captain:never")
}
