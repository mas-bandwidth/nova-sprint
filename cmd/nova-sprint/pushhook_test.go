package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus/bustest"
)

// The seat's push loop holds the seat's hook (pushhook.go; tla/SeatHookHolder.tla). These
// tests run the loop on the in-memory store with a fake clock, the hook's stream a pipe
// to the test's own server (hookDial), the session's socket a listener that is no socket
// (hookListen), the bus an in-memory fake, and the loop's beat a channel the test fires,
// each inside a testing/synctest bubble, whose clock is fake: the waits below (eventually,
// stop, run) sleep on the bubble's clock, which moves only when every goroutine of the
// test is blocked. No real time, no socket, no network (internal/ci, the unit rule).

// fakeListener is the holder's listener in a test: it accepts nothing and ends on Close.
type fakeListener struct {
	done chan struct{}
	once sync.Once
}

func (l *fakeListener) Accept() (net.Conn, error) { <-l.done; return nil, net.ErrClosed }
func (l *fakeListener) Close() error              { l.once.Do(func() { close(l.done) }); return nil }
func (l *fakeListener) Addr() net.Addr            { return fakeAddr{} }

type fakeAddr struct{}

func (fakeAddr) Network() string { return "fake" }
func (fakeAddr) String() string  { return "fake.sock" }

// pushHookRig is a sprint whose coordinator holds the seat, with a home of its own (the
// holder's inbox under it), the hook's gate armed for the coordinator, and the seams the
// push loop's hold needs. The sprint has one card dealt to m1 and the machine running.
type pushHookRig struct {
	ta      *testApp
	name    string
	inbox   string
	beat    chan time.Time
	mu      sync.Mutex
	server  net.Conn    // the server's end of the latest hook stream
	stopped bool        // stopLoop read the loop's end; else the cleanup ends it
	done    chan string // the running loop's end (runLoop): a loop that ends early is said
	log     lockedLog   // the loop's lines, stdout and stderr together
	dials   int         // the hook streams opened (hookDial)
}

// lockedLog is a buffer two goroutines write (the loop's stdout and stderr) and the test reads.
type lockedLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

// text is the loop's log so far.
func (l *lockedLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func newPushHookRig(t *testing.T) *pushHookRig {
	t.Helper()
	const name = "coordinator"
	ta := newTestApp(t)
	env := map[string]string{"NOVA_SPRINT_REDIS": "mem:0", "NOVA_SPRINT_ACTOR": name, busRedisEnv: "bus:test"}
	var emu sync.Mutex
	ta.a.getenv = func(k string) string { emu.Lock(); defer emu.Unlock(); return env[k] }
	ta.homeOfItsOwn()
	// the home the loop's inbox is under (seatInbox reads a.home, os.UserHomeDir in the
	// binary): the test's own, never the machine's
	home := t.TempDir()
	ta.a.home = func() (string, error) { return home, nil }
	inbox := filepath.Join(home, name+"-working", "inbox", "sprint-judgments")
	require.NoError(t, os.MkdirAll(inbox, 0o755))
	// the sprint, set up before the gate is armed
	ta.ok("init --readers reader-a,reader-b --members m1,m2 --owner glenn")
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
	ta.ok("start")
	// the gate, the bus, the record's write signal (hook_test.go)
	fake := bustest.NewFake(t0, name, "glenn")
	// a read that finds nothing waits a little of the bubble's clock, never spins, and
	// wakes at once when the test is over: a bubble's clock stops when its test returns,
	// so a reader left asleep would hold the bubble open
	over, done := context.WithCancel(context.Background())
	t.Cleanup(done)
	fake.Sleep = func(time.Duration) {
		select {
		case <-over.Done():
		case <-time.After(5 * time.Millisecond):
		}
	}
	ta.a.busOpen = func(context.Context, string, string) (*bus.Bus, func(), error) {
		return &bus.Bus{Store: fake}, func() {}, nil
	}
	hookTests.Store(name, true)
	t.Cleanup(func() { hookTests.Delete(name) })
	hs := &hookSync{writes: make(chan struct{}, 64)}
	hookSyncs.Store(ta, hs)
	t.Cleanup(func() { hookSyncs.Delete(ta) })
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) {
		return &hookSignalKV{Mem: ta.m, writes: hs.writes}, nil
	}
	ta.a.serveAddr = "mem:0"
	// a server refreshes its copies of the bus push records off the line for its whole
	// life (hookerRefresh); a goroutine that never ends cannot live in a synctest bubble,
	// so the refresh is marked as already running and the gate reads the store itself
	ta.a.hk.refilling = true
	// the server the hook is held on: named now, after the setup verbs ran on the store;
	// this process is the server, so every verb runs here and only the hook's stream
	// (hookDial) is dialled
	emu.Lock()
	env[ServerEnv] = "127.0.0.1:1"
	emu.Unlock()
	// the app's sleep (the store's tick-end poll sleeps on it every 100 ms of its wait,
	// store.WaitTickEnd) steps the sprint's clock a hundredth of the way and blocks a
	// millisecond of the bubble's clock: a durable block, so the bubble's time moves (a
	// sleep that only steps a clock is a spin, and a spinning goroutine freezes a synctest
	// clock); the sprint's clock moves slowly enough that a proven hook stays live (15 min)
	// through a test
	ta.a.sleep = func(d time.Duration) {
		ta.mu.Lock()
		ta.now = ta.now.Add(max(d/100, time.Millisecond))
		ta.mu.Unlock()
		ta.beat()
		time.Sleep(time.Millisecond)
	}
	hookHolders.Delete(name)
	r := &pushHookRig{ta: ta, name: name, inbox: inbox, beat: make(chan time.Time)}
	ta.a.after = func(time.Duration) <-chan time.Time { return r.beat }
	ta.a.hookListen = func(string) (net.Listener, string, error) {
		return &fakeListener{done: make(chan struct{})}, "fake.sock", nil
	}
	ta.a.hookDial = func(ctx context.Context, _, name string, _ bool) (io.ReadWriteCloser, error) {
		server, client := net.Pipe()
		r.mu.Lock()
		r.server = server
		r.dials++
		r.mu.Unlock()
		go ta.a.hookRun(context.Background(), name, server, 0)
		return client, nil
	}
	return r
}

// run runs the push loop until the interrupt; its output comes back on done.
func runLoop(t *testing.T, r *pushHookRig) (in *interrupt, done chan string) {
	t.Helper()
	in = r.ta.interruptible()
	done = make(chan string, 1)
	go func() {
		// --redis: the store here, and no test-home scrub of a verb that names a server
		// (runInto); the loop's lines go to the rig's log, readable while it runs
		r.ta.beat()
		code := runInto(r.ta.a, r.ta.withEpoch(split("inbox --wait --push seat --redis mem:0 --timeout 1m")), &r.log, &r.log)
		r.log.mu.Lock()
		out := r.log.b.String()
		r.log.mu.Unlock()
		if code != 0 {
			out += "\nEXIT " + string(rune('0'+code))
		}
		done <- out
	}()
	// a test that fails leaves the loop running: the cleanup ends it, so the bubble ends
	// with every goroutine gone and the failure is said, not a bubble deadlock; the
	// loop's log is said with the failure
	r.stopped, r.done = false, done
	t.Cleanup(func() {
		if !r.stopped {
			in.now(t)
			t.Logf("the push loop's log:\n%s", <-done)
		}
	})
	return in, done
}

// stop interrupts the loop and returns its output.
func stopLoop(t *testing.T, r *pushHookRig, in *interrupt, done chan string) string {
	t.Helper()
	in.now(t)
	r.stopped = true
	select {
	case out := <-done:
		return out
	case <-time.After(10 * time.Second):
		t.Fatal("the push loop did not end on the interrupt")
		return ""
	}
}

// eventually waits for cond, waking every few milliseconds of the bubble's clock, for
// at most a bound; a push loop that ends meanwhile (refused, or failed) is the failure.
func eventually(t *testing.T, r *pushHookRig, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); {
		if cond() {
			return
		}
		if r != nil && r.done != nil {
			select {
			case out := <-r.done:
				r.stopped = true
				t.Fatalf("the push loop ended while waiting for %s:\n%s", what, out)
			default:
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("never: %s", what)
}

// told is how many messages the server sent the owner (the end of a hook is one).
func (r *pushHookRig) told() int {
	r.ta.mu.Lock()
	defer r.ta.mu.Unlock()
	return len(r.ta.sent)
}

// holder is the loop's holder of the moment.
func holderOf(t *testing.T, r *pushHookRig) *hookHolder {
	t.Helper()
	var h *hookHolder
	eventually(t, r, "the loop made its holder", func() bool {
		v, ok := hookHolders.Load(r.name)
		if ok {
			h = v.(*hookHolder)
		}
		return ok
	})
	return h
}

var challengeRe = regexp.MustCompile(`HOOK CHALLENGE ([0-9a-f]+\.[0-9a-f]+): `)

// challengeFile waits for a challenge delivered into the inbox and returns it, and the
// file it came in.
func challengeOf(t *testing.T, r *pushHookRig, notIn map[string]bool) (string, string) {
	t.Helper()
	var chal, file string
	eventually(t, r, "a challenge in the inbox", func() bool {
		entries, err := os.ReadDir(r.inbox)
		if err != nil {
			return false
		}
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "hook-") || notIn[e.Name()] {
				continue
			}
			b, err := os.ReadFile(filepath.Join(r.inbox, e.Name()))
			if err != nil {
				continue
			}
			if m := challengeRe.FindStringSubmatch(string(b)); m != nil {
				chal, file = m[1], e.Name()
				return true
			}
		}
		return false
	})
	return chal, file
}

// The push loop holds the seat's hook: it connects as the holder, the challenge is
// delivered into the holder's inbox (never answered by the loop), the session answers it
// over the loop's socket, and from then every coordinator verb runs; a judgment pushed
// over the stream is acknowledged by the loop only once its file is in the inbox.
func TestThePushLoopHoldsTheSeatsHookAndTheSessionProvesItFromItsInbox(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newPushHookRig(t)
		ta := r.ta
		ctx := context.Background()
		in, done := runLoop(t, r)

		rec := ta.waitHook(sprint.HookHooked)
		assert.True(t, rec.Proven.IsZero(), "the loop proved the hook itself: %+v", rec)
		chal, file := challengeOf(t, r, nil)
		text, _ := os.ReadFile(filepath.Join(r.inbox, file))
		assert.Contains(t, string(text), "nova-sprint hook --prove "+chal+" --actor "+r.name, "the file carries the answer:\n%s", text)

		// unproven: the holder's verbs are refused, with the breadcrumb
		ta.refusedFirst("add --stream s2 --count 1 --one", sprint.HookFirst)

		// the session answers from the inbox, through the loop's socket
		h := holderOf(t, r)
		ans, err := h.take(ctx, hookMsg{T: "prove", Challenge: "nope"})
		require.NoError(t, err)
		assert.Equal(t, "refused", ans.T, "%+v", ans)
		ans, err = h.take(ctx, hookMsg{T: "prove", Challenge: chal})
		require.NoError(t, err)
		require.Equal(t, "ok", ans.T, "%+v", ans)
		rec = ta.waitHookWhere("", func(rec sprint.HookRecord) bool { return !rec.Proven.IsZero() }, "the record never said proven")
		state, _ := h.State()
		assert.Equal(t, hookOpen, state)

		// proven: the holder's verbs run
		code, out, errs := ta.doLined("add --stream s2 --count 1 --one")
		require.Equal(t, 0, code, out+errs)

		// a judgment: pushed over the stream by the server's look, written into the inbox by
		// the loop, then acknowledged by the loop, and not before
		ta.failOnce("m1", "s1-1.w1@1", "tests red")
		ta.ok("tick")
		s := ta.hookSession()
		require.NotNil(t, s)
		s.look(ctx)
		eventually(t, r, "a push pending on the holder", func() bool { return len(h.Pending()) > 0 })
		fresh := ta.group(sprint.NWorkFailed, "s1")
		eventually(t, r, "the judgment's file in the inbox", func() bool {
			_, err := os.Stat(filepath.Join(r.inbox, fresh.Notes[0]+".md"))
			return err == nil
		})
		eventually(t, r, "the push acknowledged once its file is written", func() bool { return len(h.Pending()) == 0 })
		rec, ok := ta.hookRecord()
		require.True(t, ok)
		assert.Equal(t, 0, rec.Unacked, "%+v", rec)

		logs := stopLoop(t, r, in, done)
		assert.Contains(t, logs, "HOOK CONNECTED name="+r.name+" role=coordinator", logs)
		assert.Contains(t, logs, "HOOK CHALLENGE "+chal+" delivered=", logs)
		assert.Contains(t, logs, "HOOK PROVEN name="+r.name, logs)
		assert.Regexp(t, "HOOK ACK OK name="+r.name+` through=\d+ unacked=0: delivered into the inbox`, logs)
	})
}

// A push whose judgment is not in the inbox is never acknowledged: a loop that cannot
// deliver is found by the ack bound, not hidden by its own acknowledgement (the reversed
// witness of the one above).
func TestTheLoopAcknowledgesNoPushWhoseJudgmentIsNotInTheInbox(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &hookHolder{name: "x", pending: []hookPush{{ID: 1, What: "g1"}, {ID: 2, What: "g2"}, {ID: 3, What: "g3"}}}
		// no connection: nothing is acknowledged
		assert.Equal(t, "", h.delivered(context.Background(), func(string) bool { return true }))
		assert.Len(t, h.Pending(), 3)
		// the second is not written: the first is acknowledged, the rest held back behind it
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()
		h.relay = &hookRelay{w: client, pending: map[string]chan hookMsg{}}
		answers := make(chan hookMsg, 1)
		go func() {
			// the server's side: the ack line, answered ok
			buf := make([]byte, 4096)
			n, _ := server.Read(buf)
			var m hookMsg
			if json.Unmarshal(buf[:n], &m) != nil {
				return
			}
			answers <- m
			reply := hookMsg{T: "ok", Req: m.Req, Of: "ack", ID: m.ID}
			b, _ := json.Marshal(reply)
			_, _ = server.Write(append(b, '\n'))
		}()
		// the relay's answer is read by the holder's connection loop in the binary; here the
		// test reads it for the relay
		go func() {
			buf := make([]byte, 4096)
			n, err := client.Read(buf)
			if err == nil {
				var m hookMsg
				if json.Unmarshal(buf[:n], &m) == nil {
					h.relay.answered(m)
				}
			}
		}()
		line := h.delivered(context.Background(), func(what string) bool { return what != "g2" })
		assert.Contains(t, line, "through=1", line)
		asked := <-answers
		assert.Equal(t, uint64(1), asked.ID, "the ack went past an unwritten judgment: %+v", asked)
		assert.Equal(t, []hookPush{{ID: 2, What: "g2"}, {ID: 3, What: "g3"}}, h.Pending())
	})
}

// An unhook stands across the loop's restart: the seat's word to stop ends the hold, a
// loop started again finds the record away and holds nothing, and only the seat's word
// to hook in again (nova-sprint hook, over the loop's socket) opens a connection.
func TestAnUnhookStandsAcrossTheLoopsRestartUntilTheSeatsWord(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newPushHookRig(t)
		ta := r.ta
		ctx := context.Background()
		in, done := runLoop(t, r)
		ta.waitHook(sprint.HookHooked)
		h := holderOf(t, r)
		ans, err := h.take(ctx, hookMsg{T: "unhook"})
		require.NoError(t, err)
		require.Equal(t, "ok", ans.T, "%+v", ans)
		ta.waitHook(sprint.HookAway)
		eventually(t, r, "the holder away", func() bool { s, _ := h.State(); return s == hookAway })
		// the beat passes: the loop opens nothing on an unhooked record
		r.beat <- time.Time{}
		eventually(t, r, "the loop still away after a beat", func() bool { s, _ := h.State(); return s == hookAway })
		rec, _ := ta.hookRecord()
		assert.Equal(t, sprint.HookAway, rec.State)
		logs := stopLoop(t, r, in, done)
		assert.Contains(t, logs, "HOOK AWAY name="+r.name, logs)

		// the loop restarts: its memory is gone, the record is not
		in, done = runLoop(t, r)
		eventually(t, r, "a new holder", func() bool { v, ok := hookHolders.Load(r.name); return ok && v.(*hookHolder) != h })
		h = holderOf(t, r)
		r.beat <- time.Time{}
		eventually(t, r, "the restarted loop read away", func() bool { s, _ := h.State(); return s == hookAway })
		rec, _ = ta.hookRecord()
		assert.Equal(t, sprint.HookAway, rec.State, "the restarted loop re-armed an unhooked record: %+v", rec)
		assert.Nil(t, ta.hookSession(), "a subscription is open after unhook")

		// the seat's word: hook in again now
		ans, err = h.take(ctx, hookMsg{T: "hook"})
		require.NoError(t, err)
		require.Equal(t, "ok", ans.T, "%+v", ans)
		assert.Equal(t, hookAway, ans.Why)
		ta.waitHook(sprint.HookHooked)
		logs = stopLoop(t, r, in, done)
		assert.Contains(t, logs, "HOOK AWAY name="+r.name, logs)
		assert.Contains(t, logs, "HOOK CONNECTED name="+r.name, logs)
	})
}

// A challenge unanswered past its bound leaves the loop down until the seat's word: the
// owner is told once, not every bound; the seat's word opens a connection at once.
func TestAMissedChallengeLeavesTheLoopDownUntilTheSeatsWord(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newPushHookRig(t)
		ta := r.ta
		ctx := context.Background()
		in, done := runLoop(t, r)
		ta.waitHook(sprint.HookHooked)
		h := holderOf(t, r)
		challengeOf(t, r, nil)
		told := r.told()
		// the bound passes with no answer: the server's look records the hook gone
		ta.mu.Lock()
		ta.now = ta.now.Add(sprint.HookAnswerBound + time.Minute)
		ta.mu.Unlock()
		s := ta.hookSession()
		require.NotNil(t, s)
		s.look(ctx)
		rec := ta.waitHook(sprint.HookGone)
		assert.True(t, strings.HasPrefix(rec.Why, hookMissWhy), rec.Why)
		eventually(t, r, "the holder missed", func() bool { s, _ := h.State(); return s == hookMissed })
		// beats pass: no new connection, the owner told once
		r.beat <- time.Time{}
		r.beat <- time.Time{}
		eventually(t, r, "still missed after two beats", func() bool { s, _ := h.State(); return s == hookMissed })
		assert.Nil(t, ta.hookSession(), "the loop opened a connection after a missed challenge")
		assert.Equal(t, told+1, r.told(), "the owner was told other than once")
		// the seat's word
		ans, err := h.take(ctx, hookMsg{T: "hook"})
		require.NoError(t, err)
		require.Equal(t, "ok", ans.T, "%+v", ans)
		ta.waitHookWhere("", func(rec sprint.HookRecord) bool { return rec.State == sprint.HookHooked }, "the seat's word opened no connection")
		logs := stopLoop(t, r, in, done)
		assert.Contains(t, logs, "HOOK MISSED name="+r.name, logs)
	})
}

// A connection that drops (the server restarts, the stream breaks) is opened again at
// the loop's next beat, with a fresh challenge into the inbox.
func TestADroppedConnectionIsOpenedAgainAtTheNextBeat(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newPushHookRig(t)
		ta := r.ta
		in, done := runLoop(t, r)
		first := ta.waitHook(sprint.HookHooked)
		h := holderOf(t, r)
		_, file := challengeOf(t, r, nil)
		r.mu.Lock()
		server := r.server
		r.mu.Unlock()
		require.NoError(t, server.Close())
		ta.waitHook(sprint.HookGone)
		eventually(t, r, "the holder down", func() bool { s, _ := h.State(); return s == hookDown })
		r.beat <- time.Time{}
		second := ta.waitNewSession(first.Session)
		assert.Equal(t, sprint.HookHooked, second.State)
		_, file2 := challengeOf(t, r, map[string]bool{file: true})
		assert.NotEqual(t, file, file2, "a fresh challenge is delivered for the new connection")
		logs := stopLoop(t, r, in, done)
		assert.Contains(t, logs, "HOOK DROPPED name="+r.name, logs)
		assert.Equal(t, 2, strings.Count(logs, "HOOK CONNECTED name="+r.name), logs)
	})
}

// The seat's word to a Monitor's hook is refused (one Monitor holds the seat); to the
// loop's it is taken, and while a connection is open it is nothing more than the state.
func TestTheSeatsWordIsTheLoopHoldersAlone(t *testing.T) {
	ctx := context.Background()
	monitor := &hookHolder{name: "m", loop: false, state: hookOpen, word: make(chan struct{}, 1)}
	ans, err := monitor.take(ctx, hookMsg{T: "hook"})
	require.NoError(t, err)
	assert.Equal(t, "refused", ans.T)
	assert.Contains(t, ans.Why, "a hook is already running on this machine for m")
	assert.Len(t, monitor.word, 0)

	loop := &hookHolder{name: "l", loop: true, state: hookMissed, word: make(chan struct{}, 1)}
	ans, err = loop.take(ctx, hookMsg{T: "hook"})
	require.NoError(t, err)
	assert.Equal(t, "ok", ans.T)
	assert.Equal(t, hookMissed, ans.Why)
	assert.Len(t, loop.word, 1, "the word was not passed to the loop")
	<-loop.word
	loop.state = hookOpen
	ans, _ = loop.take(ctx, hookMsg{T: "hook"})
	assert.Equal(t, "ok", ans.T)
	assert.Len(t, loop.word, 0, "an open connection is not asked for again")

	// a line for the server with no connection open is refused, naming the state and the word
	ans, err = loop.take(ctx, hookMsg{T: "prove", Challenge: "c"})
	require.NoError(t, err)
	assert.Equal(t, "refused", ans.T)
	assert.Contains(t, ans.Why, "nova-sprint hook --actor l")
}

// The push loop is the hook's holder, so it is exempt from the gate as hook is: inbox
// --wait --push, by its words; inbox alone is not.
func TestThePushLoopIsExemptFromTheGateByItsWords(t *testing.T) {
	assert.True(t, hookExemptCall("inbox", []string{"--wait", "--push", "seat"}))
	assert.True(t, hookExemptCall("inbox", []string{"--wait", "--push=seat", "--redis", "x"}))
	assert.False(t, hookExemptCall("inbox", []string{"--wait"}))
	assert.False(t, hookExemptCall("inbox", nil))
	assert.False(t, hookExemptCall("add", []string{"--push", "x"}))
}

// Without a bus store named the loop holds no hook, says so once, and pushes judgments
// all the same.
func TestWithoutABusStoreTheLoopHoldsNoHookAndSaysSo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newPushHookRig(t)
		ta := r.ta
		env := ta.a.getenv
		ta.a.getenv = func(k string) string {
			if k == busRedisEnv {
				return ""
			}
			return env(k)
		}
		in, done := runLoop(t, r)
		time.Sleep(50 * time.Millisecond) // the loop's first look and its one NOTE
		_, held := hookHolders.Load(r.name)
		assert.False(t, held, "a holder was made with no bus store")
		logs := stopLoop(t, r, in, done)
		assert.Contains(t, logs, "NOTE the seat's hook is not held by this loop: "+busRedisEnv+" is not set", logs)
		assert.Equal(t, 1, strings.Count(logs, "NOTE the seat's hook is not held"), logs)
		rec, ok := ta.hookRecord()
		assert.False(t, ok, "a record was written with no hook held: %+v", rec)
	})
}
