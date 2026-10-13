package main

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// refusalRig is the land loop with every outside thing faked: the merge queue's count
// (queueSeam), land itself (landSeam), the goroutine a flight runs on (startSeam), and
// the clock (the test app's). The store is the in-memory one, so the judgment the loop
// raises is read back through the inbox. No git, no socket, no real time.
type refusalRig struct {
	t     *testing.T
	ta    *testApp
	mu    sync.Mutex
	why   string // what land refuses with; "" lands
	coord string // the coordinator the queue names
	hold  chan struct{}
	calls int
}

func newRefusalRig(t *testing.T) *refusalRig {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	r := &refusalRig{t: t, ta: ta, coord: "coordinator"}
	b := ta.a.landState()
	b.mu.Lock()
	b.queueSeam = func(context.Context, string) (int, string, string, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return 6, r.coord, "s1", nil
	}
	b.landSeam = func(_ []string, stdout, stderr io.Writer, _ **hookGateError) int {
		r.mu.Lock()
		why, hold := r.why, r.hold
		r.calls++
		r.mu.Unlock()
		if hold != nil {
			<-hold
		}
		if why == "" {
			_, _ = fmt.Fprintln(stdout, "LAND OK stream=s1 cards=1 bench=here wall=0.1s")
			_, _ = fmt.Fprintln(stdout, "LAND DONE landed=1")
			return 0
		}
		return refuse(stderr, "land", why)
	}
	b.startSeam = func(run func()) { run() }
	b.mu.Unlock()
	return r
}

// beat is one cycle of the loop on the fake clock: its lines, and the flight it leaves.
func (r *refusalRig) beat(flight *landFlight) (string, *landFlight) {
	r.t.Helper()
	var out beatBuf
	next := r.ta.a.landCycle(r.t.Context(), "mem:0", &out, flight)
	r.ta.mu.Lock()
	r.ta.now = r.ta.now.Add(LandEvery)
	r.ta.mu.Unlock()
	return out.String(), next
}

func (r *refusalRig) set(why string) {
	r.mu.Lock()
	r.why = why
	r.mu.Unlock()
}

// judgments is the open judgments of the lander's standing refusal, read from the store.
func (r *refusalRig) judgments() []sprint.Open {
	r.t.Helper()
	st, err := r.ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(r.t, err)
	open, err := st.B.OpenNotes(r.t.Context())
	require.NoError(r.t, err)
	var got []sprint.Open
	for _, o := range open {
		if o.Note.Kind == sprint.Judgment && o.Note.Type == sprint.NLanderRefuses {
			got = append(got, o)
		}
	}
	return got
}

// A refusal is a state, not an event (tla/ServerSwitch.tla LandRefused, LandOk): every
// beat it stands writes LAND REFUSED naming the reason and the remedy, never a bare
// LAND IDLE with cards queued; the first beat of a reason raises one judgment at once;
// a landing closes it; a new reason raises a new one; a flight truly running still
// writes LAND IDLE why=running, and a queue with no coordinator why=no-coordinator.
func TestALandRefusalIsNamedEveryBeatAndJudgedAtOnce(t *testing.T) {
	t.Parallel()
	r := newRefusalRig(t)
	identity := "the sprint row names no git_identity, so the landing would commit as the host's global git config; " + identityRemedy
	r.set(identity)

	refusedLine := regexp.MustCompile(`LAND REFUSED queued=6 since=(\S+) reason=(.*) run: (.*)`)
	var flight *landFlight
	var out string
	for i := range 30 {
		out, flight = r.beat(flight)
		assert.Nil(t, flight, "beat %d: an inline flight is reported on its own beat", i+1)
		assert.NotContains(t, out, "LAND IDLE", "beat %d: a standing refusal is never a bare IDLE:\n%s", i+1, out)
		m := refusedLine.FindStringSubmatch(out)
		if !assert.NotNil(t, m, "beat %d writes LAND REFUSED with the reason and the remedy:\n%s", i+1, out) {
			continue
		}
		assert.Equal(t, (time.Duration(i) * LandEvery).String(), m[1], "beat %d: since is the refusal's age", i+1)
		assert.Contains(t, m[2], "the sprint row names no git_identity", "beat %d: the reason", i+1)
		assert.NotContains(t, m[2], "run:", "beat %d: the reason is the reason alone", i+1)
		assert.Contains(t, m[3], "nova-config sprint set --git_identity", "beat %d: the remedy", i+1)
		assert.Equal(t, 1, strings.Count(out, "LAND REFUSED queued="), "beat %d: one standing line:\n%s", i+1, out)
		js := r.judgments()
		if assert.Len(t, js, 1, "beat %d: exactly one judgment, raised on the first beat", i+1) {
			assert.Contains(t, js[0].Note.What, "the sprint row names no git_identity")
			assert.Contains(t, js[0].Note.What, "nova-config sprint set --git_identity")
			assert.Contains(t, js[0].Note.Decisions, "ack")
		}
	}

	// the tick's landing-stall judgment names the standing refusal
	st, err := r.ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(t, err)
	s, err := st.Load(t.Context(), []string{sprint.Work}, nil)
	require.NoError(t, err)
	assert.Contains(t, sprint.LandRefusalStanding(s), "the sprint row names no git_identity", "the stall alarm's cause")

	// a landing goes through: LAND OK, and the judgment is closed
	r.set("")
	out, flight = r.beat(flight)
	require.Nil(t, flight)
	assert.Contains(t, out, "LAND OK stream=s1")
	assert.NotContains(t, out, "LAND REFUSED")
	assert.Empty(t, r.judgments(), "a landing closes the standing refusal's judgment")
	s, err = st.Load(t.Context(), []string{sprint.Work}, nil)
	require.NoError(t, err)
	assert.Empty(t, sprint.LandRefusalStanding(s), "no refusal stands after a landing")

	// a second, different reason raises a second judgment
	other := "the merge queue's fence names another server; run: nova-sprint where"
	r.set(other)
	for i := range 3 {
		out, flight = r.beat(flight)
		require.Nil(t, flight)
		m := refusedLine.FindStringSubmatch(out)
		if assert.NotNil(t, m, "second reason, beat %d:\n%s", i+1, out) {
			assert.Contains(t, m[2], "the merge queue's fence names another server")
			assert.Equal(t, "nova-sprint where", m[3])
		}
		js := r.judgments()
		if assert.Len(t, js, 1, "second reason, beat %d: one judgment", i+1) {
			assert.Contains(t, js[0].Note.What, "the merge queue's fence names another server")
			assert.NotContains(t, js[0].Note.What, "git_identity", "the first reason's judgment was closed")
		}
	}
	// the same reason changing back is a new reason again: a third judgment, the second closed
	r.set(identity)
	out, flight = r.beat(flight)
	require.Nil(t, flight)
	assert.Regexp(t, refusedLine, out)
	if js := r.judgments(); assert.Len(t, js, 1) {
		assert.Contains(t, js[0].Note.What, "git_identity")
	}

	// a landing clears it, then a flight truly running writes LAND IDLE why=running
	r.set("")
	_, flight = r.beat(flight)
	require.Nil(t, flight)
	require.Empty(t, r.judgments())
	hold := make(chan struct{})
	r.mu.Lock()
	r.hold = hold
	r.mu.Unlock()
	b := r.ta.a.landState()
	b.mu.Lock()
	b.startSeam = nil // the flight runs beside the loop, held at land until released
	b.mu.Unlock()
	out, flight = r.beat(flight)
	require.NotNil(t, flight, "the held flight is still running")
	assert.Regexp(t, `LAND IDLE queued=6 step=land since=0s why=running`, out)
	out, flight = r.beat(flight)
	assert.Regexp(t, `LAND IDLE queued=6 step=land since=2s why=running`, out)
	close(hold)
	for {
		flight.mu.Lock()
		done := flight.done
		flight.mu.Unlock()
		if done {
			break
		}
		runtime.Gosched()
	}
	out, flight = r.beat(flight)
	require.Nil(t, flight)
	assert.Contains(t, out, "LAND OK stream=s1")
	assert.NotContains(t, out, "LAND IDLE")

	// cards queued and no coordinator: IDLE, saying why
	r.mu.Lock()
	r.coord, r.hold = "", nil
	r.mu.Unlock()
	out, _ = r.beat(nil)
	assert.Regexp(t, `LAND IDLE queued=6 step=- since=0s why=no-coordinator`, out)
}

// The standing refusal's reason and remedy are read off land's own refusal line: the
// text after REFUSED, split at its last "; run: " (refuse in main.go always names one).
func TestALandRefusalSplitsIntoReasonAndRemedy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ said, reason, remedy string }{
		{"nova-sprint land REFUSED: the sprint row names no git_identity; run: nova-config sprint set --git_identity x then nova-config apply",
			"the sprint row names no git_identity", "nova-config sprint set --git_identity x then nova-config apply"},
		{"LAND FAILED the merge queue could not be read: boom; nothing was landed, and the next round tries again; run: nova-sprint where",
			"LAND FAILED the merge queue could not be read: boom; nothing was landed, and the next round tries again", "nova-sprint where"},
		{"LAND REFUSED stream=s1 conflict on s1-2\nnova-sprint land REFUSED: one batch refused; run: nova-sprint land --dry-run",
			"LAND REFUSED stream=s1 conflict on s1-2", "nova-sprint land --dry-run"},
		{"something with no remedy", "something with no remedy", landRefusalRemedy},
	} {
		reason, remedy := landRefusalOf(tc.said)
		assert.Equal(t, tc.reason, reason, tc.said)
		assert.Equal(t, tc.remedy, remedy, tc.said)
	}
}
