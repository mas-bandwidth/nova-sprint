package main

import (
	"context"
	"errors"
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
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
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
	// pass, when set, is land itself: what it prints and its exit
	pass func(call int, stdout, stderr io.Writer) int
	// queue, when set, is the queue's coordinator on each read (the cycle's, then land's)
	queue func(read int) string
	reads int
}

func newRefusalRig(t *testing.T) *refusalRig {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	return newRefusalRigOn(t, ta)
}

// newRefusalRigOn is the rig on a test app whose store is already made: a second
// server on the first's store is a restart.
func newRefusalRigOn(t *testing.T, ta *testApp) *refusalRig {
	t.Helper()
	r := &refusalRig{t: t, ta: ta, coord: "coordinator"}
	b := ta.a.landState()
	b.mu.Lock()
	b.queueSeam = func(context.Context, string) (int, string, string, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.reads++
		if r.queue != nil {
			return 6, r.queue(r.reads), "s1", nil
		}
		return 6, r.coord, "s1", nil
	}
	b.landSeam = func(_ []string, stdout, stderr io.Writer, _ **hookGateError) int {
		r.mu.Lock()
		why, hold, pass := r.why, r.hold, r.pass
		r.calls++
		call := r.calls
		r.mu.Unlock()
		if hold != nil {
			<-hold
		}
		if pass != nil {
			return pass(call, stdout, stderr)
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
	return r.judgmentsOf(sprint.NLanderRefuses)
}

// judgmentsOf is the open judgments of one type, read from the store.
func (r *refusalRig) judgmentsOf(typ string) []sprint.Open {
	r.t.Helper()
	st, err := r.ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(r.t, err)
	open, err := st.B.OpenNotes(r.t.Context())
	require.NoError(r.t, err)
	var got []sprint.Open
	for _, o := range open {
		if o.Note.Kind == sprint.Judgment && o.Note.Type == typ {
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
	for _, tc := range []struct {
		said                 string
		code                 int
		kind, reason, remedy string
	}{
		{"nova-sprint land REFUSED: the sprint row names no git_identity; run: nova-config sprint set --git_identity x then nova-config apply", 2,
			causeRefused, "the sprint row names no git_identity", "nova-config sprint set --git_identity x then nova-config apply"},
		{"LAND FAILED the merge queue could not be read: boom; nothing was landed, and the next round tries again; run: nova-sprint where", 2,
			causeQueue, "LAND FAILED the merge queue could not be read: boom; nothing was landed, and the next round tries again", "nova-sprint where"},
		{"LAND OK stream=s1 cards=1 base=main tip=abc ids=s1-1 fetch=0.2s\nnova-sprint land REFUSED: the sprint row names no git_identity; run: nova-config apply", 1,
			causeRefused, "the sprint row names no git_identity", "nova-config apply"},
		{"nova-sprint land: the sprint is at epoch 3, not 2 (cleared since): nothing was fetched, pushed or reported; run: nova-sprint where", 1,
			causeFailed, "the sprint is at epoch 3, not 2 (cleared since): nothing was fetched, pushed or reported", "nova-sprint where"},
		{"something with no remedy", 2, causeFailed, "something with no remedy", landRefusalRemedy},
		{"", 2, causeFailed, "land exited 2 and said nothing", landRefusalRemedy},
	} {
		kind, reason, remedy := landRefusalOf(tc.said, tc.code)
		assert.Equal(t, tc.kind, kind, tc.said)
		assert.Equal(t, tc.reason, reason, tc.said)
		assert.Equal(t, tc.remedy, remedy, tc.said)
	}
}

// Land exits 1 when any batch is refused, though others landed (land.go report): such a
// pass reached its batches, so no refusal stands and none is judged, and a refusal that
// stood is cleared by it; each refused batch has its own record (PropLandRefusal) and
// judgment (tla/ServerSwitch.tla LandPartial).
func TestAPassOfLandedAndRefusedBatchesIsNoStandingRefusal(t *testing.T) {
	t.Parallel()
	r := newRefusalRig(t)
	r.set("the sprint row names no git_identity; run: nova-config apply")
	out, _ := r.beat(nil)
	require.Contains(t, out, "LAND REFUSED queued=6")
	require.Len(t, r.judgments(), 1)
	r.mu.Lock()
	r.pass = func(call int, stdout, stderr io.Writer) int {
		_, _ = fmt.Fprintf(stdout, "LAND OK stream=s1 cards=1 base=main tip=abc ids=s1-1 fetch=0.%ds merge=0.1s\n", call)
		_, _ = fmt.Fprintf(stderr, "LAND REFUSED stream=s2 cards=2 base=main tip=- ids=s2-1..s2-2 fetch=0.%ds merge=0.2s\n", call)
		_, _ = fmt.Fprintln(stderr, "NOTE nothing was pushed or reported for stream s2; its cards stay queued")
		return 1
	}
	r.mu.Unlock()
	for i := range 5 {
		out, _ = r.beat(nil)
		assert.NotContains(t, out, "LAND REFUSED queued=", "beat %d: a pass that reached its batches is no standing refusal:\n%s", i+1, out)
		assert.NotContains(t, out, "reason=LAND OK", "beat %d", i+1)
		assert.Contains(t, out, "LAND OK stream=s1", "beat %d: the pass's own lines", i+1)
		assert.Empty(t, r.judgments(), "beat %d: no standing-refusal judgment, and the one that stood is closed", i+1)
	}
}

// A refusal whose text carries a pass's timings or counts is the same refusal pass
// after pass (sprint.LandRefusalKey): one judgment, never closed and raised again, and
// its since keeps growing.
func TestARefusalWhoseTextVariesIsOneRefusal(t *testing.T) {
	t.Parallel()
	r := newRefusalRig(t)
	r.mu.Lock()
	r.pass = func(call int, _, stderr io.Writer) int {
		return refuse(stderr, "land", fmt.Sprintf("the fence names another server, seen %d.%ds ago at tick %d; run: nova-sprint where", call, call*7%10, 1000+call))
	}
	r.mu.Unlock()
	var first string
	for i := range 12 {
		out, _ := r.beat(nil)
		m := regexp.MustCompile(`LAND REFUSED queued=6 since=(\S+) reason=`).FindStringSubmatch(out)
		if assert.NotNil(t, m, "beat %d:\n%s", i+1, out) {
			assert.Equal(t, (time.Duration(i) * LandEvery).String(), m[1], "beat %d: since grows", i+1)
		}
		js := r.judgments()
		if assert.Len(t, js, 1, "beat %d: one judgment", i+1) {
			if first == "" {
				first = js[0].Note.ID
			}
			assert.Equal(t, first, js[0].Note.ID, "beat %d: the same judgment, never closed and raised again", i+1)
		}
	}
	all := r.allJudgments()
	assert.Len(t, all, 1, "one judgment was ever written")
}

// A judgment the store does not take is said, once, as LAND JUDGMENT NOT WRITTEN, and
// written on the first beat the store takes it.
func TestAJudgmentNotWrittenIsSaidOnce(t *testing.T) {
	t.Parallel()
	r := newRefusalRig(t)
	var down sync.Mutex
	broken := true
	backend := r.ta.a.backend
	r.ta.a.backend = func(ctx context.Context, addr string, names sprint.Names) (store.Backend, error) {
		down.Lock()
		b := broken
		down.Unlock()
		if b {
			return nil, errors.New("the store is down")
		}
		return backend(ctx, addr, names)
	}
	r.set("the sprint row names no git_identity; run: nova-config apply")
	var text string
	for range 5 {
		out, _ := r.beat(nil)
		assert.Contains(t, out, "LAND REFUSED queued=6", "the refusal is still named")
		text += out
	}
	assert.Equal(t, 1, strings.Count(text, "LAND JUDGMENT NOT WRITTEN: "), "said once:\n%s", text)
	assert.Contains(t, text, "the store is down")
	down.Lock()
	broken = false
	down.Unlock()
	out, _ := r.beat(nil)
	assert.NotContains(t, out, "LAND JUDGMENT NOT WRITTEN")
	assert.Len(t, r.judgments(), 1, "written on the first beat the store takes it")
}

// A server restarted under a refusal whose judgment is open does not raise it again;
// a cards-but-no-coordinator re-read clears nothing; and a second server's other
// refusal never closes the first's judgment, which is checked against the store, not
// a flag.
func TestARestartUnderAnOpenJudgmentRaisesNoSecond(t *testing.T) {
	t.Parallel()
	r := newRefusalRig(t)
	why := "the sprint row names no git_identity; run: nova-config apply"
	r.set(why)
	_, _ = r.beat(nil)
	require.Len(t, r.judgments(), 1)

	// the restart: a new server on the same store
	ta2 := newTestApp(t)
	ta2.a.backend = r.ta.a.backend
	r2 := newRefusalRigOn(t, ta2)
	r2.set(why)
	for range 3 {
		out, _ := r2.beat(nil)
		assert.Contains(t, out, "LAND REFUSED queued=6")
	}
	assert.Len(t, r.judgments(), 1, "the restarted server raises no second judgment")
	assert.Len(t, r.allJudgments(), 1)

	// a second server fenced with another reason: its judgment is its own
	r2.set("the merge queue's fence names another server; run: nova-sprint where")
	_, _ = r2.beat(nil)
	_, _ = r.beat(nil)
	js := r.judgments()
	assert.Len(t, js, 2, "each server's refusal has its judgment: %+v", js)

	// the first's judgment closed by another step: the first raises it again
	st, err := r.ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(t, err)
	_, err = st.Run(t.Context(), store.Step{Verb: "land", Plan: sprint.LandRefusalClose})
	require.NoError(t, err)
	require.Empty(t, r.judgments())
	_, _ = r.beat(nil)
	if js := r.judgments(); assert.Len(t, js, 1) {
		assert.Contains(t, js[0].Note.What, "git_identity", "checked against the store, not the flag")
	}

	// the cycle reads cards and a coordinator, land's own re-read none: nothing ran,
	// nothing is cleared
	r.mu.Lock()
	r.queue = func(read int) string {
		if read%2 == 0 {
			return ""
		}
		return "coordinator"
	}
	r.mu.Unlock()
	for range 3 {
		out, _ := r.beat(nil)
		assert.Contains(t, out, "LAND REFUSED queued=6", "a round that ran nothing clears nothing:\n%s", out)
	}
	assert.Len(t, r.judgments(), 1)
}

// allJudgments is every lander-refusal judgment ever written, open or closed.
func (r *refusalRig) allJudgments() []sprint.Note {
	r.t.Helper()
	return r.allJudgmentsOf(sprint.NLanderRefuses)
}

// allJudgmentsOf is every judgment of one type ever written, open or closed.
func (r *refusalRig) allJudgmentsOf(typ string) []sprint.Note {
	r.t.Helper()
	st, err := r.ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(r.t, err)
	notes, _, err := st.B.NotesSince(r.t.Context(), "", 100000)
	require.NoError(r.t, err)
	var got []sprint.Note
	for _, n := range notes {
		if n.Kind == sprint.Judgment && n.Type == typ {
			got = append(got, n)
		}
	}
	return got
}

// A queue that cannot be read is a failure, never worded as a refusal: LAND FAILED
// every beat, one "the lander fails every landing" judgment by the machine (the queue
// names no coordinator) whose text carries no error words, however the error's words
// change read to read; the first read of the queue clears it.
func TestAnUnreadableQueueIsOneFailureByTheMachine(t *testing.T) {
	t.Parallel()
	r := newRefusalRig(t)
	reads := 0
	b := r.ta.a.landState()
	b.mu.Lock()
	b.queueSeam = func(context.Context, string) (int, string, string, error) {
		reads++
		if reads <= 8 {
			return 0, "", "", fmt.Errorf("dial tcp 10.0.0.%d:6390: i/o timeout after %dms", reads, 100*reads)
		}
		return 0, "coordinator", "", nil
	}
	b.mu.Unlock()
	for i := range 4 {
		out, _ := r.beat(nil)
		m := regexp.MustCompile(`LAND FAILED queued=\? since=(\S+) reason=(.*) run: (.*)`).FindStringSubmatch(out)
		if assert.NotNil(t, m, "beat %d:\n%s", i+1, out) {
			assert.Equal(t, (time.Duration(i) * LandEvery).String(), m[1], "beat %d: since grows", i+1)
			assert.Equal(t, landQueueUnread, m[2])
		}
		assert.NotContains(t, out, "LAND REFUSED", "beat %d: a failure is not a refusal", i+1)
		js := r.judgmentsOf(sprint.NLanderFails)
		if assert.Len(t, js, 1, "beat %d: one failure judgment", i+1) {
			assert.Equal(t, sprint.MachineActor, js[0].Note.Who, "by the machine")
			assert.NotContains(t, js[0].Note.What, "i/o timeout", "no error words in the judgment")
		}
		assert.Empty(t, r.judgments(), "no refusal judgment")
	}
	assert.Len(t, r.allJudgmentsOf(sprint.NLanderFails), 1)
	for range 4 {
		_, _ = r.beat(nil)
	}
	out, _ := r.beat(nil)
	assert.Contains(t, out, "LAND IDLE queued=0", "the queue is read: the failure is gone:\n%s", out)
	assert.Empty(t, r.judgmentsOf(sprint.NLanderFails), "its judgment is closed")
}

// A land that fails saying nothing still names a cause: "land exited <code> and said
// nothing", a failure; and a refusal that stands while the queue empties is still
// named, queued=0.
func TestASilentFailureAndAnEmptyQueueStillNameTheCause(t *testing.T) {
	t.Parallel()
	r := newRefusalRig(t)
	r.mu.Lock()
	r.pass = func(int, io.Writer, io.Writer) int { return 2 }
	r.mu.Unlock()
	out, _ := r.beat(nil)
	assert.Regexp(t, `LAND FAILED queued=6 since=0s reason=land exited 2 and said nothing run: nova-sprint land --dry-run`, out)
	if js := r.judgmentsOf(sprint.NLanderFails); assert.Len(t, js, 1) {
		assert.Equal(t, "land exited 2 and said nothing; run: nova-sprint land --dry-run", js[0].Note.What)
	}
	r.mu.Lock()
	r.pass = nil
	r.mu.Unlock()
	r.set("the sprint row names no git_identity; run: nova-config apply")
	_, _ = r.beat(nil)
	require.Len(t, r.judgments(), 1)
	assert.Empty(t, r.judgmentsOf(sprint.NLanderFails), "the failure's judgment was closed by the refusal that replaced it")
	b := r.ta.a.landState()
	b.mu.Lock()
	b.queueSeam = func(context.Context, string) (int, string, string, error) { return 0, "coordinator", "", nil }
	b.mu.Unlock()
	out, _ = r.beat(nil)
	assert.Regexp(t, `LAND REFUSED queued=0 since=2s reason=the sprint row names no git_identity run: nova-config apply`, out)
	assert.NotContains(t, out, "LAND IDLE")
	assert.Len(t, r.judgments(), 1, "nothing ran, nothing is cleared")
}
