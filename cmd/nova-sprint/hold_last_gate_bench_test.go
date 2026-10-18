package main

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// TestHoldRefusesTheLastGateBench is the CLI's half of the hold guard: holding the last
// member that can run a gate when no other member is a bench is refused, at exit 2 with
// the refusal's line and nothing written; holding one while another is a coordinator-held
// (recoverable) bench, and a single-member fleet's own hold, are never refused.
func TestHoldRefusesTheLastGateBench(t *testing.T) {
	t.Parallel()
	// a machine holds m1 (no recoverable bench): holding m2, the last member that can run
	// a gate, is refused at exit 2 naming it, nothing written
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1,m2")
	st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(t, err)
	_, err = st.Run(context.Background(), store.FleetStep(sprint.FleetReq{Op: "hold", Member: "m1", Who: "machine", HeldBy: sprint.HeldByFault}))
	require.NoError(t, err)
	code, _, errs := ta.do("hold m2 --reason r")
	assert.Equal(t, 2, code, errs)
	assert.Contains(t, errs, "m2", "it names the last member that could run a gate")
	assert.NotContains(t, ta.ok("log"), "member m2 held", "a refused hold writes nothing")

	// holding m1 (m2 remains a bench) passes, and a coordinator-held m1 is still a
	// recoverable bench: holding m2 is then allowed
	ok := newTestApp(t)
	ok.ok("init --readers reader-a --members m1,m2")
	ok.ok("hold m1 --reason r")
	code, _, errs = ok.do("hold m2 --reason r")
	assert.Equal(t, 0, code, errs)

	// a single-member fleet holds its only member freely: its routine maintenance flow
	single := newTestApp(t)
	single.ok("init --readers reader-a --members m1")
	code, _, errs = single.do("hold m1 --reason r")
	assert.Equal(t, 0, code, errs)
	assert.Contains(t, single.ok("log"), "member m1 held")
}

// TestHoldForceLastRecordsTheOverride pins the override on the log line: --force-last
// holds the last bench (m1 is machine-held, m2 is the last member that can run a gate)
// and says so on the hold's own happened note.
func TestHoldForceLastRecordsTheOverride(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1,m2")
	st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(t, err)
	_, err = st.Run(context.Background(), store.FleetStep(sprint.FleetReq{Op: "hold", Member: "m1", Who: "machine", HeldBy: sprint.HeldByFault}))
	require.NoError(t, err)
	ta.ok("hold m2 --reason r --force-last")
	var line string
	for _, l := range strings.Split(ta.ok("log"), "\n") {
		if strings.Contains(l, "member m2 held") {
			line = strings.TrimSpace(l)
		}
	}
	require.NotEmpty(t, line, "the log holds the forced hold")
	assert.True(t, strings.HasSuffix(line, "--force-last: no gate bench left"), line)
}

// allNotesOf is every note of one type ever written, open or closed.
func (r *refusalRig) allNotesOf(typ string) []sprint.Note {
	r.t.Helper()
	st, err := r.ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(r.t, err)
	notes, _, err := st.B.NotesSince(r.t.Context(), "", 100000)
	require.NoError(r.t, err)
	var got []sprint.Note
	for _, n := range notes {
		if n.Type == typ {
			got = append(got, n)
		}
	}
	return got
}

// TestTheNoGateBenchPushFiresOncePerOutage drives the land loop through one outage of
// no gate bench: exactly one judgment is written with the streams and the cause, the
// resume writes one note, and a second outage judges once more.
func TestTheNoGateBenchPushFiresOncePerOutage(t *testing.T) {
	t.Parallel()
	r := newRefusalRig(t)
	b := r.ta.a.landState()
	b.mu.Lock()
	b.streamsSeam = func(context.Context, string) ([]string, error) { return []string{"s1", "s2"}, nil }
	b.mu.Unlock()
	r.set(benchGateUnavailableWhy)
	var flight *landFlight
	for range 3 {
		var out string
		out, flight = r.beat(flight)
		require.Nil(t, flight, out)
		assert.Contains(t, out, "LAND REFUSED")
		assert.Len(t, r.judgments(), 1, "one judgment while the outage stands")
	}
	js := r.allJudgments()
	require.Len(t, js, 1, "one judgment for the outage")
	assert.Contains(t, js[0].What, noGateBenchWhy)
	assert.Contains(t, js[0].What, "streams: s1,s2")
	assert.Contains(t, js[0].What, "nova-sprint fleet unhold <member> restores it")
	assert.NotContains(t, js[0].What, "configured remote bench")

	r.set("")
	_, flight = r.beat(flight)
	require.Nil(t, flight)
	assert.Empty(t, r.judgments(), "the bench returned: the judgment is closed")
	assert.Len(t, r.allNotesOf("landing resumed"), 1, "one resume note")
	assert.Equal(t, "landing resumed: a gate bench ran", r.allNotesOf("landing resumed")[0].What)

	r.set(benchGateUnavailableWhy)
	_, flight = r.beat(flight)
	require.Nil(t, flight)
	require.Len(t, r.judgments(), 1, "a second outage judges once again")
	assert.Len(t, r.allJudgments(), 2, "one judgment per outage")
}
