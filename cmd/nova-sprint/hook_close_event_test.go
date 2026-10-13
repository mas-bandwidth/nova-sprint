package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The hook's close event, pinned against the interface that exists today (Stella's
// v1.2.16 repair 3, closed-harness evidence: the recovery of a down friend's cards must
// never read busy as closed, so it wants an authoritative signal that a session CLOSED,
// never quiet alone). What the server records today, and within what bound, for each way
// a hook connection ends; and that a quiet-but-connected session is not closed. Fake
// clock (testApp.now), net.Pipe, no real time, no sockets.
//
// The shapes: the Claude shape is the seat's (Rowan's) hook, `nova-sprint hook` held by
// a child process of the Claude Code session; the session's end ends the process, whose
// socket closes cleanly (EOF at the server). The Codex shape is a friend's (Stella's)
// session: `nova-bus recv --forever` in the background and a daemon or `nova-hook hook`
// holding leases in the bus Redis; she holds no stream to the sprint server at all.

// pinClose is one way a hook connection ends and what the server records for it today:
// the name the test hooks as, how the end is caused, the record's state, the exact words
// of its why, and the bound in sprint clock within which it is written (zero: at the end
// itself, no clock step needed).
type pinClose struct {
	name  string
	as    string
	end   func(t *testing.T, c *hookClient, s *hookSession)
	state string
	why   string
	bound time.Duration
}

// A hook connection closed by the client is recorded at once, gone, with the reason in
// its why and the time of the end in Since (hookSession.read -> hookRun -> end); the
// owner is told once on the bus. Clean EOF (the Claude shape: the hook process ended with
// its session) and a transport error (a reset) alike; unhook is away, the same way.
// Bound: the end itself; no sprint clock passes.
func TestHookCloseEvidenceClientCloseIsRecordedGoneAtOnce(t *testing.T) {
	t.Parallel()
	for _, pc := range []pinClose{
		{name: "claude shape: clean close (the hook process ended with its session)", as: "hook-close-eof",
			end:   func(t *testing.T, c *hookClient, _ *hookSession) { require.NoError(t, c.conn.Close()) },
			state: sprint.HookGone, why: "the connection closed without nova-sprint unhook"},
		{name: "transport error: the read fails (a reset, not an EOF)", as: "hook-close-reset",
			end: func(t *testing.T, c *hookClient, s *hookSession) {
				s.conn.(*failingConn).fail(errors.New("read tcp 127.0.0.1:6390: connection reset by peer"))
				require.NoError(t, c.conn.Close())
			},
			state: sprint.HookGone, why: "the connection dropped: read tcp 127.0.0.1:6390: connection reset by peer"},
		{name: "unhook: the seat's own word", as: "hook-close-unhook",
			end: func(t *testing.T, c *hookClient, _ *hookSession) {
				require.Equal(t, "ok", c.say(hookMsg{T: "unhook"}).T)
				assert.Contains(t, c.next("bye").Why, "unhooked")
			},
			state: sprint.HookAway, why: "nova-sprint unhook"},
	} {
		t.Run(pc.name, func(t *testing.T) {
			t.Parallel()
			name := pc.as
			ta, _ := hookSprint(t, name)
			c, ended := ta.hookOnFailing(name)
			hello := c.next("hello")
			require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
			ta.busProven(name)
			before := ta.waitHook(sprint.HookHooked)
			require.Equal(t, hello.Session, before.Session)
			s := ta.hookSession()
			require.NotNil(t, s)
			at := ta.a.now()

			pc.end(t, c, s)

			rec := ta.waitHook(pc.state)
			assert.Equal(t, name, rec.Name)
			assert.Equal(t, hello.Session, rec.Session, "the close names the session that closed")
			assert.Equal(t, pc.why, rec.Why)
			assert.Equal(t, at.Add(pc.bound).UTC(), rec.Since.UTC(), "Since is the server's clock at the end, within the bound")
			assert.True(t, rec.Proven.IsZero() && rec.Live.IsZero(), "a closed hook is neither proven nor live")
			assert.False(t, sprint.HookLive(rec, true, name, ta.a.now()))
			<-ended // hookRun returns once end has written the record and told the owner
			assert.Equal(t, 1, ta.told("seat "+name+" is "+pc.state+" -> glenn"), "the owner is told once")
			ta.refusedFirst("where", sprint.HookFirst)
		})
	}
}

// A quiet-but-connected session is NOT closed: no act of any kind for one second short
// of sprint.HookSilentBound leaves the record hooked and live; after that, keepalives
// alone (the transport's word that the hook process holds the stream, no act of the seat)
// keep it hooked for another hour of sprint clock: nothing gone, no bye, the owner not
// told. The only quiet-based end the server has is the lapse at HookSilentBound with not
// even a keepalive, and that is a lapse (NHookLapsed), never a close.
func TestHookCloseEvidenceQuietButConnectedIsNotClosed(t *testing.T) {
	t.Parallel()
	const name = "hook-close-quiet"
	ta, _ := hookSprint(t, name)
	c := ta.hookOn(name)
	c.next("hello")
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
	ta.busProven(name)
	s := ta.hookSession()
	require.NotNil(t, s)

	// quiet: no verb, no ack, no keepalive, one second short of the bound
	ta.step(sprint.HookSilentBound - time.Second)
	s.look(context.Background())
	rec, ok := ta.hookRecord()
	require.True(t, ok)
	assert.Equal(t, sprint.HookHooked, rec.State, "quiet short of the bound is not closed")
	assert.Empty(t, rec.Why)
	assert.True(t, sprint.HookLive(rec, true, name, ta.a.now()))

	// connected and quiet of acts, with the transport's keepalive only, for an hour: each
	// keepalive is seen by the server (its acted moves to now) before the clock steps, so
	// the test's order is the stream's order and no look runs ahead of a line in flight
	for minute := 1; minute <= 60; minute++ {
		c.tell(hookMsg{T: "alive"})
		s.waitActed(ta.a.now())
		ta.step(time.Minute)
		s.look(context.Background())
		for _, m := range c.drained() {
			require.NotEqual(t, "bye", m.T, "minute %d: a connected hook was ended: %s", minute, m.Why)
			if m.T == "push" {
				require.Equal(t, "ok", c.say(hookMsg{T: "ack", ID: m.ID, Session: m.Session}).T)
			}
		}
		rec, _ = ta.hookRecord()
		require.Equal(t, sprint.HookHooked, rec.State, "minute %d: quiet-but-connected read as closed", minute)
		require.Empty(t, rec.Why, "minute %d", minute)
	}
	assert.Empty(t, lapsesOf(ta), "no lapse judgment for a connected hook")
	assert.Equal(t, 0, ta.told("seat "+name+" is gone -> glenn"), "the owner told of a close that did not happen")
}

// The Codex shape: a friend's session holds no stream to the sprint server, and the hook
// endpoint admits the seat's holder alone (hookHolderWhy). Her hook is refused before a
// session begins, and the server records nothing of her, open or closed: the seat-hook
// record is untouched and the owner is not told. So the sprint server has no close
// event for a friend today; the friend's only signals live in the bus Redis (the hook
// module's leases, LeaseTTL 90 s) and in her daemon's beat, both quiet-based.
func TestHookCloseEvidenceCodexShapeAFriendHasNoHookToClose(t *testing.T) {
	t.Parallel()
	const seat, friend = "hook-close-seat", "hook-close-friend"
	ta, _ := hookSprint(t, seat)
	c := ta.hookOn(seat)
	c.next("hello")
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
	ta.busProven(seat)
	before := ta.waitHook(sprint.HookHooked)

	f := ta.hookOn(friend)
	bye := f.next("bye")
	assert.Contains(t, bye.Why, "the hook is the seat's holder's")
	_, open := <-f.lines
	assert.False(t, open, "the friend's connection is closed by the server after the bye")

	after, ok := ta.hookRecord()
	require.True(t, ok)
	assert.Equal(t, before, after, "the seat's record is untouched by a friend's refused hook")
	assert.Equal(t, seat, after.Name)
	assert.Equal(t, 0, ta.told("seat "+friend+" is gone -> glenn"), "the owner told of a friend's hook")
	ta.lined(func() { ta.ok("where") })
}

// told is how many bus messages with subject -> to the test app's bus has sent.
func (ta *testApp) told(subject string) int {
	ta.mu.Lock()
	defer ta.mu.Unlock()
	n := 0
	for _, x := range subjects(ta) {
		if x == subject {
			n++
		}
	}
	return n
}

// waitActed yields until the session's last act is at or after at: a line the test wrote
// on the pipe has been read and counted by the server's read loop. No clock is read.
func (s *hookSession) waitActed(at time.Time) {
	for {
		s.mu.Lock()
		acted := s.acted
		s.mu.Unlock()
		if !acted.Before(at) {
			return
		}
		runtime.Gosched()
	}
}

// failingConn is the server's end of a pipe whose Read can be made to fail with an error
// that is not EOF: the transport error path of hookSession.read ("the connection
// dropped"). Until fail is called it reads the pipe as it is.
type failingConn struct {
	net.Conn
	mu  sync.Mutex
	err error
}

func (f *failingConn) fail(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

func (f *failingConn) Read(p []byte) (int, error) {
	n, err := f.Conn.Read(p)
	if err == nil {
		return n, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil && errors.Is(err, io.EOF) {
		return n, f.err
	}
	return n, err
}

// hookOnFailing is hookOn with the server's end wrapped in a failingConn, so a test can
// end the stream with a transport error as well as an EOF; ended is closed once the
// server's hookRun has returned (the end recorded, the owner told).
func (ta *testApp) hookOnFailing(name string) (c *hookClient, ended <-chan struct{}) {
	ta.t.Helper()
	server, client := net.Pipe()
	c = &hookClient{t: ta.t, conn: client, lines: make(chan hookMsg, 64)}
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(client)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			var m hookMsg
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				if m.T == "hello" {
					c.session = m.Session
				}
				c.lines <- m
			}
		}
		close(c.lines)
	}()
	go func() {
		ta.a.hookRun(context.Background(), name, &failingConn{Conn: server}, 0)
		close(done)
	}()
	ta.t.Cleanup(func() { _ = client.Close() })
	return c, done
}
