package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The link fact of the member-env check (docs/SPEC-SPRINT.md, doctor: member-env; the card
// doctor-names-a-member-whose-link-flapped): two or more changes in DoctorLinkWindow are one
// WARN naming the last line, never a FAIL; one change (a single up at boot) or a fact not
// measured is no line, and the member-env check's other lines are unchanged.

// a member whose host counted four link changes in the window is one WARN naming the last line,
// and it is never a FAIL.
func TestDoctorMemberEnvWarnsOnAFlappingLink(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	in := healthyInput(w)
	p := fullProbe("m1")
	p.LinkChanges = 4
	p.LinkLast = "igc 0000:01:00.0 enp1s0: NIC Link is Up 2500 Mbps"
	in.Probes = map[string]MemberProbe{"m1": p}
	cs := DoctorMemberEnv(in)
	require.Len(t, cs, 1, lines(cs))
	assert.Equal(t, DoctorWarn, cs[0].Status, lines(cs))
	assert.Equal(t, "m1", cs[0].Subject)
	assert.Equal(t, CheckMemberEnv, cs[0].Check)
	assert.Contains(t, cs[0].Evidence, "4 link changes in the last 1h, the last: igc 0000:01:00.0 enp1s0: NIC Link is Up 2500 Mbps")
	assert.Contains(t, cs[0].Remedy, "journalctl -k --since -1h | grep -iE")
	assert.Empty(t, failsOn(cs, CheckMemberEnv), "a flapping link is never a FAIL: %s", lines(cs))
}

// one change or a fact not measured is no link line, and the member-env check's other lines are
// unchanged.
func TestDoctorMemberEnvQuietOnOneOrNoLinkFact(t *testing.T) {
	t.Parallel()
	w := doctorWorld(t)
	for _, n := range []int{1, 0, -1} {
		in := healthyInput(w)
		p := fullProbe("m1")
		p.LinkChanges = n
		in.Probes = map[string]MemberProbe{"m1": p}
		cs := DoctorMemberEnv(in)
		for _, c := range cs {
			assert.NotContains(t, c.Evidence, "link changes", "n=%d: %s", n, c.Line())
			assert.NotEqual(t, CheckPreflightLinks, c.Check, "n=%d: %s", n, c.Line())
		}
		require.Len(t, cs, 1, "only the OK line, nothing about the link: %s", lines(cs))
		assert.Equal(t, DoctorOK, cs[0].Status, lines(cs))
	}
}
