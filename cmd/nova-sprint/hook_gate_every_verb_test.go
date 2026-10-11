package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
)

// The owner, 2026-10-11: "If you are not hooked in to push notifications, every verb
// except "nova-sprint hook" must fail, and tell you to hook in." For the coordinator
// with no live, proven hook, every verb of the help registry (verbs) fails with exit
// 1 and the one line that starts "You must hook in first: run nova-sprint hook" and
// leads on to the exact commands (the breadcrumb), and nothing is written. The table is
// the whole registry, so a verb added later is covered by construction. Exempt: hook
// and unhook (the way in and out; help and version are not verbs of the registry and
// run before the gate), and the machine's verbs, which are the run loop's and the
// server's own and never the caller's (the hook is a stream from the server, so the
// verb that starts the server cannot wait on it).
func TestEveryVerbRefusesTheCoordinatorUntilHooked(t *testing.T) {
	t.Parallel()
	const name = "hook-every"
	ta, _ := hookSprint(t, name)

	var refused, ran, crumbless []string
	for _, v := range verbs {
		if hookExempt[v.name] || verbClasses[v.name] == classMachine {
			continue
		}
		before := ta.applies()
		code, out, errs := ta.doLined(v.name)
		line := strings.TrimRight(errs, "\n")
		ok := code == 1 && out == "" && !strings.Contains(line, "\n") &&
			strings.HasPrefix(line, sprint.HookFirst) &&
			strings.Contains(line, "nova-sprint hook --actor "+name) &&
			strings.Contains(line, "nova-sprint hook --prove ") &&
			strings.Contains(line, "nova-sprint unhook --actor "+name) &&
			before == ta.applies()
		if ok {
			refused = append(refused, v.name)
			continue
		}
		if code == 1 && strings.HasPrefix(line, sprint.HookFirst) {
			crumbless = append(crumbless, v.name)
			continue
		}
		ran = append(ran, v.name+" (exit "+strconv.Itoa(code)+": "+gateFirstLine(out+errs)+")")
	}
	assert.Empty(t, ran, "verbs the unhooked coordinator ran past the gate")
	assert.Empty(t, crumbless, "verbs refused without the breadcrumb (role, push, and the exact commands)")
	assert.NotEmpty(t, refused)
	// the way in and out is never refused by the gate
	for _, v := range []string{"hook", "unhook"} {
		assert.True(t, slices.ContainsFunc(verbs, func(x verb) bool { return x.name == v }), v)
		assert.True(t, hookExempt[v], v)
	}
}

func gateFirstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}

// hookCLI is the caller's side as a process of its own: the server listens on the twin
// (localHandler), and the caller's verbs, its hook and its answers run through run, on
// the same bus store and clock.
type hookCLI struct {
	t      *testing.T
	client *app
}

func (ta *testApp) hookCLI(env map[string]string) *hookCLI {
	srv := httptest.NewServer(localHandler{ta.a})
	ta.t.Cleanup(srv.Close)
	env["NOVA_SPRINT_SERVER"] = srv.Listener.Addr().String()
	client := newApp(ta.a.getenv)
	client.hookSock = ta.a.hookSock
	client.busOpen, client.now = ta.a.busOpen, ta.a.now
	return &hookCLI{t: ta.t, client: client}
}

func (h *hookCLI) run(argv ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := h.client.run(argv, &out, &errb)
	return code, out.String(), errb.String()
}

// heldHook is a hook started by a command line (argv after "nova-sprint"), its
// stdout read a line at a time.
type heldHook struct {
	t     *testing.T
	lines chan string
	done  chan int
}

func (h *hookCLI) hold(argv []string) *heldHook {
	pr, pw := io.Pipe()
	hh := &heldHook{t: h.t, lines: make(chan string, 1024), done: make(chan int, 1)}
	go func() {
		var errb bytes.Buffer
		code := h.client.run(argv, pw, &errb)
		_ = pw.CloseWithError(io.EOF)
		hh.done <- code
	}()
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			hh.lines <- sc.Text()
		}
		close(hh.lines)
	}()
	return hh
}

// next is the next line of the hook's stdout that starts with prefix, within a bound.
func (hh *heldHook) next(prefix string) string {
	hh.t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case l, ok := <-hh.lines:
			if !ok {
				hh.t.Fatalf("the hook ended, wanting a line %q", prefix)
			}
			if strings.HasPrefix(l, prefix) {
				return l
			}
		case <-deadline:
			hh.t.Fatalf("no line %q", prefix)
		}
	}
}

func (hh *heldHook) ended() int {
	hh.t.Helper()
	select {
	case code := <-hh.done:
		return code
	case <-time.After(20 * time.Second):
		hh.t.Fatal("the hook did not end")
	}
	return -1
}

// crumb is the command the breadcrumb names after marker, up to the next ';' or '(',
// as the words after "nova-sprint".
func crumb(t *testing.T, line, marker string) []string {
	t.Helper()
	_, rest, ok := strings.Cut(line, marker)
	require.True(t, ok, "the breadcrumb names no %q: %s", marker, line)
	rest = strings.TrimSpace(rest)
	if i := strings.IndexAny(rest, ";("); i >= 0 {
		rest = rest[:i]
	}
	words := strings.Fields(rest)
	require.NotEmpty(t, words)
	require.Equal(t, "nova-sprint", words[0], line)
	return words[1:]
}

// The breadcrumb is the way in, run as it is written (the owner: "the error leads you
// with breadcrumbs to what you need to do -- which is to HOOK IN (here is exactly
// how)"): the refusal names the role, which push is not live, and the commands; the
// hook it names, run on the twin, arms both pushes; its challenge answered with the
// --prove line it names proves both, and the verb runs. The stop it names, unhook,
// closes both, and the same verb is refused again; hook again, and it runs again
// (functional: hook, a verb runs; unhook, it refuses; re-hook, it runs).
func TestTheBreadcrumbHooksTheCallerIn(t *testing.T) {
	t.Parallel()
	const name = "hook-crumb"
	ta, env := hookSprint(t, name)
	cli := ta.hookCLI(env)

	code, out, errs := cli.run("where")
	require.Equal(t, 1, code, out+errs)
	line := strings.TrimSpace(errs)
	require.True(t, strings.HasPrefix(line, sprint.HookFirst), line)
	assert.Contains(t, line, "the sprint push is not live")
	assert.Contains(t, line, "the coordinator "+name+" wants the sprint push and the bus push live")
	assert.Contains(t, line, "nova-hook hook --as "+name+" --role coordinator")

	hookIn := func() *heldHook {
		hh := cli.hold(crumb(t, line, "You must hook in first: run"))
		assert.Contains(t, hh.next("HOOK CONNECTED"), "role=coordinator")
		chl := hh.next("HOOK CHALLENGE ")
		chal := strings.TrimSuffix(strings.Fields(chl)[2], ":")
		require.Contains(t, chal, ".", "one answer over both pushes: <sprint>.<bus>")
		// a half is not an answer
		sprintHalf, _, _ := strings.Cut(chal, ".")
		code, _, errs := cli.run("hook", "--prove", sprintHalf, "--actor", name)
		require.Equal(t, 1, code)
		assert.Contains(t, errs, "both halves")
		prove := crumb(t, line, "then answer the HOOK CHALLENGE it prints:")
		for i, w := range prove {
			if w == "<challenge>" {
				prove[i] = chal
			}
		}
		code, out, errs = cli.run(prove...)
		require.Equal(t, 0, code, out+errs)
		hh.next("HOOK PROVEN")
		return hh
	}

	hh := hookIn()
	code, out, errs = cli.run("where")
	require.Equal(t, 0, code, out+errs)
	rec, ok, err := ta.hookShared().Shared(context.Background(), name)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, sprint.HookRoleCoordinator, rec.Role)
	assert.Equal(t, []string{sprint.HookPushSprint, sprint.HookPushBus}, rec.Pushes)

	// a message to the caller on the bus is pushed into the hook's stream
	require.NoError(t, ta.a.hk.bus.Store.AddAll(context.Background(), []string{bus.StreamOf(name)},
		bus.Message{ID: "m1", From: "glenn", To: []string{name}, Subject: "hello rowan", At: ta.a.now()}.Fields()))
	assert.Contains(t, hh.next("BUS "), `subject="hello rowan"`)

	code, out, errs = cli.run(crumb(t, line, "to stop:")...)
	require.Equal(t, 0, code, out+errs)
	assert.Equal(t, 0, hh.ended())
	rec, _, err = ta.hookShared().Shared(context.Background(), name)
	require.NoError(t, err)
	assert.False(t, rec.UnhookedAt.IsZero(), "unhook is recorded in the shared record")
	code, _, errs = cli.run("where")
	require.Equal(t, 1, code)
	assert.True(t, strings.HasPrefix(errs, sprint.HookFirst), errs)

	hh = hookIn()
	code, out, errs = cli.run("where")
	require.Equal(t, 0, code, out+errs)
	_, _, _ = cli.run("unhook", "--actor", name)
	hh.ended()
}

// The coordinator's two pushes, each required (the owner: "For a coordinator, this is
// hooking into to sprint push notifications and message bus push notifications."):
// the sprint push proven and the bus push not is refused naming the bus push; the bus
// push proven and the sprint push not, naming the sprint push; both, and it runs; the
// bus push unhooked, refused again.
func TestTheCoordinatorWantsBothPushes(t *testing.T) {
	t.Parallel()
	const name = "hook-both"
	ta, _ := hookSprint(t, name)
	busOnly := sprint.HookBreadcrumb(sprint.HookRoleCoordinator, name, sprint.HookPushSprint, "")
	sprintOnly := sprint.HookBreadcrumb(sprint.HookRoleCoordinator, name, sprint.HookPushBus, "")
	assert.Contains(t, busOnly, "the sprint push is not live")
	assert.Contains(t, sprintOnly, "the bus push is not live")

	ta.busProven(name, sprint.HookRoleCoordinator)
	ta.refusedFirst("where", busOnly)

	c := ta.hookOn(name)
	assert.Equal(t, sprint.HookRoleCoordinator, c.next("hello").Role)
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
	ta.lined(func() { ta.ok("where") })

	require.NoError(t, ta.hookShared().Unhook(context.Background(), name, ta.a.now()))
	ta.refusedFirst("where", sprintOnly)
	// armed for the other role is not armed for this one
	require.NoError(t, ta.hookShared().Arm(context.Background(), name, sprint.HookRoleWorker, ta.a.now()))
	require.NoError(t, ta.hookShared().Prove(context.Background(), name, ta.a.now()))
	ta.refusedFirst("where", sprintOnly)
	ta.busProven(name, sprint.HookRoleCoordinator)
	ta.lined(func() { ta.ok("where") })
	// past its bound unanswered, the bus push lapses
	ta.step(sprint.HookSharedBound + time.Second)
	code, _, errs := ta.doLined("where")
	assert.Equal(t, 1, code)
	assert.True(t, strings.HasPrefix(errs, sprint.HookFirst), errs)
}

// A worker friend (a name on the friends table) is gated on its two pushes, the bus
// push and the work push, for its worker verbs (the owner: "For a friend in sprint as
// worker, it is message bus and work notifications as push."); its beats run, as do
// another worker's verbs and the friend's reads. Its hook is the server's work stream
// and the bus: hello names the worker role, and a proof keeps no seat record.
func TestAWorkerFriendWantsItsBusAndWorkPushes(t *testing.T) {
	t.Parallel()
	const seat, friend = "hook-seat-w", "hook-zhi"
	ta, _ := hookSprint(t, seat)
	hookTests.Store(friend, true)
	t.Cleanup(func() { hookTests.Delete(friend) })
	ta.lined(func() {
		st, err := ta.a.store(common{redis: "mem:0"})
		require.NoError(t, err)
		_, _, _, err = st.SyncFriends(context.Background(), []store.FriendSpec{{Name: friend, Width: 1, Class: "flash"}})
		require.NoError(t, err)
	})
	want := sprint.HookBreadcrumb(sprint.HookRoleWorker, friend, sprint.HookPushBus, "")
	assert.Contains(t, want, "the worker "+friend+" wants the bus push and the work push live")
	for _, line := range []string{"take --as " + friend, "finish --as " + friend + " c-1", "queue --as " + friend, "progress --as " + friend} {
		ta.refusedFirst(line, want)
	}
	// its beats, another worker's verbs, and its reads are not its work
	code, out, errs := ta.doLined("friend beat --as " + friend)
	assert.NotContains(t, out+errs, sprint.HookFirst, code)
	_, out, errs = ta.doLined("take --as m1")
	assert.NotContains(t, out+errs, sprint.HookFirst)
	_, out, errs = ta.doLined("where --actor " + friend)
	assert.NotContains(t, out+errs, sprint.HookFirst)

	// the server's side of its hook: the worker role, its proof keeps no seat record
	c := ta.hookOn(friend)
	assert.Equal(t, sprint.HookRoleWorker, c.next("hello").Role)
	r := c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge})
	require.Equal(t, "ok", r.T, r.Why)
	rec, ok := ta.hookRecord()
	assert.False(t, ok && rec.Name == friend, "a worker's hook writes no seat record: %+v", rec)
	ta.busProven(friend, sprint.HookRoleWorker)
	_, out, errs = ta.doLined("take --as " + friend)
	assert.NotContains(t, out+errs, sprint.HookFirst)
	// armed as a coordinator is not armed as a worker
	ta.busProven(friend, sprint.HookRoleCoordinator)
	ta.refusedFirst("take --as "+friend, want)
	// a name that is neither the seat's nor a friend's has no hook on this server
	d := ta.hookOn("nobody-here")
	assert.Contains(t, d.next("bye").Why, "is neither")
}

// Fail closed (the owner: "Fail closed, not open."): a caller the gate judges is refused
// with the reason when the bus store is not named (NOVA_BUS_REDIS unset), when the
// sprint store does not open, and when no sprint store is named; and `hook` itself
// refuses to start, arming nothing, with no bus store to arm the bus push on.
func TestTheGateFailsClosed(t *testing.T) {
	t.Parallel()
	const name = "hook-closed"
	ta, env := hookSprint(t, name)
	c := ta.hookOn(name)
	c.next("hello")
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
	ta.busProven(name, sprint.HookRoleCoordinator)
	ta.lined(func() { ta.ok("where") })

	// a fresh process with no bus store named cannot read the bus push's record
	delete(env, busRedisEnv)
	fresh := newApp(ta.a.getenv)
	fresh.backend, fresh.now, fresh.busOpen = ta.a.backend, ta.a.now, ta.a.busOpen
	var out, errb bytes.Buffer
	code := fresh.run([]string{"where", "--redis", "mem:0"}, &out, &errb)
	assert.Equal(t, 1, code, out.String()+errb.String())
	assert.True(t, strings.HasPrefix(errb.String(), sprint.HookFirst), errb.String())
	assert.Contains(t, errb.String(), busRedisEnv+" is not set")

	// hook with no bus store refuses and arms nothing
	env["NOVA_SPRINT_SERVER"] = "127.0.0.1:1"
	out.Reset()
	errb.Reset()
	code = fresh.run([]string{"hook", "--actor", name}, &out, &errb)
	assert.Equal(t, 2, code, out.String()+errb.String())
	assert.Contains(t, errb.String(), "the bus push cannot be armed")
	assert.Contains(t, errb.String(), "nothing was armed")
	env[busRedisEnv] = "bus:test"
	delete(env, "NOVA_SPRINT_SERVER")

	// a sprint store that does not open refuses with why
	broken := newApp(ta.a.getenv)
	broken.now, broken.busOpen = ta.a.now, ta.a.busOpen
	broken.backend = func(context.Context, string, sprint.Names) (store.Backend, error) {
		return nil, errors.New("connection refused")
	}
	out.Reset()
	errb.Reset()
	code = broken.run([]string{"where", "--redis", "mem:0"}, &out, &errb)
	assert.Equal(t, 1, code, out.String()+errb.String())
	assert.True(t, strings.HasPrefix(errb.String(), sprint.HookFirst), errb.String())
	assert.Contains(t, errb.String(), "did not open, so the hook cannot be checked")
	assert.Contains(t, errb.String(), "connection refused")
}

// warningAnswer is the first command the warning names to answer its first judgment, as
// the words after "nova-sprint".
func warningAnswer(t *testing.T, text string) []string {
	t.Helper()
	for _, l := range strings.Split(text, "\n") {
		if !strings.HasPrefix(l, "  ") {
			continue
		}
		_, cmdline, ok := strings.Cut(strings.TrimSpace(l), ": ")
		require.True(t, ok, l)
		first, _, _ := strings.Cut(cmdline, " && ")
		words := strings.Fields(first)
		require.Equal(t, "nova-sprint", words[0], l)
		return words[1:]
	}
	t.Fatalf("the warning names no answer: %s", text)
	return nil
}

// The waiting-judgment warning is a push over the hook (the owner, 2026-10-11: "Any
// card waiting for judgement should alert you via warning every second once it has been
// in this state for 10 seconds or longer." "These must be push notifications."): a
// judgment open 9 s, nothing; at 10 s one warning push in that look naming it and the
// command that answers it; at 11 s again; an ack of the push does not silence it;
// answered with the command it names, none.
func TestAWaitingJudgmentIsPushedAsAWarningEverySecond(t *testing.T) {
	t.Parallel()
	const name = "hook-warn"
	ta, _ := hookSprint(t, name)
	c := ta.hookOn(name)
	c.next("hello")
	require.Equal(t, "ok", c.say(hookMsg{T: "prove", Challenge: c.next("challenge").Challenge}).T)
	ta.busProven(name, sprint.HookRoleCoordinator)

	ta.lined(func() { ta.ok("add --stream s1 --count 2") })
	ta.lined(func() { ta.ok("start") }) // the machine runs, so its tick raises the timer
	ta.lined(func() { ta.ok("remind --in 1m --note look-here --for " + name) })
	ta.step(2 * time.Minute) // past its due time: the tick raises the timer's judgment now
	ta.lined(func() { ta.ok("tick") })
	s := ta.hookSession()
	require.NotNil(t, s)
	ctx := context.Background()
	s.look(ctx)
	p := c.next("push")
	require.NotEqual(t, hookWarnWhat, p.What, "the judgment itself first: %+v", p)

	ta.step(9 * time.Second)
	s.look(ctx)
	c.quiet("9 s: no warning")

	ta.step(time.Second)
	s.look(ctx)
	w := c.next("push")
	require.Equal(t, hookWarnWhat, w.What, "%+v", w)
	assert.Contains(t, w.Text, "WARNING 1 judgment(s) waiting on the coordinator 10s or longer")
	assert.Contains(t, w.Text, "waited=10s")
	c.quiet("one warning push a look")

	ta.step(time.Second)
	s.look(ctx)
	w2 := c.next("push")
	require.Equal(t, hookWarnWhat, w2.What)
	assert.Contains(t, w2.Text, "waited=11s")
	assert.Greater(t, w2.ID, w.ID, "each warning is a push of its own number")

	// an ack does not silence it, and a warning never blocks the gate
	require.Equal(t, "ok", c.say(hookMsg{T: "ack", ID: w2.ID}).T)
	ta.step(time.Second)
	s.look(ctx)
	w3 := c.next("push")
	require.Equal(t, hookWarnWhat, w3.What)
	ta.lined(func() { ta.ok("where") })

	// answered with the command it names: no more warnings
	ta.lined(func() { ta.ok(strings.Join(warningAnswer(t, w3.Text), " ")) })
	ta.step(time.Second)
	s.look(ctx)
	c.quiet("answered: no warning")
}

// The same warning as the seat sees it: `nova-sprint hook` on a twin server prints the
// PUSH line of the warning each second (the server's look, real time), naming the
// judgment, with no hand poll.
func TestTheWarningArrivesAsAPushLineOnTheHook(t *testing.T) {
	t.Parallel()
	const name = "hook-warn-cli"
	ta, env := hookSprint(t, name)
	cli := ta.hookCLI(env)
	hh := cli.hold([]string{"hook", "--actor", name})
	chl := hh.next("HOOK CHALLENGE ")
	chal := strings.TrimSuffix(strings.Fields(chl)[2], ":")
	code, out, errs := cli.run("hook", "--prove", chal, "--actor", name)
	require.Equal(t, 0, code, out+errs)

	ta.lined(func() { ta.ok("add --stream s1 --count 2") })
	ta.lined(func() { ta.ok("start") }) // the machine runs, so its tick raises the timer
	ta.lined(func() { ta.ok("remind --in 1m --note look-here --for " + name) })
	ta.step(2 * time.Minute) // past its due time: the tick raises the timer's judgment now
	ta.lined(func() { ta.ok("tick") })
	ta.step(10 * time.Second)
	first := hh.next("PUSH ")
	if !strings.Contains(first, hookWarnWhat) {
		first = hh.next("PUSH ")
	}
	assert.Contains(t, first, "PUSH ")
	assert.Contains(t, first, hookWarnWhat+": answer the judgments below; an ack does not silence it")
	assert.Contains(t, hh.next("  WARNING "), "judgment(s) waiting on the coordinator 10s or longer")
	// again, a second later, as a push of its own
	again := hh.next("PUSH ")
	assert.Contains(t, again, hookWarnWhat)
	assert.NotEqual(t, first, again)
	_, _, _ = cli.run("unhook", "--actor", name)
	hh.ended()
}
