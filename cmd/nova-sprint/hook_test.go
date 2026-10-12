package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
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
	t       *testing.T
	conn    net.Conn
	lines   chan hookMsg
	n       int
	session string
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
				if m.T == "hello" {
					c.session = m.Session
				}
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
	if m.T == "ack" && m.Session == "" {
		m.Session = c.session
	}
	return c.sayRaw(m)
}

func (c *hookClient) sayRaw(m hookMsg) hookMsg {
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
	code, _, errs = ta.doLined("hook --ack 1 --session old-session --actor " + name)
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "no hook is running on this machine for "+name)
	code, _, errs = ta.doLined("hook --ack 1 --actor " + name)
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "requires --session <token>")
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
// acknowledged (AckedOrBlocking). The server challenges again every HookEvery; an
// answered seat keeps its hook (HookStays), and a challenge unanswered past its bound
// unhooks: recorded gone, and the owner told (Miss).
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
	assert.Equal(t, ta.a.now().Add(sprint.HookEvery+sprint.HookAnswerBound).UTC(), r.Live.UTC())
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

	// past its bound unacknowledged, every command is refused naming it; the cadence
	// challenges again meanwhile
	ta.step(sprint.HookAckBound + time.Second)
	ta.refusedFirst("add --stream s2 --count 1 --one", sprint.HookUnacked(*rec.Oldest, 1))
	s.look(context.Background())
	again := c.next("challenge").Challenge
	assert.NotEqual(t, chal, again)
	r = c.say(hookMsg{T: "ack", ID: 9})
	assert.Equal(t, "refused", r.T, "no push 9 was delivered")
	r = c.say(hookMsg{T: "ack", ID: 1})
	require.Equal(t, "ok", r.T, r.Why)
	assert.Equal(t, 0, r.Unacked)
	ta.lined(func() { ta.ok("add --stream s2 --count 1 --one") })
	r = c.say(hookMsg{T: "prove", Challenge: again})
	require.Equal(t, "ok", r.T, r.Why)

	// answered, it stays; unanswered past its bound, it is gone and the owner told
	ta.step(sprint.HookEvery)
	s.look(context.Background())
	c.next("challenge")
	// The due set grew from two to three when s2 was added, which starts a new
	// computed machine:stopped episode and therefore a distinct push. It is
	// intentionally not the earlier acknowledged group. The challenge cadence
	// look delivered it before the answer deadline, and it remains unacknowledged.
	computed := c.next("push")
	assert.Equal(t, "machine:stopped", computed.What)
	assert.Greater(t, computed.ID, p.ID)
	rec, haveRecord := ta.hookRecord()
	require.True(t, haveRecord)
	assert.Equal(t, 1, rec.Unacked, "the later computed push remains independently unacknowledged")
	ta.step(sprint.HookAnswerBound + time.Second)
	s.look(context.Background())
	bye := c.next("bye")
	assert.Contains(t, bye.Why, "not answered in time")
	rec = ta.waitHook(sprint.HookGone)
	assert.Contains(t, rec.Why, "was not answered within 5m0s")
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

// The record's Live is the outstanding challenge's bound, not the proof's: a
// re-challenge sent late (a slow look) keeps the seat live until the challenge's own
// bound, so a command between the proof's Live and that bound runs. The model refuses
// only once the outstanding challenge is late (SeatHook.tla, Rechallenge and Gate:
// ~lateC); before this the record held the proof's bound and refused HookFirst.
func TestARechallengeKeepsTheHookLiveUntilItsOwnBound(t *testing.T) {
	t.Parallel()
	const name = "hook-e"
	ta, _ := hookSprint(t, name)

	c := ta.hookOn(name)
	c.next("hello")
	provedAt := ta.a.now()
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
	rec, _ := ta.hookRecord()
	require.Equal(t, provedAt.Add(sprint.HookEvery+sprint.HookAnswerBound).UTC(), rec.Live.UTC())

	// the look that sends the re-challenge runs late: the challenge goes out 30s after
	// HookEvery, and its own bound is later than the proof's
	ta.step(sprint.HookEvery + 30*time.Second)
	s := ta.hookSession()
	require.NotNil(t, s)
	s.look(context.Background())
	challengeAt := ta.a.now()
	require.Len(t, c.next("challenge").Challenge, 32)

	// past the proof's Live, inside the challenge's own bound: a command runs
	ta.step(sprint.HookAnswerBound - 15*time.Second)
	require.True(t, ta.a.now().After(provedAt.Add(sprint.HookEvery+sprint.HookAnswerBound)),
		"the proof's bound has passed")
	ta.lined(func() { ta.ok("where") })
	rec, _ = ta.hookRecord()
	require.Equal(t, challengeAt.Add(sprint.HookAnswerBound).UTC(), rec.Live.UTC(),
		"the record is live until the re-challenge's own bound")

	// unanswered past the challenge's own bound, the look unhooks it (Miss)
	ta.step(30 * time.Second)
	s.look(context.Background())
	bye := c.next("bye")
	assert.Contains(t, bye.Why, "not answered in time")
	rec = ta.waitHook(sprint.HookGone)
	assert.Contains(t, rec.Why, "was not answered within 5m0s")
	ta.refusedFirst("where", sprint.HookFirst)

	// a fresh proof is live until its next challenge's bound (HookProven, unchanged)
	d := ta.hookOn(name)
	d.next("hello")
	provedAt = ta.a.now()
	require.Equal(t, "ok", d.say(hookMsg{T: "prove", Challenge: d.next("challenge").Challenge}).T)
	rec, _ = ta.hookRecord()
	assert.Equal(t, provedAt.Add(sprint.HookEvery+sprint.HookAnswerBound).UTC(), rec.Live.UTC())
}

// step moves the test's clock.
func (ta *testApp) step(d time.Duration) {
	ta.mu.Lock()
	ta.now = ta.now.Add(d)
	ta.mu.Unlock()
}

// restartTestServer creates a fresh app over the same persisted sprint, like the
// server process restarting while its store remains available.
func (ta *testApp) restartTestServer() {
	ta.t.Helper()
	old := ta.a
	a := newApp(old.getenv)
	a.now = old.now
	a.sleep = old.sleep
	a.backend = old.backend
	a.hookSock = old.hookSock
	a.serveAddr = old.serveAddr
	a.bus = old.bus
	a.meter = old.meter
	a.mergeQueue = old.mergeQueue
	a.checkTwin = old.checkTwin
	ta.a = a
	a.hookServerStart(context.Background())
}

func collectHookPushes(c *hookClient) []hookMsg {
	c.t.Helper()
	var pushes []hookMsg
	for {
		select {
		case m, ok := <-c.lines:
			if !ok {
				return pushes
			}
			if m.T == "push" {
				pushes = append(pushes, m)
			}
		case <-time.After(250 * time.Millisecond):
			return pushes
		}
	}
}

// hookCIRedOnce records one CI-red notification as a replay fixture. It does not
// exercise the wait command or claim that this card-holding judgment is waitable.
func (ta *testApp) hookCIRedOnce(card, run string) {
	ta.t.Helper()
	ta.a.sleep(time.Second)
	ta.ok("ci " + card + " --red --run " + run)
}

func (ta *testApp) hookTick() {
	ta.t.Helper()
	st, err := ta.a.store(common{redis: "mem:0", actor: sprint.MachineActor})
	require.NoError(ta.t, err)
	_, err = st.Tick(context.Background())
	require.NoError(ta.t, err)
}

// A running-time wait on a timer judgment survives a server restart. A separate
// card-holding invariant remains open exactly once; it is never used as a wait.
func TestAWaitHoldsAcrossAServerRestart(t *testing.T) {
	t.Parallel()
	const name = "hook-wait-restart"
	ta, _ := hookSprint(t, name)
	first := ta.hookOn(name)
	first.next("hello")
	challenge := first.next("challenge").Challenge
	require.Equal(t, "ok", first.say(hookMsg{T: "prove", Challenge: challenge}).T)
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
	ta.ok("start")
	ta.timerOnce("restart-look")
	// timerOnce restores a stopped fixture after the reminder tick; start it
	// again before measuring the wait over running time.
	ta.ok("start")
	// A second live work card for the same primary is a stable rule-2 invariant
	// violation; keep that card-holding judgment open to check it is not duplicated.
	st, err := ta.a.store(common{redis: "mem:0", actor: name})
	require.NoError(t, err)
	snap, err := st.Load(context.Background(), []string{sprint.Fleet}, nil)
	require.NoError(t, err)
	_, err = ta.m.Apply(context.Background(), ntable.BatchManifest{Schema: 1, Table: st.Names.Table(sprint.Fleet), Epoch: fmt.Sprint(snap.Epoch),
		ExpectedTableRevision: fmt.Sprint(snap.Fleet.Revision), OperationID: "outside-invariant",
		Members: []ntable.BatchMemberEntry{{ID: "s1-1.w9", Expect: &ntable.MemberExpect{Absent: true},
			Create: &ntable.MemberCreateOp{Row: "m2", Col: sprint.Working, Score: 1}, Set: map[string]string{"kind": "work", "primary": "s1-1", "gen": "1"}}}})
	require.NoError(t, err)
	ta.ok("tick")
	var invariant sprint.Group
	for _, g := range ta.inboxGroups() {
		if g.Kind == sprint.Judgment && g.Type == sprint.NInvariant {
			invariant = g
			break
		}
	}
	require.NotEmpty(t, invariant.ID, "the tick-check invariant judgment: %+v", ta.inboxGroups())
	held := ta.group(sprint.NTimer, "")
	ta.ok("wait " + held.ID + " --for 60m")
	held = ta.group(sprint.NTimer, "")
	require.True(t, held.Quiet, "wait persisted the timer review: %+v", held)
	reviewUntil := ta.a.now().Add(60 * time.Minute)
	require.Equal(t, reviewUntil, held.Due, "wait persisted the timer review time: %+v", held)
	// The server and hook come back 15m later, inside the stated running-time hold.
	beforeRestart := ta.a.now()
	ta.step(15 * time.Minute)
	require.Equal(t, beforeRestart.Add(15*time.Minute), ta.a.now(), "the running clock advanced through the hold")
	require.True(t, ta.a.now().Before(reviewUntil), "the timer review remains in the future after the clock advance")
	require.NoError(t, first.conn.Close())
	ta.waitHook(sprint.HookGone)
	ta.restartTestServer()
	second := ta.hookOn(name)
	second.next("hello")
	challenge = second.next("challenge").Challenge
	require.Equal(t, "ok", second.say(hookMsg{T: "prove", Challenge: challenge}).T)
	ta.ok("tick")
	s := ta.hookSession()
	require.NotNil(t, s)
	s.look(context.Background())
	restartedHeld := ta.group(sprint.NTimer, "")
	assert.True(t, restartedHeld.Quiet, "the timer remains quiet after server restart: %+v", restartedHeld)
	assert.Equal(t, reviewUntil, restartedHeld.Due, "the timer review survived restart: %+v", restartedHeld)
	replayed := collectHookPushes(second)
	for _, p := range replayed {
		assert.NotEqual(t, held.ID, p.What, "a held timer judgment is not raised again after restart")
	}
	notes, _, err := ta.m.NotesSince(context.Background(), "", 10000)
	require.NoError(t, err)
	var raised int
	for _, n := range notes {
		if n.Kind == sprint.Judgment && n.Type == sprint.NInvariant && n.What == invariant.What {
			raised++
		}
	}
	assert.Equal(t, 1, raised, "the tick-check invariant was not raised again after restart: %+v", notes)
}

// A new hook session replays only groups whose pushes remain unacknowledged;
// a held group and an already acknowledged push are not sent again.
func TestAReconnectReplaysOnlyUnackedPushesInMemory(t *testing.T) {
	t.Parallel()
	const name = "hook-reconnect"
	ta, _ := hookSprint(t, name)
	first := ta.hookOn(name)
	first.next("hello")
	challenge := first.next("challenge").Challenge
	require.Equal(t, "ok", first.say(hookMsg{T: "prove", Challenge: challenge}).T)

	ta.lined(func() { ta.ok("add --stream s1 --count 1 --one") })
	ta.lined(func() { ta.ok("add --stream s2 --count 1 --one") })
	ta.lined(func() { ta.ok("add --stream s3 --count 1 --one") })
	ta.deal(3)
	ta.ok("start")
	for _, id := range []string{"s1-1", "s2-1", "s3-1"} {
		card := ta.fleetCard(id + ".w1")
		require.NotNil(t, card)
		ta.ok("take --as " + card.Row + " " + card.ID + "@1")
		ta.ok("finish --as " + card.Row + " " + card.ID + "@1")
	}
	ta.ok("ask")
	for _, id := range []string{"s1-1", "s2-1", "s3-1"} {
		ta.hookCIRedOnce(id, "run-1")
	}
	acked := ta.group(sprint.NCIRed, "s1")
	pending := ta.group(sprint.NCIRed, "s2")
	held := ta.group(sprint.NCIRed, "s3")

	s := ta.hookSession()
	require.NotNil(t, s)
	s.look(context.Background())
	initial := collectHookPushes(first)
	require.NotEmpty(t, first.session)
	ids := map[string]uint64{}
	for _, p := range initial {
		assert.Equal(t, first.session, p.Session, "the PUSH carries the token needed to bind its ACK")
		ids[p.What] = p.ID
	}
	require.Contains(t, ids, acked.ID, "the acknowledged candidate was delivered")
	require.Contains(t, ids, pending.ID, "the pending candidate was delivered")
	require.Greater(t, ids[pending.ID], ids[acked.ID], "the cumulative ack leaves the later pending push open")
	var initialWhat []string
	for what := range ids {
		initialWhat = append(initialWhat, what)
	}
	assert.ElementsMatch(t, []string{acked.ID, pending.ID, held.ID}, initialWhat, "the fixture has only the three intended pushes")
	r := first.sayRaw(hookMsg{T: "ack", ID: ids[acked.ID]})
	require.Equal(t, "refused", r.T, "a missing session token cannot acknowledge even on the current stream")
	rec, ok := ta.hookRecord()
	require.True(t, ok)
	assert.Equal(t, 3, rec.Unacked, "a missing session token leaves every push open")
	require.Equal(t, "ok", first.say(hookMsg{T: "ack", ID: ids[acked.ID]}).T, "acknowledge the first push only with its session token")

	second := ta.hookOn(name)
	second.next("hello")
	challenge = second.next("challenge").Challenge
	require.Equal(t, "ok", second.say(hookMsg{T: "prove", Challenge: challenge}).T)
	s = ta.hookSession()
	require.NotNil(t, s)
	s.look(context.Background())
	replayed := collectHookPushes(second)
	require.Len(t, replayed, 2, "only open, unacknowledged groups are replayed: %+v", replayed)
	for _, push := range replayed {
		assert.Equal(t, second.session, push.Session)
	}
	var replayedWhat []string
	for _, push := range replayed {
		replayedWhat = append(replayedWhat, push.What)
	}
	assert.ElementsMatch(t, []string{pending.ID, held.ID}, replayedWhat)
	assert.NotContains(t, replayedWhat, acked.ID, "the acknowledged group is not replayed")
	var initialMax, replayMax uint64
	for _, id := range ids {
		if id > initialMax {
			initialMax = id
		}
	}
	for _, push := range replayed {
		assert.Greater(t, push.ID, initialMax, "replayed pushes use IDs above every push from the old session")
		if push.ID > replayMax {
			replayMax = push.ID
		}
	}
	// A delayed cumulative ACK from the old stream must not clear freshly replayed
	// pushes. The old pending ID was valid in that stream, but the replay has new IDs.
	r = second.say(hookMsg{T: "ack", ID: ids[pending.ID], Session: first.session})
	require.Equal(t, "refused", r.T, "an old-session token cannot acknowledge replayed pushes")
	rec, ok = ta.hookRecord()
	require.True(t, ok)
	assert.Equal(t, 2, rec.Unacked, "the old-session ACK leaves both new deliveries pending")
	assert.NotContains(t, rec.Acked, noteKey(pending.ID), "the stale ACK did not durably acknowledge the replay")
	assert.Equal(t, replayMax, rec.PushSeq, "the push high-water mark is persisted with the pending deliveries")

	// A server restart also restores the high-water mark, not a zeroed session-local
	// counter. Replayed notes get IDs above the entire preceding session, and its last
	// delayed ACK remains harmless.
	require.NoError(t, second.conn.Close())
	ta.waitHook(sprint.HookGone)
	ta.restartTestServer()
	third := ta.hookOn(name)
	third.next("hello")
	challenge = third.next("challenge").Challenge
	require.Equal(t, "ok", third.say(hookMsg{T: "prove", Challenge: challenge}).T)
	s = ta.hookSession()
	require.NotNil(t, s)
	s.look(context.Background())
	replayedAfterRestart := collectHookPushes(third)
	require.Len(t, replayedAfterRestart, 2)
	for _, push := range replayedAfterRestart {
		assert.Greater(t, push.ID, replayMax, "restart replay uses IDs above the prior server's high-water mark")
	}
	r = third.say(hookMsg{T: "ack", ID: replayMax, Session: second.session})
	require.Equal(t, "refused", r.T, "a pre-restart session token cannot acknowledge current pushes")
	rec, ok = ta.hookRecord()
	require.True(t, ok)
	assert.Equal(t, 2, rec.Unacked, "the pre-restart ACK cannot clear post-restart pushes")
	var latest uint64
	for _, push := range replayedAfterRestart {
		if push.ID > latest {
			latest = push.ID
		}
	}
	assert.Equal(t, latest, rec.PushSeq, "the restarted server persists the new high-water mark")
	require.NoError(t, third.conn.Close())
	ta.waitHook(sprint.HookGone)
	st, err := ta.a.store(common{redis: "mem:0", actor: name})
	require.NoError(t, err)
	cleared, err := st.Clear(context.Background())
	require.NoError(t, err)
	require.Greater(t, cleared.To, rec.Epoch, "the fixture advanced the sprint epoch")
	ta.restartTestServer()
	fourth := ta.hookOn(name)
	fourth.next("hello")
	challenge = fourth.next("challenge").Challenge
	require.Equal(t, "ok", fourth.say(hookMsg{T: "prove", Challenge: challenge}).T)
	s = ta.hookSession()
	require.NotNil(t, s)
	assert.Equal(t, cleared.To, s.epoch, "the new session adopted the new epoch")
	assert.Equal(t, latest, s.seq, "the push high-water mark survives an epoch change")
}

func TestHookAckWriteFailureLeavesThePushPending(t *testing.T) {
	t.Parallel()
	const name = "hook-ack-write-failure"
	ta, _ := hookSprint(t, name)
	first := ta.hookOn(name)
	first.next("hello")
	challenge := first.next("challenge").Challenge
	require.Equal(t, "ok", first.say(hookMsg{T: "prove", Challenge: challenge}).T)
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
	card := ta.fleetCard("s1-1.w1")
	require.NotNil(t, card)
	ta.failOnce(card.Row, "s1-1.w1@1", "ack write test")
	ta.ok("tick")
	s := ta.hookSession()
	require.NotNil(t, s)
	s.look(context.Background())
	p := first.next("push")

	// The record write is the ACK commit. A failed commit must refuse and retain
	// the push so a retry can commit it and clear the gate.
	ta.failHookWrite(1)
	r := first.say(hookMsg{T: "ack", ID: p.ID})
	assert.Equal(t, "refused", r.T)
	assert.Contains(t, r.Why, "acknowledgement could not be recorded")
	rec, ok := ta.hookRecord()
	require.True(t, ok)
	assert.Equal(t, 1, rec.Unacked, "a failed ACK commit stays pending")
	assert.Empty(t, rec.Acked, "a failed ACK commit is not remembered")

	r = first.say(hookMsg{T: "ack", ID: p.ID})
	require.Equal(t, "ok", r.T, r.Why)
	rec, ok = ta.hookRecord()
	require.True(t, ok)
	assert.Zero(t, rec.Unacked)
	require.Len(t, rec.Acked, 1, "the committed ACK is durable")
}

func TestHookAckOfEligibleGroupMemberDoesNotHideHeldMember(t *testing.T) {
	t.Parallel()
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	g := sprint.Group{ID: "j1", Kind: sprint.Judgment, Type: sprint.NAlarmReview, Notes: []string{"held-note", "eligible-note"}}
	open := []sprint.Open{
		{Note: sprint.Note{ID: "held-note", Kind: sprint.Judgment, Type: sprint.NAlarmReview, Review: now.Add(time.Hour), ReviewSet: now}},
		{Note: sprint.Note{ID: "eligible-note", Kind: sprint.Judgment, Type: sprint.NAlarmReview}},
	}
	active, before := deliveryKeys(g, open, now, nil)
	require.Len(t, active, 2)
	require.Len(t, before, 1, "only the unheld group member can be acknowledged")
	assert.Contains(t, before[0], "eligible-note")
	assert.NotContains(t, before[0], "held-note")
	_, due := deliveryKeys(g, open, now.Add(61*time.Minute), nil)
	require.Len(t, due, 2, "the previously held note becomes a fresh delivery obligation at its review time")
	assert.Contains(t, due[0], "held-note")
	assert.Contains(t, due[1], "eligible-note")
}

func TestHookReplaysANewNoteAddedToAnAcknowledgedGroup(t *testing.T) {
	t.Parallel()
	const name = "hook-growing-group"
	ta, _ := hookSprint(t, name)
	first := ta.hookOn(name)
	first.next("hello")
	challenge := first.next("challenge").Challenge
	require.Equal(t, "ok", first.say(hookMsg{T: "prove", Challenge: challenge}).T)
	ta.ok("start")
	ta.ok("add --stream s1 --count 2 --one")
	ta.deal(2)
	card1 := ta.fleetCard("s1-1.w1")
	require.NotNil(t, card1)
	ta.failOnce(card1.Row, "s1-1.w1@1", "first group member")
	ta.ok("tick")
	s := ta.hookSession()
	require.NotNil(t, s)
	s.look(context.Background())
	firstPush := first.next("push")
	require.Equal(t, "ok", first.say(hookMsg{T: "ack", ID: firstPush.ID}).T)

	card2 := ta.fleetCard("s1-2.w1")
	require.NotNil(t, card2)
	ta.failOnce(card2.Row, "s1-2.w1@1", "second group member")
	ta.ok("tick")
	s.look(context.Background())
	newPush := first.next("push")
	assert.Equal(t, firstPush.What, newPush.What, "the same group ID gained a new note")
	assert.Equal(t, uint64(2), newPush.ID)
	assert.Contains(t, newPush.Text, "size=2")
}

func TestHookReplaysANewComputedMachineEpisode(t *testing.T) {
	t.Parallel()
	const name = "hook-machine-episodes"
	ta, _ := hookSprint(t, name)
	client := ta.hookOn(name)
	client.next("hello")
	challenge := client.next("challenge").Challenge
	require.Equal(t, "ok", client.say(hookMsg{T: "prove", Challenge: challenge}).T)
	ta.ok("start")
	s := ta.hookSession()
	require.NotNil(t, s)
	ta.step(store.MachineSilence + time.Second)
	s.look(context.Background())
	first := collectHookPushes(client)
	var silent hookMsg
	for _, push := range first {
		if push.What == "machine:silent" {
			silent = push
		}
	}
	require.NotZero(t, silent.ID, "the machine's first silent episode was delivered: %+v", first)
	require.Equal(t, "ok", client.say(hookMsg{T: "ack", ID: silent.ID}).T)

	// A tick ends the first episode, but no hook look observes the gap. The
	// heartbeat then goes silent again under the same group ID.
	ta.ok("tick")
	ta.step(store.MachineSilence + time.Second)
	s.look(context.Background())
	second := collectHookPushes(client)
	var replay hookMsg
	for _, push := range second {
		if push.What == "machine:silent" {
			replay = push
		}
	}
	require.NotZero(t, replay.ID, "a later computed episode with the same group ID must be delivered: %+v", second)
	assert.Greater(t, replay.ID, silent.ID)
}

func TestHookEpochChangeClearsPriorPendingPush(t *testing.T) {
	t.Parallel()
	const name = "hook-epoch-pending"
	ta, _ := hookSprint(t, name)
	client := ta.hookOn(name)
	client.next("hello")
	challenge := client.next("challenge").Challenge
	require.Equal(t, "ok", client.say(hookMsg{T: "prove", Challenge: challenge}).T)
	ta.ok("start")
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
	card := ta.fleetCard("s1-1.w1")
	require.NotNil(t, card)
	ta.failOnce(card.Row, "s1-1.w1@1", "clear pending hook push")
	ta.ok("tick")
	s := ta.hookSession()
	require.NotNil(t, s)
	s.look(context.Background())
	p := client.next("push")
	require.NotZero(t, p.ID)
	rec, ok := ta.hookRecord()
	require.True(t, ok)
	require.Equal(t, 1, rec.Unacked)

	ta.lined(func() {
		st, err := ta.a.store(common{redis: "mem:0", actor: name})
		require.NoError(t, err)
		_, err = st.Clear(context.Background())
		require.NoError(t, err)
	})
	s.look(context.Background())
	rec, ok = ta.hookRecord()
	require.True(t, ok)
	assert.Equal(t, uint64(1), rec.Epoch)
	assert.Zero(t, rec.Unacked, "old-epoch pushes no longer block the hook gate")
	assert.Nil(t, rec.Oldest)
	code, _, errs := ta.doLined("where")
	assert.Zero(t, code, errs)
}

func TestHookTakeoverWriteFailureLeavesOldSessionLive(t *testing.T) {
	t.Parallel()
	const name = "hook-takeover-write-failure"
	ta, _ := hookSprint(t, name)
	first := ta.hookOn(name)
	first.next("hello")
	challenge := first.next("challenge").Challenge
	require.Equal(t, "ok", first.say(hookMsg{T: "prove", Challenge: challenge}).T)
	before, ok := ta.hookRecord()
	require.True(t, ok)
	require.Equal(t, sprint.HookHooked, before.State)
	require.NotZero(t, before.Proven)

	ta.failHookWrite(1)
	second := ta.hookOn(name)
	select {
	case _, open := <-second.lines:
		require.False(t, open, "failed replacement start closes its own connection without a hello")
	case <-time.After(time.Second):
		t.Fatal("failed replacement start left its connection open")
	}
	current := ta.hookSession()
	require.NotNil(t, current)
	assert.Equal(t, before.Session, current.id, "the old hook remains the active subscription")
	assert.False(t, current.isEnded())
	after, ok := ta.hookRecord()
	require.True(t, ok)
	assert.Equal(t, before, after, "the failed replacement does not corrupt the old durable hook record")
	r := first.say(hookMsg{T: "unhook"})
	assert.Equal(t, "ok", r.T, r.Why)
}

type hookHelloWriteFailure struct{ closed bool }

func (c *hookHelloWriteFailure) Read([]byte) (int, error) { return 0, io.EOF }
func (c *hookHelloWriteFailure) Write([]byte) (int, error) {
	return 0, errors.New("hello write failed")
}
func (c *hookHelloWriteFailure) Close() error { c.closed = true; return nil }

func TestHookStartSendFailureEndsPublishedSession(t *testing.T) {
	t.Parallel()
	const name = "hook-start-send-failure"
	ta, _ := hookSprint(t, name)
	conn := &hookHelloWriteFailure{}
	ta.a.hookRun(context.Background(), name, conn, 0)
	require.True(t, conn.closed, "failed hello closes the connection")
	rec, ok := ta.hookRecord()
	require.True(t, ok)
	assert.Equal(t, sprint.HookGone, rec.State, "published HookHooked is ended when hello cannot be sent")
	assert.Contains(t, rec.Why, "hello write failed")
	assert.Nil(t, ta.hookSession(), "failed published session is no longer current")
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

// TestHookFlushesAfterChallenge verifies that the hook's HOOK CHALLENGE line is
// readable on a real OS pipe within 1s while the hook is still running: the seat's
// monitor must see the challenge at once, not only when the hook exits. It is the
// guard for the hook's per-line flush (every line is one write(2) to stdout; there
// is no user-space buffer to flush). A regression that wrapped stdout in a
// buffer without flushing it would make this test time out.
func TestHookFlushesAfterChallenge(t *testing.T) {
	const name = "hook-flush"
	ta, env := hookSprint(t, name)
	srv := httptest.NewServer(localHandler{ta.a})
	t.Cleanup(srv.Close)
	env["NOVA_SPRINT_SERVER"] = srv.Listener.Addr().String()

	client := newApp(ta.a.getenv)
	client.hookSock = ta.a.hookSock

	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	defer pr.Close()
	done := make(chan int, 1)
	go func() {
		var errb bytes.Buffer
		code := client.run([]string{"hook"}, pw, &errb)
		_ = pw.Close()
		done <- code
	}()

	challenge := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "HOOK CHALLENGE ") {
				challenge <- sc.Text()
				return
			}
		}
	}()

	select {
	case line := <-challenge:
		assert.Contains(t, line, "nova-sprint hook --prove ")
	case <-time.After(time.Second):
		t.Fatal("HOOK CHALLENGE was not readable on the OS pipe within 1s while the hook ran")
	}

	// the hook is still running when the challenge was read: it did not exit
	select {
	case code := <-done:
		t.Fatalf("the hook exited (code %d) before the challenge was read", code)
	default:
	}

	// end the hook cleanly
	var out, errs bytes.Buffer
	code := client.run(strings.Fields("unhook"), &out, &errs)
	require.Equal(t, 0, code, out.String()+errs.String())
	select {
	case code := <-done:
		require.Equal(t, 0, code)
	case <-time.After(10 * time.Second):
		t.Fatal("the hook did not end after unhook")
	}
}
