package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// shortSock is where a test's hook takes its session's lines: a directory of its own,
// short enough for a unix socket's path. It is made under the test's working directory,
// not os.TempDir, whose sandbox path can already overflow a unix socket's 108 bytes.
func shortSock(t *testing.T) func(string) (string, error) {
	dir, err := os.MkdirTemp(".", ".hk")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return func(name string) (string, error) { return filepath.Join(dir, name+".sock"), nil }
}

// hookSprint is a sprint whose coordinator is name, armed for the hook's gate, its
// owner glenn, and the server's store the twin.
func hookSprint(t *testing.T, name string) (*testApp, map[string]string) {
	t.Helper()
	ta := newTestApp(t)
	env := map[string]string{"NOVA_SPRINT_REDIS": "mem:0", "NOVA_SPRINT_ACTOR": name}
	var mu sync.Mutex
	ta.a.getenv = func(k string) string { mu.Lock(); defer mu.Unlock(); return env[k] }
	hookTests.Store(name, true)
	t.Cleanup(func() { hookTests.Delete(name) })
	ta.ok("init --readers reader-a,reader-b --members m1,m2 --owner glenn")
	ta.a.serveAddr = "mem:0"
	return ta, env
}

// lined runs f holding the server's line, as every verb the server runs does: the
// hook's server side writes on the same line, so a test's own reads take it too.
func (ta *testApp) lined(f func()) {
	ta.a.serial.Lock()
	defer ta.a.serial.Unlock()
	f()
}

func (ta *testApp) doLined(line string) (code int, out, errs string) {
	ta.lined(func() { code, out, errs = ta.do(line) })
	return
}

func (ta *testApp) hookRecord() (rec sprint.HookRecord, ok bool) {
	ta.t.Helper()
	ta.lined(func() {
		st, err := ta.a.store(common{redis: "mem:0"})
		require.NoError(ta.t, err)
		rec, ok, err = readHook(context.Background(), st)
		require.NoError(ta.t, err)
	})
	return
}

// refusedFirst wants the line refused with exactly the hook's line, exit 1, and nothing
// written.
func (ta *testApp) refusedFirst(line, want string) {
	ta.t.Helper()
	before := ta.applies()
	code, out, errs := ta.doLined(line)
	require.Equal(ta.t, 1, code, "%s: exit %d\n%s%s", line, code, out, errs)
	require.Equal(ta.t, want+"\n", errs, line)
	require.Empty(ta.t, out, line)
	require.Equal(ta.t, before, ta.applies(), "%s was refused and wrote", line)
}

// hookClient is the session's end of a subscription the test runs on a pipe.
type hookClient struct {
	t     *testing.T
	conn  net.Conn
	lines chan hookMsg
	n     int
}

func (ta *testApp) hookOn(name string) *hookClient {
	ta.t.Helper()
	server, client := net.Pipe()
	c := &hookClient{t: ta.t, conn: client, lines: make(chan hookMsg, 64)}
	go func() {
		sc := bufio.NewScanner(client)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			var m hookMsg
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				c.lines <- m
			}
		}
		close(c.lines)
	}()
	go ta.a.hookRun(context.Background(), name, server, 0)
	ta.t.Cleanup(func() { _ = client.Close() })
	return c
}

// next is the next line the server sent, of the type wanted.
func (c *hookClient) next(t string) hookMsg {
	c.t.Helper()
	select {
	case m, ok := <-c.lines:
		require.True(c.t, ok, "the hook closed, wanting %s", t)
		require.Equal(c.t, t, m.T, "%+v", m)
		return m
	case <-time.After(10 * time.Second):
		c.t.Fatalf("no %s line", t)
	}
	return hookMsg{}
}

// say sends one of the session's lines and returns the server's answer.
func (c *hookClient) say(m hookMsg) hookMsg {
	c.t.Helper()
	c.n++
	m.Req = "q" + string(rune('a'+c.n))
	b, _ := json.Marshal(m)
	_, err := c.conn.Write(append(b, '\n'))
	require.NoError(c.t, err)
	for {
		r := <-c.lines
		if r.Req == m.Req {
			return r
		}
		require.NotEmpty(c.t, r.T, "the hook closed waiting for the answer")
	}
}

func (ta *testApp) hookSession() *hookSession {
	h := ta.a.hookHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cur
}

// waitHook waits for the record to say state.
func (ta *testApp) waitHook(state string) sprint.HookRecord {
	ta.t.Helper()
	for range 500 {
		if rec, ok := ta.hookRecord(); ok && rec.State == state && (state != sprint.HookHooked || rec.Session != "") {
			return rec
		}
		time.Sleep(10 * time.Millisecond)
	}
	rec, _ := ta.hookRecord()
	ta.t.Fatalf("the record never said %s: %+v", state, rec)
	return rec
}

// The owner, 2026-10-10: "This is not optional. Every command fails with 'You must
// hook in first'." Every coordinator command, and every other command the seat's
// holder runs but a worker's and the machine's, fails with exactly that line until
// the seat has a live, proven hook; nothing is written. The hook and unhook, the
// workers' verbs, the machine's and anyone else's reads run.
func TestEveryCoordinatorCommandFailsUntilHookedIn(t *testing.T) {
	t.Parallel()
	const name = "hook-a"
	ta, env := hookSprint(t, name)
	ctx := context.Background()

	st, err := ta.a.store(common{redis: "mem:0"})
	require.NoError(t, err)
	for v, class := range verbClasses {
		switch class {
		case classCoordinator, classSeat, classRead:
		default:
			continue
		}
		why, err := coordinatorOnly(ctx, st, common{verb: v, actor: name})
		require.NoError(t, err, v)
		if hookExempt[v] {
			assert.Empty(t, why, v)
			continue
		}
		assert.Equal(t, sprint.HookFirst, why, v)
	}
	for _, line := range []string{"add --stream s1 --count 2", "init --readers reader-c", "start", "hold s1-1 --reason r", "where", "view coordinator", "card s1-1", "inbox", "seat check", "seat push", "coordinator glenn --reason r"} {
		ta.refusedFirst(line, sprint.HookFirst)
	}
	// no override: a flag no verb has is its own refusal, never a way past
	code, _, errs := ta.doLined("add --stream s1 --count 2 --no-hook")
	assert.NotEqual(t, 0, code, errs)

	// the workers', the machine's and another's verbs run
	ta.lined(func() { ta.ok("tick") })
	code, _, errs = ta.doLined("take --as m1")
	assert.NotContains(t, errs, sprint.HookFirst)
	assert.NotEqual(t, 2, code, errs)
	_, out, errs := ta.doLined("where --actor glenn")
	assert.NotContains(t, out+errs, sprint.HookFirst)
	// the owner's seat check says the hook DOWN with its remedy
	ta.a.outside = mockHealthyOutside()
	env["NOVA_SPRINT_ACTOR"] = "glenn"
	_, out, _ = ta.doLined("seat check")
	assert.Contains(t, out, "MACHINERY hook DOWN holder="+name+" state=none unacked=0 proven=- why=\""+sprint.HookFirst+"\" remedy=\"nova-sprint hook\"", out)
	env["NOVA_SPRINT_ACTOR"] = name

	// the hook's own verbs are never refused by the gate
	code, _, errs = ta.doLined("hook --ack 1")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "no hook is running on this machine for "+name)
	code, _, errs = ta.doLined("unhook")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "no hook is running on this machine for "+name)

	// the push proof's verbs are gone
	for _, v := range verbs {
		assert.NotContains(t, []string{"seat pong", "seat watch"}, v.name)
	}
	_, ok := verbClasses["seat pong"]
	assert.False(t, ok)
	_, ok = verbClasses["seat watch"]
	assert.False(t, ok)
}

// A hook counts only once the session answers the challenge it read off the stream,
// over the subscription (SeatHook.tla, Prove; ProofMeansRead): a wrong answer is
// refused; the right one lets every command run. Each judgment is pushed with its id;
// one left unacknowledged past its bound refuses every command naming it, until it is
// acknowledged (AckedOrBlocking). No challenge is asked again on a connected hook; a
// seat that acts keeps its hook (HookStays), and one silent past its bound lapses:
// recorded gone, and the owner told.
func TestAProvenHookRunsAndAPushBlocksUntilAcknowledged(t *testing.T) {
	t.Parallel()
	const name = "hook-b"
	ta, _ := hookSprint(t, name)

	c := ta.hookOn(name)
	hello := c.next("hello")
	assert.Equal(t, name, hello.Name)
	chal := c.next("challenge").Challenge
	require.Len(t, chal, 32)
	rec := ta.waitHook(sprint.HookHooked)
	assert.True(t, rec.Proven.IsZero(), "hooked is not proven")
	assert.NotContains(t, rec.Session+rec.Why, chal, "the challenge is only in the stream")
	ta.refusedFirst("add --stream s1 --count 2", sprint.HookFirst)

	r := c.say(hookMsg{T: "prove", Challenge: "0000"})
	assert.Equal(t, "refused", r.T)
	ta.refusedFirst("add --stream s1 --count 2", sprint.HookFirst)
	r = c.say(hookMsg{T: "prove", Challenge: chal})
	require.Equal(t, "ok", r.T, r.Why)
	assert.Equal(t, ta.a.now().Add(sprint.HookSilentBound).UTC(), r.Live.UTC())
	r = c.say(hookMsg{T: "prove", Challenge: chal})
	assert.Equal(t, "refused", r.T, "a challenge counts once")
	ta.lined(func() { ta.ok("add --stream s1 --count 2") })
	ta.lined(func() { ta.ok("where") })

	// a judgment for the seat is pushed with its id
	ta.lined(func() { ta.ok("remind --in 1m --note look-here --for " + name) })
	ta.step(2 * time.Minute)
	ta.lined(func() { ta.ok("tick") })
	s := ta.hookSession()
	require.NotNil(t, s)
	s.look(context.Background())
	p := c.next("push")
	assert.Equal(t, uint64(1), p.ID)
	assert.NotEmpty(t, p.What)
	rec, _ = ta.hookRecord()
	require.NotNil(t, rec.Oldest)
	assert.Equal(t, 1, rec.Unacked)
	s.look(context.Background()) // nothing new: nothing pushed twice
	ta.lined(func() { ta.ok("where") })

	// past its bound unacknowledged, every command is refused naming it; no challenge is
	// asked again on a connection that stays up
	ta.step(sprint.HookAckBound + time.Second)
	ta.refusedFirst("add --stream s2 --count 1 --one", sprint.HookUnacked(*rec.Oldest, 1))
	s.look(context.Background())
	r = c.say(hookMsg{T: "ack", ID: 9})
	assert.Equal(t, "refused", r.T, "no push 9 was delivered")
	r = c.say(hookMsg{T: "ack", ID: 1})
	require.Equal(t, "ok", r.T, r.Why)
	assert.Equal(t, 0, r.Unacked)
	ta.lined(func() { ta.ok("add --stream s2 --count 1 --one") })

	// silent past its bound, it lapses: gone, and the owner told
	ta.step(sprint.HookSilentBound + time.Second)
	s.look(context.Background())
	bye := c.next("bye")
	assert.Contains(t, bye.Why, "silent past")
	rec = ta.waitHook(sprint.HookGone)
	assert.Contains(t, rec.Why, "the seat was silent past 15m0s")
	ta.refusedFirst("add --stream s3 --count 1 --one", sprint.HookFirst)
	ta.mu.Lock()
	sent := append([]string(nil), subjects(ta)...)
	ta.mu.Unlock()
	assert.Contains(t, sent, "seat "+name+" is gone -> glenn")
}

func subjects(ta *testApp) []string {
	var out []string
	for _, m := range ta.sent {
		out = append(out, m.Subject+" -> "+strings.Join(m.To, ","))
	}
	return out
}

// A seat that drops without unhook is recorded gone at once and the owner told
// (SeatHook.tla, Drop; RecordTrue); unhook records it away and tells the owner
// (Unhook); a new hook replaces the one open, whose proof does not carry over.
func TestADropIsGoneAndUnhookIsAway(t *testing.T) {
	t.Parallel()
	const name = "hook-c"
	ta, _ := hookSprint(t, name)

	c := ta.hookOn(name)
	c.next("hello")
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
	ta.lined(func() { ta.ok("where") })
	require.NoError(t, c.conn.Close())
	rec := ta.waitHook(sprint.HookGone)
	assert.Contains(t, rec.Why, "without nova-sprint unhook")
	ta.refusedFirst("where", sprint.HookFirst)

	c = ta.hookOn(name)
	c.next("hello")
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
	first := ta.waitHook(sprint.HookHooked)
	// a second hook replaces the first: the first is told and closed, and the new one
	// is not proven by the first's answer
	d := ta.hookOn(name)
	assert.Contains(t, c.next("bye").Why, "replaced")
	d.next("hello")
	d.next("challenge")
	rec = ta.waitHook(sprint.HookHooked)
	for rec.Session == first.Session {
		time.Sleep(10 * time.Millisecond)
		rec, _ = ta.hookRecord()
	}
	assert.True(t, rec.Proven.IsZero())
	ta.refusedFirst("where", sprint.HookFirst)

	r := d.say(hookMsg{T: "unhook"})
	assert.Equal(t, "ok", r.T)
	d.next("bye")
	rec = ta.waitHook(sprint.HookAway)
	assert.Equal(t, "nova-sprint unhook", rec.Why)
	ta.refusedFirst("where", sprint.HookFirst)
	ta.mu.Lock()
	sent := subjects(ta)
	ta.mu.Unlock()
	assert.Contains(t, sent, "seat "+name+" is gone -> glenn")
	assert.Contains(t, sent, "seat "+name+" is away -> glenn")

	// a server that starts finds a record left hooked by the one before: gone, and told
	ta.lined(func() {
		st, err := ta.a.store(common{redis: "mem:0"})
		require.NoError(t, err)
		require.NoError(t, writeHook(context.Background(), st, sprint.HookRecord{Name: name, Session: "old", State: sprint.HookHooked, Proven: ta.a.now(), Live: ta.a.now().Add(time.Hour)}))
	})
	ta.lined(func() { ta.ok("where") })
	ta.a.hookServerStart(context.Background())
	rec, _ = ta.hookRecord()
	assert.Equal(t, sprint.HookGone, rec.State)
	assert.Contains(t, rec.Why, "the server restarted")
	ta.refusedFirst("where", sprint.HookFirst)

	// the record is the sprint's: teardown removes it
	ta.lined(func() {
		st, err := ta.a.store(common{redis: "mem:0"})
		require.NoError(t, err)
		_, err = st.Teardown(context.Background())
		require.NoError(t, err)
		_, ok, err := readHook(context.Background(), st)
		require.NoError(t, err)
		assert.False(t, ok, "teardown left the hook record")
	})
}

// The whole path, as the seat runs it: the server listens, `nova-sprint hook` holds the
// subscription and prints one event a line, `hook --prove <challenge>` answers through
// the running hook's connection, the coordinator's commands go to the server and run,
// and `unhook` ends it with exit 0. Before the proof, a command sent to the server
// fails with the one line.
func TestTheHookFromTheCommandLine(t *testing.T) {
	t.Parallel()
	const name = "hook-d"
	ta, env := hookSprint(t, name)
	srv := httptest.NewServer(localHandler{ta.a})
	t.Cleanup(srv.Close)
	env["NOVA_SPRINT_SERVER"] = srv.Listener.Addr().String()
	// the seat's side is a process of its own: it holds no store, and every
	// coordinator command goes to the server
	client := newApp(ta.a.getenv)
	client.hookSock = ta.a.hookSock
	run := func(line string) (int, string, string) {
		var out, errb bytes.Buffer
		code := client.run(strings.Fields(line), &out, &errb)
		return code, out.String(), errb.String()
	}

	pr, pw := io.Pipe()
	done := make(chan int, 1)
	go func() {
		var errb bytes.Buffer
		code := client.run([]string{"hook"}, pw, &errb)
		_ = pw.CloseWithError(io.EOF)
		done <- code
	}()
	sc := bufio.NewScanner(pr)
	nextLine := func(prefix string) string {
		t.Helper()
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), prefix) {
				return sc.Text()
			}
		}
		t.Fatalf("no line %q", prefix)
		return ""
	}
	assert.Contains(t, nextLine("HOOK CONNECTED"), "name="+name)
	line := nextLine("HOOK CHALLENGE ")
	chal := strings.TrimSuffix(strings.Fields(line)[2], ":")
	assert.Contains(t, line, "nova-sprint hook --prove "+chal+" --actor "+name)

	code, out, errs := run("add --stream s1 --count 2")
	assert.Equal(t, 1, code, out+errs)
	assert.Equal(t, sprint.HookFirst+"\n", errs)

	// a second hook on this machine is refused: one Monitor holds the seat
	code, _, errs = run("hook")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "a hook is already running on this machine for "+name)

	code, out, errs = run("hook --prove nope")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "not the challenge outstanding", errs)
	code, out, errs = run("hook --prove " + chal)
	require.Equal(t, 0, code, out+errs)
	assert.Contains(t, out, "HOOK PROVEN name="+name+" live-until=")
	assert.Contains(t, nextLine("HOOK PROVEN"), "name="+name)

	code, out, errs = run("add --stream s1 --count 2")
	require.Equal(t, 0, code, out+errs)

	code, out, errs = run("unhook")
	require.Equal(t, 0, code, out+errs)
	assert.Contains(t, out, "UNHOOK OK name="+name+" state=away")
	assert.Contains(t, nextLine("HOOK ENDED"), "unhooked")
	select {
	case code := <-done:
		assert.Equal(t, 0, code)
	case <-time.After(10 * time.Second):
		t.Fatal("the hook did not end after unhook")
	}
	code, _, errs = run("add --stream s2 --count 1 --one")
	assert.Equal(t, 1, code)
	assert.Equal(t, sprint.HookFirst+"\n", errs)
}

// tell sends one of the session's lines that wants no answer (the keepalive).
func (c *hookClient) tell(m hookMsg) {
	c.t.Helper()
	b, _ := json.Marshal(m)
	_, err := c.conn.Write(append(b, '\n'))
	require.NoError(c.t, err)
}

// drained is every line the hook's stream carries within a moment, none waited for.
func (c *hookClient) drained() []hookMsg {
	var out []hookMsg
	for {
		select {
		case m, ok := <-c.lines:
			if !ok {
				return out
			}
			out = append(out, m)
		case <-time.After(50 * time.Millisecond):
			return out
		}
	}
}

// The seat, 2026-10-11/12: the hook dropped about 15 minutes after each proof though the
// seat's stream stayed connected and the seat kept acting; every lapse refused every
// coordinator verb and replayed ~100 pushes. A hooked seat that acts once a minute (a verb,
// an acknowledgement) for 40 minutes of sprint clock never lapses: no challenge after the
// first, no bye, every verb runs, the record live past each act.
func TestAConnectedSeatThatActsNeverLapses(t *testing.T) {
	t.Parallel()
	const name = "hook-live-a"
	ta, _ := hookSprint(t, name)
	c := ta.hookOn(name)
	c.next("hello")
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
	s := ta.hookSession()
	require.NotNil(t, s)
	ta.lined(func() { ta.ok("add --stream s1 --count 1 --one") })
	for minute := 1; minute <= 40; minute++ {
		ta.step(time.Minute)
		s.look(context.Background())
		for _, m := range c.drained() {
			require.NotEqual(t, "challenge", m.T, "minute %d: a challenge asked again on a connected hook", minute)
			require.NotEqual(t, "bye", m.T, "minute %d: the hook lapsed: %s", minute, m.Why)
			if m.T == "push" {
				require.Equal(t, "ok", c.say(hookMsg{T: "ack", ID: m.ID}).T, "minute %d: the seat answers its push", minute)
			}
		}
		if minute%2 == 0 {
			ta.lined(func() { ta.ok("where") }) // a verb of the seat's
		} else {
			c.tell(hookMsg{T: "alive"}) // the client's own keepalive: no answer
		}
		rec, _ := ta.hookRecord()
		require.Equal(t, sprint.HookHooked, rec.State, "minute %d", minute)
		require.True(t, sprint.HookLive(rec, true, name, ta.a.now()), "minute %d: live until %s", minute, rec.Live)
	}
	rec, _ := ta.hookRecord()
	assert.True(t, rec.Live.After(ta.a.now().Add(sprint.HookSilentBound-2*time.Minute)), "live a bound past the last act: %s", rec.Live)
}

// A connected seat silent past the bound (no verb, no answer, no keepalive) lapses once: one
// bye, the record gone, one judgment of the lapse, the owner told once; a later look writes
// nothing more; a new hook's proof closes the judgment.
func TestASilentSeatLapsesOnceAndSaysSo(t *testing.T) {
	t.Parallel()
	const name = "hook-live-b"
	const lapsed = "the seat's hook lapsed: the seat was silent past its bound" // sprint.NHookLapsed
	ta, _ := hookSprint(t, name)
	c := ta.hookOn(name)
	c.next("hello")
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
	s := ta.hookSession()
	require.NotNil(t, s)
	ta.step(sprint.HookAckBound) // within the bound: still live
	s.look(context.Background())
	rec, _ := ta.hookRecord()
	require.Equal(t, sprint.HookHooked, rec.State)

	ta.step(sprint.HookSilentBound - sprint.HookAckBound + time.Second)
	s.look(context.Background())
	var byes int
	for _, m := range c.drained() {
		if m.T == "bye" {
			byes++
			assert.Contains(t, m.Why, "silent past")
		}
	}
	assert.Equal(t, 1, byes, "one bye")
	rec = ta.waitHook(sprint.HookGone)
	assert.Contains(t, rec.Why, "silent past")
	s.look(context.Background())
	lapses := func() int {
		n := 0
		ta.lined(func() {
			st, err := ta.a.store(common{redis: "mem:0"})
			require.NoError(t, err)
			open, err := st.B.OpenNotes(context.Background())
			require.NoError(t, err)
			for _, o := range open {
				if o.Note.Type == lapsed {
					n++
				}
			}
		})
		return n
	}
	assert.Equal(t, 1, lapses(), "one judgment of the lapse")
	ta.mu.Lock()
	told := 0
	for _, x := range subjects(ta) {
		if x == "seat "+name+" is gone -> glenn" {
			told++
		}
	}
	ta.mu.Unlock()
	assert.Equal(t, 1, told, "the owner told once")

	d := ta.hookOn(name)
	d.next("hello")
	require.Equal(t, "ok", d.say(hookMsg{T: "prove", Challenge: d.next("challenge").Challenge}).T)
	assert.Equal(t, 0, lapses(), "a new hook's proof answers the lapse")
}

// step moves the test's clock.
func (ta *testApp) step(d time.Duration) {
	ta.mu.Lock()
	ta.now = ta.now.Add(d)
	ta.mu.Unlock()
}

// failHookKV embeds the test's in-memory store, so every part of it (the sprint
// Backend, KV, and the optional interfaces the engine may find) is what it was, and
// refuses the first n writes of the seat's hook record (store.KeySeatHook): a failed
// record write.
type failHookKV struct {
	*store.Mem
	left int
}

func (f *failHookKV) SetKey(ctx context.Context, name, value string) error {
	if f.left > 0 && name == store.KeySeatHook {
		f.left--
		return errors.New("the hook record could not be written")
	}
	return f.Mem.SetKey(ctx, name, value)
}

// failHookWrite arms the store so the next n writes of the hook record fail.
func (ta *testApp) failHookWrite(n int) {
	kv := &failHookKV{Mem: ta.m, left: n}
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) { return kv, nil }
}

// A push is delivered only once its record names it (SeatHook.tla, Deliver and
// AckedOrBlocking: Deliver writes the record in the same step). A record write that
// fails records and sends nothing: the look undoes the sequence, the sent keys and the
// open entries it added, so the next look offers the push again, records it, and sends
// it once. Before the fix the push went out while the record named no unacknowledged
// push, so a coordinator command past HookAckBound ran, though the push was delivered.
func TestAPushIsSentOnlyOnceRecorded(t *testing.T) {
	t.Parallel()
	const name = "hook-f"
	ta, _ := hookSprint(t, name)

	c := ta.hookOn(name)
	c.next("hello")
	chal := c.next("challenge").Challenge
	ta.waitHook(sprint.HookHooked)
	r := c.say(hookMsg{T: "prove", Challenge: chal})
	require.Equal(t, "ok", r.T, r.Why)

	// the record write fails for this look: it is delivered nothing, and the record
	// names no unacknowledged push
	ta.failHookWrite(1)
	ta.lined(func() { ta.ok("add --stream s1 --count 2") })
	ta.lined(func() { ta.ok("where") })
	ta.lined(func() { ta.ok("remind --in 1m --note look-here --for " + name) })
	ta.step(2 * time.Minute)
	ta.lined(func() { ta.ok("tick") })
	s := ta.hookSession()
	require.NotNil(t, s)
	s.look(context.Background())
	select {
	case m := <-c.lines:
		t.Fatalf("a push was delivered though its record write failed: %+v", m)
	case <-time.After(200 * time.Millisecond):
	}
	rec, _ := ta.hookRecord()
	assert.Nil(t, rec.Oldest)
	assert.Equal(t, 0, rec.Unacked)

	// the next look offers it again, records it, and delivers it once, with id 1
	s.look(context.Background())
	p := c.next("push")
	assert.Equal(t, uint64(1), p.ID)
	assert.NotEmpty(t, p.What)
	rec, _ = ta.hookRecord()
	require.NotNil(t, rec.Oldest)
	assert.Equal(t, 1, rec.Unacked)
	assert.Equal(t, uint64(1), rec.Oldest.ID)
	s.look(context.Background()) // nothing new: nothing pushed twice
	select {
	case m := <-c.lines:
		t.Fatalf("the push was delivered twice: %+v", m)
	case <-time.After(200 * time.Millisecond):
	}

	// past its bound unacknowledged, the command is refused naming it; after the ack
	// it runs
	ta.step(sprint.HookAckBound + time.Second)
	ta.refusedFirst("add --stream s2 --count 1 --one", sprint.HookUnacked(*rec.Oldest, 1))
	r = c.say(hookMsg{T: "ack", ID: 1})
	require.Equal(t, "ok", r.T, r.Why)
	ta.lined(func() { ta.ok("add --stream s2 --count 1 --one") })
}
