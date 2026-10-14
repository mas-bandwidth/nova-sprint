//go:build functional

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type reconnectHTTPHook struct {
	conn    net.Conn
	lines   chan hookMsg
	mu      sync.Mutex
	n       int
	pending []hookMsg
	session string
}

func openReconnectHTTPHook(t *testing.T, addr, name string) *reconnectHTTPHook {
	t.Helper()
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(addr, "http://"), 3*time.Second)
	require.NoError(t, err)
	_, err = fmt.Fprintf(conn, "GET %s?as=%s HTTP/1.1\r\nHost: localhost\r\nUpgrade: %s\r\nConnection: Upgrade\r\n\r\n", hookPath, name, hookUpgrade)
	require.NoError(t, err)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	c := &reconnectHTTPHook{conn: conn, lines: make(chan hookMsg, 64)}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		sc := bufio.NewScanner(br)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			var m hookMsg
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				c.lines <- m
			}
		}
		close(c.lines)
	}()
	return c
}

func (c *reconnectHTTPHook) next(t *testing.T, typ string) hookMsg {
	t.Helper()
	if len(c.pending) > 0 {
		m := c.pending[0]
		c.pending = c.pending[1:]
		require.Equal(t, typ, m.T, "%+v", m)
		if m.T == "hello" {
			c.session = m.Session
		}
		return m
	}
	select {
	case m, ok := <-c.lines:
		require.True(t, ok, "HTTP hook closed while waiting for %s", typ)
		require.Equal(t, typ, m.T, "%+v", m)
		if m.T == "hello" {
			c.session = m.Session
		}
		return m
	case <-time.After(5 * time.Second):
		require.FailNowf(t, "HTTP hook did not send %s", typ)
		return hookMsg{}
	}
}

// tell writes one line that wants no answer (the hook client's keepalive).
func (c *reconnectHTTPHook) tell(t *testing.T, m hookMsg) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := json.Marshal(m)
	require.NoError(t, err)
	_, err = c.conn.Write(append(b, '\n'))
	require.NoError(t, err)
}

func (c *reconnectHTTPHook) send(t *testing.T, m hookMsg) hookMsg {
	t.Helper()
	c.mu.Lock()
	c.n++
	if m.T == "ack" && m.Session == "" {
		m.Session = c.session
	}
	m.Req = fmt.Sprintf("reconnect-%d", c.n)
	b, err := json.Marshal(m)
	require.NoError(t, err)
	_, err = c.conn.Write(append(b, '\n'))
	c.mu.Unlock()
	require.NoError(t, err)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got, ok := <-c.lines:
			require.True(t, ok, "HTTP hook closed before replying to %s", m.T)
			if got.Req == m.Req {
				return got
			}
			require.NotEmpty(t, got.T, "unexpected empty HTTP hook line while waiting for %s", m.T)
			c.pending = append(c.pending, got)
		case <-deadline:
			require.FailNowf(t, "HTTP hook did not answer %s", m.T)
			return hookMsg{}
		}
	}
}

func (c *reconnectHTTPHook) requireNoFrame(t *testing.T, d time.Duration) {
	t.Helper()
	// The waiting-judgment warning repeats every second while a judgment has
	// waited JudgmentWarnAfter or longer (sprint.JudgmentWarnings). The fixture
	// judgments are minutes old, so it is a status push that can land inside this
	// window at any time; it is not a replay, so it is skipped here. Every other
	// frame, a replayed acknowledged or held push included, still fails.
	skipWarning := func(m hookMsg) bool {
		return m.T == "push" && m.What == hookWarnWhat
	}
	deadline := time.After(d)
	for {
		if len(c.pending) > 0 {
			m := c.pending[0]
			if skipWarning(m) {
				c.pending = c.pending[1:]
				continue
			}
			require.FailNowf(t, "unexpected queued HTTP hook frame: %+v", m)
		}
		select {
		case m, ok := <-c.lines:
			if !ok {
				return
			}
			if skipWarning(m) {
				continue
			}
			require.FailNowf(t, "unexpected HTTP hook frame: %+v", m)
		case <-deadline:
			return
		}
	}
}

func TestAReconnectReplaysOnlyUnheldUnackedPushes(t *testing.T) {
	ctx := context.Background()
	addr := testutil.Start(t)
	admin := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, fn.Load(ctx, admin))
	const name = "reconnect-seat"
	home := t.TempDir()
	var clockMu sync.Mutex
	currentNow := time.Now().UTC()
	clock := func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return currentNow }
	advance := func(at time.Time) { clockMu.Lock(); currentNow = at; clockMu.Unlock() }
	env := func(k string) string {
		return map[string]string{
			"NOVA_SPRINT_REDIS":  addr,
			"NOVA_SPRINT_ACTOR":  name,
			"NOVA_SPRINT_SERVER": "http://127.0.0.1:1", // hostile ambient seat: --redis must keep fixture local
			"HOME":               home,
			"XDG_CONFIG_HOME":    filepath.Join(home, ".config"),
		}[k]
	}
	app := newApp(env)
	app.now = clock
	app.home = func() (string, error) { return home, nil }
	app.forward = func(_ context.Context, server string, verbs ...[]string) ([]sprintwire.Result, error) {
		require.FailNowf(t, "fixture command was forwarded to ambient seat %s: %q", server, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	t.Cleanup(app.close)
	var out, errs strings.Builder
	require.Equal(t, 0, runInto(app, []string{"init", "--members", "m1", "--readers", "reader-a", "--owner", name, "--redis", addr}, &out, &errs), "%s%s", out.String(), errs.String())
	app.bus = func(context.Context, bus.Message, func(string)) error { return nil }
	app.serveAddr = addr
	srv := httptest.NewServer(localHandler{app})
	t.Cleanup(srv.Close)

	st, err := app.store(common{redis: addr, actor: name})
	require.NoError(t, err)
	now := clock()
	future := now.Add(time.Hour)
	_, err = st.Run(ctx, store.Step{Verb: "persist reconnect fixtures", Plan: func(*sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Notes: []sprint.Note{
			{Kind: sprint.Judgment, Type: sprint.NTimer, To: name, Primaries: []string{"timer-subject"}, Count: 1, What: "acknowledged timer", Who: name, At: now.Add(-3 * time.Minute), Decisions: append([]string(nil), sprint.Decisions[sprint.NTimer]...)},
			{Kind: sprint.Judgment, Type: sprint.NStopDebtBeat, Primaries: []string{"m1"}, Count: 1, What: "unheld STOP debt beat", Who: name, At: now.Add(-2 * time.Minute), Decisions: append([]string(nil), sprint.Decisions[sprint.NStopDebtBeat]...)},
			{Kind: sprint.Judgment, Type: sprint.NStopDebtBeat, Primaries: []string{"m2"}, Count: 1, What: "held sibling STOP debt beat", Who: name, At: now.Add(-90 * time.Second), Review: future, ReviewSet: now, Decisions: append([]string(nil), sprint.Decisions[sprint.NStopDebtBeat]...)},
			{Kind: sprint.Judgment, Type: sprint.NStopDebtBeat, Primaries: []string{"m3"}, Count: 1, What: "held STOP debt beat", Who: name, At: now.Add(-time.Minute), Marked: true, Review: future, ReviewSet: now, Decisions: append([]string(nil), sprint.Decisions[sprint.NStopDebtBeat]...)},
		}}
	}})
	require.NoError(t, err)
	groups, err := st.Inbox(ctx, defaultDeadline, defaultStale, 10000)
	require.NoError(t, err)

	first := openReconnectHTTPHook(t, srv.URL, name)
	require.Equal(t, name, first.next(t, "hello").Name)
	challenge := first.next(t, "challenge").Challenge
	require.NotEmpty(t, challenge)
	require.Equal(t, "ok", first.send(t, hookMsg{T: "prove", Challenge: challenge}).T)
	out.Reset()
	errs.Reset()
	require.Equal(t, 0, runOnStore(app, addr, []string{"start", "--redis", addr}, &out, &errs), "running-time fixture: %s%s", out.String(), errs.String())
	require.Eventually(t, func() bool {
		h := app.hookHub()
		h.mu.Lock()
		var s *hookSession
		for _, v := range h.cur {
			s = v
			break
		}
		h.mu.Unlock()
		if s == nil {
			return false
		}
		s.look(ctx)
		return true
	}, 3*time.Second, 10*time.Millisecond)
	p1, p2 := first.next(t, "push"), first.next(t, "push")
	var acked, pending, held string
	for _, g := range groups.Groups {
		switch g.What {
		case "acknowledged timer":
			acked = g.ID
		case "unheld STOP debt beat":
			pending = g.ID
			require.Len(t, g.Notes, 2, "eligible and held notes share a persisted group")
		case "held STOP debt beat":
			held = g.ID
			require.True(t, g.Quiet, "waited STOP debt group remains quiet in the persisted inbox")
		}
	}
	require.NotEmpty(t, acked)
	require.NotEmpty(t, pending)
	require.NotEmpty(t, held)
	ids := map[string]uint64{p1.What: p1.ID, p2.What: p2.ID}
	require.Equal(t, uint64(2), uint64(len(ids)))
	require.Contains(t, ids, acked)
	require.Contains(t, ids, pending)
	require.NotContains(t, ids, held, "the held group is omitted before reconnect")
	require.Less(t, ids[acked], ids[pending], "the cumulative ACK will leave the later pending push open")
	require.Equal(t, "ok", first.send(t, hookMsg{T: "ack", ID: ids[acked]}).T)

	// Dropping the old HTTP stream commits its gone record while preserving ACKs.
	require.NoError(t, first.conn.Close())
	require.Eventually(t, func() bool {
		r, ok, e := readHook(ctx, st)
		return e == nil && ok && r.State == sprint.HookGone
	}, 3*time.Second, 10*time.Millisecond)
	// Simulate a record written before push_seq existed. On restart, the wire ID
	// intentionally overlaps the old stream's first ID; the session token must
	// still prevent its delayed ACK from clearing this fresh obligation.
	legacy, ok, err := readHook(ctx, st)
	require.NoError(t, err)
	require.True(t, ok)
	require.Greater(t, legacy.PushSeq, uint64(0))
	legacy.PushSeq = 0 // omitempty serializes the same shape as a pre-upgrade record
	require.NoError(t, writeHook(ctx, st, legacy))
	srv.Close()
	app.close()

	app2 := newApp(env)
	app2.now = clock
	app2.home = func() (string, error) { return home, nil }
	app2.forward = func(_ context.Context, server string, verbs ...[]string) ([]sprintwire.Result, error) {
		require.FailNowf(t, "reconnected fixture command was forwarded to ambient seat %s: %q", server, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	t.Cleanup(app2.close)
	app2.serveAddr = addr
	app2.bus = func(context.Context, bus.Message, func(string)) error { return nil }
	app2.hookServerStart(ctx)
	st2, err := app2.store(common{redis: addr, actor: name})
	require.NoError(t, err)
	srv2 := httptest.NewServer(localHandler{app2})
	t.Cleanup(srv2.Close)
	second := openReconnectHTTPHook(t, srv2.URL, name)
	second.next(t, "hello")
	challenge = second.next(t, "challenge").Challenge
	require.Equal(t, "ok", second.send(t, hookMsg{T: "prove", Challenge: challenge}).T)
	require.Eventually(t, func() bool {
		h := app2.hookHub()
		h.mu.Lock()
		var s *hookSession
		for _, v := range h.cur {
			s = v
			break
		}
		h.mu.Unlock()
		if s == nil {
			return false
		}
		s.look(ctx)
		return true
	}, 3*time.Second, 10*time.Millisecond)
	replayed := second.next(t, "push")
	// #93 adds a repeating warning push while a judgment waits; the fixture's judgments
	// are 1 to 3 minutes old, so it arrives too. The replay and the warning are counted
	// apart, in whatever order the server sends them; the replay checks stay unchanged.
	var warn hookMsg
	if replayed.What == hookWarnWhat {
		warn = replayed
		replayed = second.next(t, "push")
	} else {
		warn = second.next(t, "push")
	}
	require.Equal(t, pending, replayed.What)
	require.Equal(t, ids[acked], replayed.ID, "legacy record has no high-water mark, so session binding handles the numeric overlap")
	require.NotEqual(t, acked, replayed.What)
	require.NotEqual(t, held, replayed.What)
	require.Equal(t, hookWarnWhat, warn.What, "the waiting-judgment warning arrived")
	second.requireNoFrame(t, 150*time.Millisecond)
	stale := second.send(t, hookMsg{T: "ack", ID: replayed.ID, Session: first.session})
	require.Equal(t, "refused", stale.T, "the prior connection's session token cannot acknowledge a replay")
	require.Equal(t, "ok", second.send(t, hookMsg{T: "ack", ID: replayed.ID}).T)
	// Once the persisted sibling's review time arrives, its delivery generation
	// becomes eligible even though the earlier sibling in the same group was
	// ACKed before the reconnect.
	advance(future.Add(time.Second))
	// the hook client's keepalive arrives at the advanced clock, as it does every
	// minute: the transport is alive, so the jump is no lapse (#124), and no challenge
	// is asked again on a connection that stays up
	second.tell(t, hookMsg{T: "alive"})
	h := app2.hookHub()
	h.mu.Lock()
	var s *hookSession
	for _, v := range h.cur {
		s = v
		break
	}
	h.mu.Unlock()
	require.NotNil(t, s)
	require.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.acted.Before(future)
	}, 3*time.Second, 10*time.Millisecond, "the keepalive is taken")
	s.look(ctx)
	// Advancing the fake running clock also makes the deliberately heartbeat-free
	// machine's silence judgment due; account for that independent computed push.
	silent := second.next(t, "push")
	require.Equal(t, "machine:silent", silent.What)
	dueSibling := second.next(t, "push")
	require.Equal(t, pending, dueSibling.What, "due sibling remains in the same group identity")
	require.Greater(t, dueSibling.ID, replayed.ID)
	open, err := st2.B.OpenNotes(ctx)
	require.NoError(t, err)
	var stillHeld bool
	for _, o := range open {
		if o.Note.What == "held STOP debt beat" || o.Note.What == "held sibling STOP debt beat" {
			stillHeld = o.Note.Review.Equal(future)
		}
	}
	require.True(t, stillHeld, "restart preserved the held note and its review deadline")
}
