package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
	"github.com/stretchr/testify/require"
)

// QueueWait.tla: one durable deal wakes a worker with no fake-clock advance,
// directly and through the actual HTTP handler using an in-process transport.
func TestQueueWaitWakesOnTheDeal(t *testing.T) {
	t.Parallel()
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprintf("server=%v", remote), func(t *testing.T) {
			t.Parallel()
			ta := newTestApp(t)
			ta.ok("init --members m1")
			ta.ok("add --stream s1 --count 1 --one --brief-file " + proBriefFile(t))
			ta.ok("start")
			st := &store.Store{B: ta.m, Now: ta.a.now}
			tail, err := st.LogTail(context.Background())
			require.NoError(t, err)
			before := ta.a.now()
			steps := 0
			ta.m.LogWait = func(d time.Duration) {
				steps++
				require.Equal(t, 1, steps, "the deal wakes after one fake transport step")
				// A server waiter must not own the writer lock: the tick is
				// the action that creates its wake. No goroutine or wall clock.
				if remote {
					require.True(t, ta.a.serial.TryLock(), "wait holds the writer lock")
					ta.a.serial.Unlock()
				}
				ta.ok("tick")
			}
			argv := []string{"queue", "--as", "m1", "--wait", "10m", "--after", tail, "--json"}
			var out string
			if remote {
				ta.a.serveAddr = "mem:0"
				c := sprintwire.Client{Addr: "sprint.test:6390", HTTP: &http.Client{Transport: handlerTransport{ta.a}}}
				res, err := c.Do(context.Background(), argv)
				require.NoError(t, err)
				require.Len(t, res, 1)
				require.Zero(t, res[0].Code, res[0].Stderr)
				out = res[0].Stdout
			} else {
				out = ta.ok("queue --as m1 --wait 10m --after " + tail + " --json")
			}
			var got struct {
				Cards  []queueCard `json:"cards"`
				Cursor string      `json:"cursor"`
			}
			require.NoError(t, json.Unmarshal([]byte(out), &got))
			require.Len(t, got.Cards, 1)
			require.Equal(t, "s1-1.w1", got.Cards[0].ID)
			require.NotEmpty(t, got.Cursor)
			require.NotEqual(t, tail, got.Cursor)
			require.Equal(t, before, ta.a.now(), "push must not advance fake time")
			require.Equal(t, 1, steps)
		})
	}
}

func TestQueueWaitQuietDeadline(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --members m1")
	st := &store.Store{B: ta.m, Now: ta.a.now}
	tail, err := st.LogTail(context.Background())
	require.NoError(t, err)
	before := ta.a.now()
	out := ta.ok("queue --as m1 --wait 10m --after " + tail + " --json")
	var got struct {
		Cards  []queueCard `json:"cards"`
		Cursor string      `json:"cursor"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Empty(t, got.Cards)
	require.Equal(t, tail, got.Cursor)
	require.Equal(t, 10*time.Minute, ta.a.now().Sub(before))
}

// Losing the transient wake does not lose the durable deal: the unchanged
// backstop reads the log once, then returns one copy of the queue card.
type queueLostPush struct {
	*store.Mem
	wait func(time.Duration)
}

func (b queueLostPush) WaitLog(_ context.Context, after string, d time.Duration) (string, bool, error) {
	b.wait(d)
	return after, false, nil
}

func TestQueueWaitLostPushBackstop(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --members m1")
	ta.ok("add --stream s1 --count 1 --one --brief-file " + proBriefFile(t))
	ta.ok("start")
	st := &store.Store{B: ta.m, Now: ta.a.now}
	tail, err := st.LogTail(context.Background())
	require.NoError(t, err)
	before := ta.a.now()
	waits := 0
	b := queueLostPush{Mem: ta.m, wait: func(d time.Duration) {
		waits++
		require.Equal(t, waitLook, d)
		ta.ok("tick")
		ta.a.sleep(d)
	}}
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) { return b, nil }
	out := ta.ok("queue --as m1 --wait 10m --after " + tail + " --json")
	var got struct {
		Cards  []queueCard `json:"cards"`
		Cursor string      `json:"cursor"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got.Cards, 1)
	require.NotEqual(t, tail, got.Cursor)
	require.Equal(t, 1, waits)
	require.Equal(t, waitLook, ta.a.now().Sub(before))
}

// The actual HTTP handler permits ten minutes without widening the ordinary
// server timeout. The transport remains in-process and time is a value.
type queueDeadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *queueDeadlineWriter) SetWriteDeadline(d time.Time) error { w.deadline = d; return nil }

func TestQueueWaitTransportDeadlineAndCancellation(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --members m1")
	ta.a.serveAddr = "mem:0"
	argv := []string{"queue", "--as", "m1", "--wait", "10m", "--after", "0-0", "--json"}
	client := ta.a.longPollClient(argv)
	require.Equal(t, 10*time.Minute+sprintwire.Timeout, client.Timeout)
	require.Nil(t, ta.a.longPollClient([]string{"queue", "--as", "m1"}))
	body, err := json.Marshal(sprintwire.Request{Verbs: [][]string{argv}})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, sprintwire.Path, bytes.NewReader(body))
	w := &queueDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	ta.a.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, ta.a.now().Add(10*time.Minute+sprintwire.Timeout), w.deadline)

	// Cancellation occurs inside the fake blocking exchange. No reader beat
	// or queue snapshot is allowed afterwards, and neither server line is held.
	st := &store.Store{B: ta.m, Now: ta.a.now}
	tail, err := st.LogTail(context.Background())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ta.m.LogWait = func(time.Duration) { cancel() }
	argv[6] = tail
	res := ta.a.serveCtx(ctx, sprintwire.Request{Verbs: [][]string{argv}}, false)
	require.NotZero(t, res.Results[0].Code)
	require.Contains(t, res.Results[0].Stderr, "context canceled")
	require.True(t, ta.a.serial.TryLock())
	ta.a.serial.Unlock()
	require.True(t, ta.a.lanes.line.TryLock())
	ta.a.lanes.line.Unlock()
}

func TestQueueWaitRefusesMixedBatchAndBadFlags(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --members m1")
	ta.a.serveAddr = "mem:0"
	ta.m.LogWait = func(time.Duration) { t.Fatal("refused request must not block") }
	res := ta.a.serveCtx(context.Background(), sprintwire.Request{Verbs: [][]string{
		{"queue", "--as", "m1"}, {"queue", "--as", "m1", "--wait", "1m"},
	}}, false)
	require.Len(t, res.Results, 2)
	for _, r := range res.Results {
		require.Equal(t, 2, r.Code)
		require.Equal(t, "no", r.Changed)
	}
	for _, argv := range [][]string{
		{"queue", "--as", "m1", "--wait", "1m", "unexpected"},
		{"log", "--json", "--wait-tick", "1m", "unexpected"},
	} {
		res := ta.a.serveCtx(context.Background(), sprintwire.Request{Verbs: [][]string{argv}}, true)
		require.Equal(t, 2, res.Results[0].Code)
		require.Contains(t, res.Results[0].Stderr, "takes no words")
	}
	for _, flags := range []string{"--wait 0s", "--wait -1s", "--wait 1m --after nope", "--after 1-0", "--wait 1m --stream s1"} {
		code, _, _ := ta.do("queue --as m1 " + flags)
		require.Equal(t, 2, code, flags)
	}
}

type queueBetweenRead struct {
	*store.Mem
	afterRead *func()
}

func (b queueBetweenRead) AtEpoch(epoch uint64, old bool) store.Backend {
	return queueBetweenRead{Mem: b.Mem.AtEpoch(epoch, old).(*store.Mem), afterRead: b.afterRead}
}
func (b queueBetweenRead) ReadSet(ctx context.Context, table string, ids []string) (ntable.ReadSetResult, error) {
	got, err := b.Mem.ReadSet(ctx, table, ids)
	if err == nil && strings.HasSuffix(table, sprint.Fleet) && *b.afterRead != nil {
		f := *b.afterRead
		*b.afterRead = nil
		f()
	}
	return got, err
}

// Exact Go counterpart of MCQueueWaitBrokenCursorSkips: a deal commits after
// the fleet snapshot. Its cursor must remain pending for the following wait.
func TestQueueWaitCursorDoesNotSkipConcurrentDeal(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --members m1")
	ta.ok("add --stream s1 --count 1 --one --brief-file " + proBriefFile(t))
	ta.ok("start")
	st := &store.Store{B: ta.m, Now: ta.a.now}
	tail, err := st.LogTail(context.Background())
	require.NoError(t, err)
	f := func() { ta.ok("tick") }
	b := queueBetweenRead{Mem: ta.m, afterRead: &f}
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) { return b, nil }
	var first struct {
		Cards  []queueCard `json:"cards"`
		Cursor string      `json:"cursor"`
	}
	require.NoError(t, json.Unmarshal([]byte(ta.ok("queue --as m1 --json")), &first))
	require.Empty(t, first.Cards)
	require.Nil(t, f, "deal interleaving actually ran")
	require.Equal(t, tail, first.Cursor, "never sample the tail after the queue snapshot")
	ta.m.LogWait = func(time.Duration) { t.Fatal("pending deal must wake immediately") }
	before := ta.a.now()
	var next struct {
		Cards  []queueCard `json:"cards"`
		Cursor string      `json:"cursor"`
	}
	require.NoError(t, json.Unmarshal([]byte(ta.ok("queue --as m1 --wait 10m --after "+first.Cursor+" --json")), &next))
	require.Len(t, next.Cards, 1)
	require.NotEqual(t, first.Cursor, next.Cursor)
	require.Equal(t, before, ta.a.now())
}

func TestQueueWaitForwardedActor(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --members m1")
	ta.ok("add --stream s1 --count 1 --one --brief-file " + proBriefFile(t))
	ta.ok("start")
	ta.a.serveAddr = "mem:0"
	st := &store.Store{B: ta.m, Now: ta.a.now}
	tail, err := st.LogTail(context.Background())
	require.NoError(t, err)
	ta.m.LogWait = func(time.Duration) { ta.ok("tick") }
	client := newApp(func(k string) string {
		switch k {
		case ServerEnv:
			return "sprint.test:6390"
		case "NOVA_SPRINT_ACTOR":
			return "m1"
		}
		return ""
	})
	client.transport = handlerTransport{localHandler{ta.a}}
	var out, errs bytes.Buffer
	code := runInto(client, []string{"queue", "--as", "m1", "--wait", "10m", "--after", tail, "--json"}, &out, &errs)
	require.Zero(t, code, errs.String())
	require.Contains(t, out.String(), `"id":"s1-1.w1"`)
	require.Contains(t, out.String(), `"cursor":`)
}
