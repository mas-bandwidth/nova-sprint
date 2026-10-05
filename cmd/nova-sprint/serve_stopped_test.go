package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprintwire"
)

// A verb sent to the server is answered within ServeWait of being read, the machine
// STOPPED or RUNNING, whatever holds the line of control (docs/SPEC-SPRINT.md section 14,
// The server). Measured 2026-10-04 12:54 PM ET: with the machine STOPPED, POST /verbs on
// the loopback listener went unanswered until the client's 8 s timeout. The clock a batch
// waits on is the test's: nothing here waits on the wall clock or opens a socket.

// serveClock is the clock a batch waits for the line on: each wait asked is sent on asked,
// and runs out when the test sends on fire.
type serveClock struct {
	asked chan time.Duration
	fire  chan time.Time
}

func newServeClock(a *app) *serveClock {
	c := &serveClock{asked: make(chan time.Duration, 16), fire: make(chan time.Time)}
	a.after = func(d time.Duration) <-chan time.Time {
		c.asked <- d
		return c.fire
	}
	return c
}

// servedSprint is a sprint of three cards dealt to m1 and m2, with the friend amy beating,
// served from mem:0: the server's own store.
func servedSprint(t *testing.T) *testApp {
	t.Helper()
	ta, _ := friendApp(t, "amy")
	ta.a.serveAddr = "mem:0"
	ta.ok("friend sync --root " + t.TempDir())
	ta.ok("friend beat amy")
	ta.ok("add --stream s1 --count 3")
	ta.ok("start")
	ta.ok("tick")
	ta.ok("tick")
	return ta
}

// readsAndBeats are the verbs a STOPPED machine answers as a RUNNING one does, each with
// the token its answer begins its last line with: the coordinator's reads, a worker's queue,
// and the beats.
var readsAndBeats = []struct {
	argv []string
	ok   string
}{
	{[]string{"where"}, "SPRINT TABLE"},
	{[]string{"card", "s1-1"}, "CARD OK id=s1-1"},
	{[]string{"queue", "--as", "m1"}, "QUEUE OK cards=2"},
	{[]string{"inbox", "--read", "--actor", "coordinator"}, "INBOX"},
	{[]string{"needs"}, "NEEDS OK"},
	{[]string{"log"}, "LOG OK"},
	{[]string{"friend", "beat", "amy", "--working", "1"}, "FRIEND-BEAT OK amy"},
	{[]string{"fleet", "beat", "m1", "--load", "5"}, "FLEET-BEAT OK m1"},
}

// argvsOf is the verbs of a list, in its order.
func argvsOf(list []struct {
	argv []string
	ok   string
}) [][]string {
	var out [][]string
	for _, v := range list {
		out = append(out, v.argv)
	}
	return out
}

// partOf is a list cut in two by keep, each part in its order.
func partOf(list []struct {
	argv []string
	ok   string
}, keep func([]string) bool) (in, out []struct {
	argv []string
	ok   string
}) {
	for _, v := range list {
		if keep(v.argv) {
			in = append(in, v)
		} else {
			out = append(out, v)
		}
	}
	return in, out
}

// served is the server's step in its own goroutine: its answer arrives on the channel.
func served(a *app, verbs [][]string, local bool) <-chan sprintwire.Response {
	got := make(chan sprintwire.Response, 1)
	go func() { got <- a.serveFrom(sprintwire.Request{Verbs: verbs}, local) }()
	return got
}

// logLines is how many lines the sprint's log holds, read on the store itself.
func logLines(t *testing.T, ta *testApp) int {
	t.Helper()
	var out, errb bytes.Buffer
	require.Equal(t, 0, ta.a.run([]string{"log", "--redis", "mem:0"}, &out, &errb), errb.String())
	return strings.Count(out.String(), "\n")
}

// With the machine STOPPED the server answers every read and every beat as it does while
// RUNNING, and a worker's write answers at once as it does on a STOPPED machine, with no
// wait at all while the line is free. While the stopped machine's tick holds the line, the
// reads and the beats still answer at once on their lanes, and the verbs that take the line
// are answered within ServeWait on the server's clock, each busy, naming the tick, having
// run nothing; once the line is free the whole batch is answered.
func TestAStoppedMachineStillAnswersReadsAndBeats(t *testing.T) {
	t.Parallel()
	ta := servedSprint(t)
	ta.ok("stop --reason r --until 9999h")
	clock := newServeClock(ta.a)
	began := ta.a.now()

	// the line free: every verb runs, each as the verb run on the store answers
	for _, local := range []bool{true, false} {
		for _, v := range readsAndBeats {
			if !local && v.argv[0] != "queue" && v.argv[0] != "friend" && v.argv[0] != "fleet" {
				continue // the workers' listener runs a worker's verbs only
			}
			res := ta.a.serveFrom(sprintwire.Request{Verbs: [][]string{v.argv}}, local)
			require.Len(t, res.Results, 1)
			assert.Equal(t, 0, res.Results[0].Code, "%v local=%v: %s%s", v.argv, local, res.Results[0].Stdout, res.Results[0].Stderr)
			assert.Contains(t, res.Results[0].Stdout, v.ok, "%v local=%v", v.argv, local)
		}
	}
	take := ta.a.serveFrom(sprintwire.Request{Verbs: [][]string{{"take", "--as", "m1", "--max", "1", "--epoch", "0"}}}, false).Results[0]
	assert.Equal(t, 0, take.Code, take.Stderr)
	assert.Contains(t, take.Stdout, "TAKE OK moved=1", "a take on a STOPPED machine moves its card, as the verb does")
	assert.Contains(t, take.Stdout, "STOPPED")
	assert.Empty(t, clock.asked, "a batch that found the line free waited on no clock")
	assert.Equal(t, began, ta.a.now(), "no verb waited on the server's clock")

	// the stopped machine's tick holds the line: the reads and the beats run on their lanes
	// and answer at once, on no clock; the verbs that take the line are answered at its bound
	before := logLines(t, ta)
	ta.a.serial.LockAs("the tick begun at " + began.Format("15:04:05"))
	lane, line := partOf(readsAndBeats, func(argv []string) bool {
		return argv[0] != "queue" && argv[0] != "fleet" && (argv[0] != "inbox" || !slices.Contains(argv, "--read"))
	})
	res := ta.a.serveFrom(sprintwire.Request{Verbs: argvsOf(lane)}, true)
	require.Len(t, res.Results, len(lane))
	for i, r := range res.Results {
		assert.Equal(t, 0, r.Code, "%v on its lane: %s", lane[i].argv, r.Stderr)
		assert.Contains(t, r.Stdout, lane[i].ok, "%v on its lane", lane[i].argv)
	}
	assert.Empty(t, clock.asked, "a read or a beat waited on no clock")
	got := served(ta.a, argvsOf(line), true)
	require.Equal(t, ServeWait, <-clock.asked, "the batch waits at most ServeWait")
	require.LessOrEqual(t, ServeWait, time.Second, "a verb is answered within a second of being read")
	clock.fire <- began.Add(ServeWait)
	res = <-got
	require.Len(t, res.Results, len(line))
	for i, r := range res.Results {
		assert.Equal(t, 2, r.Code, "%v", line[i].argv)
		assert.Contains(t, r.Stderr, "busy: the tick begun at 03:04:05 held the line of control past 1s; nothing was run or changed; send it again", "%v", line[i].argv)
		assert.Empty(t, r.Stdout)
	}
	ta.a.serial.Unlock()
	assert.Equal(t, before, logLines(t, ta), "a busy batch wrote nothing")

	// the line free again: the same batch answered whole
	res = ta.a.serveFrom(sprintwire.Request{Verbs: argvsOf(readsAndBeats)}, true)
	for i, r := range res.Results {
		assert.Equal(t, 0, r.Code, "%v: %s", readsAndBeats[i].argv, r.Stderr)
		assert.Contains(t, r.Stdout, readsAndBeats[i].ok)
	}
}

// A tick of the run loop in progress, held inside its store call, never holds a worker's
// verb past ServeWait: the batch is answered busy, naming the tick, without touching the
// store the tick holds, and the tick finishes as it began. After it, the verb runs.
func TestATickInProgressNeverHoldsAVerbPastItsBound(t *testing.T) {
	t.Parallel()
	ta := servedSprint(t)
	st, err := ta.a.store(common{redis: "mem:0", actor: sprint.MachineActor})
	require.NoError(t, err)
	clock := newServeClock(ta.a)

	// the tick blocks in its first fenced read of the store until the test lets it go
	in, out := make(chan struct{}), make(chan struct{})
	var once sync.Once
	ta.m.Fail = func(point string) error {
		if point == "fence" {
			once.Do(func() {
				close(in)
				<-out
			})
		}
		return nil
	}
	var stdout, stderr bytes.Buffer
	looped := make(chan bool, 1)
	go func() { looped <- ta.a.runLoop(context.Background(), st, 0, 1, &stdout, &stderr) }()
	<-in

	batch := [][]string{{"queue", "--as", "m1"}, {"friend", "beat", "amy"}, {"fleet", "beat", "m1", "--load", "5"}}
	got := served(ta.a, batch, false)
	require.Equal(t, ServeWait, <-clock.asked)
	clock.fire <- ta.a.now().Add(ServeWait)
	res := <-got
	require.Len(t, res.Results, len(batch))
	for i, r := range res.Results {
		assert.Equal(t, 2, r.Code, "%v", batch[i])
		assert.Contains(t, r.Stderr, "busy: the tick begun at 03:04:05 held the line", "%v", batch[i])
	}

	close(out)
	assert.False(t, <-looped)
	assert.NotContains(t, stderr.String(), "FAIL")
	res = ta.a.serveFrom(sprintwire.Request{Verbs: batch}, false)
	for i, r := range res.Results {
		assert.Equal(t, 0, r.Code, "%v: %s", batch[i], r.Stderr)
	}
}
