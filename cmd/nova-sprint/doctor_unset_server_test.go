package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// advisoryUnsetServer is the doctor's advisory for a server the shell does not name.
const advisoryUnsetServer = "server: not configured in this shell (set NOVA_SPRINT_SERVER or --server)"

// An unconfigured server is no fault (docs/SPEC-SPRINT.md, "The seat check"): with no
// address in the environment and none in the seat's login, the friends area and the
// doctor's machinery report it as the advisory
// `server: not configured in this shell (set NOVA_SPRINT_SERVER or --server)` and stay
// green, never a DOWN verdict. A configured server that does not answer is unchanged.
func TestDoctorFriendsAreaCallsAnUnsetServerUnconfigured(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("start")
	ta.ok("tick")
	o := doctorOutside(ta, []string{"m1"}, nil)
	// no NOVA_SPRINT_SERVER and no seat login: the shell names no server
	o.serverAddr = func() (string, bool) { return "", false }
	ta.a.outside = o

	code, out, errs := ta.do("doctor")
	require.Equal(t, 0, code, "an unconfigured server is no fault\n%s%s", out, errs)
	assert.Contains(t, out, advisoryUnsetServer, "the advisory names the fix: %s", out)
	assert.NotContains(t, out, "MACHINERY server DOWN", "an unconfigured server is no down verdict: %s", out)
	assert.Contains(t, out, "DOCTOR friends GREEN", "the friends layer stays green: %s", out)

	code, out, errs = ta.do("doctor --area friends")
	require.Equal(t, 0, code, "the friends area does not fail on an unconfigured server\n%s%s", out, errs)
	assert.Contains(t, out, advisoryUnsetServer, "the friends area reports the advisory: %s", out)
	assert.NotContains(t, out, "MACHINERY server DOWN", "no down verdict: %s", out)
}

var errServerUnanswered = errors.New("connection refused")

// A configured server that does not answer is unchanged: it stays DOWN, and the doctor
// still names it as a fault, so the advisory only covers the unconfigured case.
func TestDoctorCallsAConfiguredServerThatDoesNotAnswerDown(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("start")
	ta.ok("tick")
	o := doctorOutside(ta, []string{"m1"}, nil)
	o.serverAddr = func() (string, bool) { return "127.0.0.1:7399", false }
	o.roundTrip = func(context.Context, string) (time.Duration, error) { return 0, errServerUnanswered }
	ta.a.outside = o

	code, out, errs := ta.do("doctor")
	require.Equal(t, 1, code, "a configured server that does not answer is RED\n%s%s", out, errs)
	assert.Contains(t, out, "MACHINERY server DOWN", "the verdict is kept: %s", out)
	assert.NotContains(t, out, advisoryUnsetServer, "a configured server is not unconfigured: %s", out)
}
