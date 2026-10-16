package main

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
	"github.com/stretchr/testify/require"
)

// inbox --wait (errata 3 amendment 8): with no tick end to come it waits out
// its --timeout on the store's clock, says so, and shows the inbox; a
// timeout of zero is refused.
func TestInboxWaitRunsOutItsTimeout(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	before := ta.a.now()
	out := ta.ok("inbox --wait --timeout 2s")
	require.Contains(t, out, "inbox --wait: nothing new in 2s", "inbox --wait with nothing to come")
	require.GreaterOrEqual(t, ta.a.now().Sub(before), 2*time.Second, "inbox --wait returned before its timeout of 2s of the store's clock") // wall-ok: the test app's fake clock, which the wait advances
	code, _, errs := ta.do("inbox --wait --timeout 0s")
	require.NotEqual(t, 0, code, "a timeout of zero: exit %d\n%s", code, errs)
	require.Contains(t, errs, "--timeout above zero", "a timeout of zero: exit %d\n%s", code, errs)
}

// accept --read-ok takes only a primary with ok reads from two different
// readers: with one ok read it accepts nothing and the primary stays in
// review; named, it is refused, saying what it lacks.
func TestAcceptReadOkRefusesOneOkRead(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 1 --one --brief-file " + proBriefFile(t))
	ta.ok("start")
	ta.ok("tick")
	ta.ok("take --as m1 --limit 5")
	ta.ok("finish --as m1 s1-1.w1@1")
	ta.ok("ask")
	ta.ok("read --as reader-a --ok --limit 10")
	require.Contains(t, ta.ok("accept --read-ok"), "moved=0", "accept --read-ok with one ok read")
	ta.ok("tick") // the pump applies the queued finish; one ok read accepts nothing
	require.Contains(t, ta.ok("card s1-1"), "stream s1   review", "s1-1 after accept --read-ok with one ok read")
	code, out, errs := ta.do("accept s1-1")
	require.NotEqual(t, 0, code, "accept s1-1 with one ok read: exit %d\n%s%s", code, out, errs)
	require.Contains(t, out+errs, "needs ok from two ok reads", "accept s1-1 with one ok read: exit %d\n%s%s", code, out, errs)
}

// QueueWait.tla Wake/Read: the server blocks on notes without either server
// line, and the client performs no one-second sleep/poll step.
type inboxNotesWake struct {
	*store.Mem
	wait func(context.Context, time.Duration) (bool, error)
}

func (b inboxNotesWake) WaitNotes(ctx context.Context, _ string, d time.Duration) (bool, error) {
	return b.wait(ctx, d)
}

func TestInboxWaitServerLongPollWakesOnTickEnd(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --members m1")
	ta.a.serveAddr = "mem:0"
	before := ta.a.now()
	steps := 0
	b := inboxNotesWake{Mem: ta.m, wait: func(_ context.Context, d time.Duration) (bool, error) {
		steps++
		require.Equal(t, 1, steps)
		require.Equal(t, waitLook, d, "existing backstop retained")
		require.True(t, ta.a.serial.TryLock(), "tick must be free to commit")
		ta.a.serial.Unlock()
		require.True(t, ta.a.lanes.line.TryLock(), "other reads must stay free")
		ta.a.lanes.line.Unlock()
		st := &store.Store{B: ta.m, Actor: "coordinator", Now: ta.a.now}
		_, err := st.Run(context.Background(), store.Step{Verb: "tick end", Plan: func(s *sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Notes: []sprint.Note{
				{Kind: sprint.Happened, Type: sprint.NMachineStarted, To: "coordinator", Who: "coordinator", What: "new pushed note", At: s.Now},
				{Kind: sprint.Happened, Type: sprint.NTickEnd, Who: sprint.MachineActor, What: "judgments=1", At: s.Now},
			}}
		}})
		return true, err
	}}
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) { return b, nil }
	client := newApp(func(k string) string {
		if k == ServerEnv {
			return "sprint.test:6390"
		}
		return ""
	})
	client.now = ta.a.now
	client.sleep = func(time.Duration) { t.Fatal("client must not poll the clock") }
	wire := sprintwire.Client{Addr: "sprint.test:6390", HTTP: &http.Client{Transport: handlerTransport{localHandler{ta.a}}}}
	client.forward = func(ctx context.Context, _ string, argv ...[]string) ([]sprintwire.Result, error) {
		return wire.Do(ctx, argv...)
	}
	var out, errs bytes.Buffer
	code := runInto(client, []string{"inbox", "--wait", "--timeout", "10m", "--json"}, &out, &errs)
	require.Zero(t, code, errs.String())
	require.Contains(t, out.String(), `"woke":true`)
	require.Contains(t, out.String(), "new pushed note")
	require.Equal(t, before, ta.a.now())
	require.Equal(t, 1, steps)
}

func TestInboxTickWaitCancellationAndFlags(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --members m1")
	ta.a.serveAddr = "mem:0"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := inboxNotesWake{Mem: ta.m, wait: func(context.Context, time.Duration) (bool, error) { cancel(); return false, ctx.Err() }}
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) { return b, nil }
	_, tail, err := ta.m.Tails(context.Background())
	require.NoError(t, err)
	argv := []string{"log", "--wait-tick", "15s", "--after", tail, "--json"}
	res := ta.a.serveCtx(ctx, sprintwire.Request{Verbs: [][]string{argv}}, true)
	require.NotZero(t, res.Results[0].Code)
	require.Contains(t, res.Results[0].Stderr, "context canceled")
	for _, suffix := range []string{"--since 1h", "--at-epoch 0", "--card x", "--stream s1", "--member m1", "--max 1", "--after $"} {
		code, _, errs := ta.do("log --wait-tick 0s --json " + suffix)
		require.Equal(t, 2, code, errs)
	}
	res = ta.a.serveCtx(context.Background(), sprintwire.Request{Verbs: [][]string{argv}}, false)
	require.Equal(t, 2, res.Results[0].Code, "worker listener must not expose coordinator notes")
}

func TestLongPollKeepsTheCoordinatorHookGate(t *testing.T) {
	t.Parallel()
	name := "queue-wait-unhooked"
	ta, _, _ := coordSprint(t, name)
	ta.m.LogWait = func(time.Duration) { t.Fatal("hook refusal must precede blocking") }
	before := ta.m.Calls["waitlog"]
	for _, argv := range [][]string{
		{"log", "--actor", name, "--wait-tick", "0s", "--json"},
		{"queue", "--as", name, "--wait", "10m", "--after", "0-0", "--json"},
	} {
		res := ta.a.serveCtx(context.Background(), sprintwire.Request{Verbs: [][]string{argv}}, true)
		require.Equal(t, 1, res.Results[0].Code)
		require.Contains(t, res.Results[0].Stderr, "You must hook in first")
		require.Equal(t, "no", res.Results[0].Changed)
	}
	require.Equal(t, before, ta.m.Calls["waitlog"])
}

// Long-poll server fixtures use the concurrent mem backend, not a file twin
// (which deliberately has no independently running writer).
func newLongPollRig(t *testing.T, lines ...string) (*serverRig, *testApp) {
	t.Helper()
	ta := newTestApp(t)
	env := ta.a.getenv
	ta.a.getenv = func(k string) string {
		if k == "NOVA_SPRINT_ACTOR" {
			return "boss"
		}
		return env(k)
	}
	for _, line := range lines {
		ta.ok(strings.TrimPrefix(line, prog+" "))
	}
	ta.a.serveAddr = "mem:0"
	return &serverRig{t: t, a: ta.a}, ta
}
