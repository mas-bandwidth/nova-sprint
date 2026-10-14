//go:build functional

package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
)

// hlLines is the hook's stdout as the seat's Monitor reads it: every line, as it comes.
type hlLines struct {
	mu              sync.Mutex
	lines           []string
	holdNextPushes  bool
	pushesSeen      int
	publishedFirst  bool
	held            []string
	heldPushesTotal int
}

func (h *hlLines) add(l string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.holdNextPushes && ackableHookPush(l) {
		h.pushesSeen++
		if !h.publishedFirst {
			h.held = append(h.held, l)
			if h.pushesSeen == 2 {
				h.lines = append(h.lines, h.held[0])
				h.held = h.held[1:]
				h.publishedFirst = true
				h.heldPushesTotal++
			}
			return
		}
		h.held = append(h.held, l)
		h.heldPushesTotal++
		return
	}
	h.lines = append(h.lines, l)
}

// holdLaterPushes drains the hook's stream until two ackable pushes are in hand,
// publishes only the first, and keeps the rest hidden until the test releases them.
// This makes the record-before-frames ordering observable without blocking the hook
// process or its pipe writer.
func (h *hlLines) holdLaterPushes() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.holdNextPushes, h.pushesSeen, h.publishedFirst = true, 0, false
}

func (h *hlLines) releaseHeldPushes() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.holdNextPushes = false
	h.lines = append(h.lines, h.held...)
	h.held = nil
	h.publishedFirst = false
}

func (h *hlLines) heldPushCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.heldPushesTotal
}

func ackableHookPush(line string) bool {
	f := strings.Fields(line)
	if len(f) < 3 || f[0] != "PUSH" {
		return false
	}
	for _, w := range f[2:] {
		if strings.HasPrefix(strings.TrimSuffix(w, ":"), "session=") {
			return true
		}
	}
	return false
}

func (h *hlLines) all() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.lines...)
}

// count is how many lines so far begin with prefix.
func (h *hlLines) count(prefix string) int {
	n := 0
	for _, l := range h.all() {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

// The seat, 2026-10-11/12, on the live server: the hook dropped about 15 minutes after each
// proof although its stream stayed connected and the seat kept acting, and every lapse
// replayed ~100 pushes as unacknowledged. A real server (its HTTP listener and the hook's
// upgrade), a real store (a redis-server of its own, the function library loaded), and the
// hook held by `nova-sprint hook` in a client process of its own, on the server's clock
// stepped past three 15-minute bounds while the seat answers its pushes: the hook never
// lapses, no challenge comes after the first, and an unhook and hook again replays no push
// the seat acknowledged. The environment is the store's address, the server's and the
// seat's name, HOME and XDG_CONFIG_HOME in the test's own directory, nothing else.
func TestAHookStaysLiveAcrossThreeBoundsWhileItsSeatActs(t *testing.T) {
	t.Parallel()
	began := time.Now()
	ctx := context.Background()
	const name = "seat-hl"
	addr := testutil.Start(t)
	busAddr := testutil.Start(t)
	admin := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, fn.Load(ctx, admin))
	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, busRedisEnv: busAddr, "NOVA_SPRINT_ACTOR": name, "HOME": home, "XDG_CONFIG_HOME": home}
	var envMu sync.Mutex
	getenv := func(k string) string { envMu.Lock(); defer envMu.Unlock(); return env[k] }
	hookTests.Store(name, true)
	t.Cleanup(func() { hookTests.Delete(name) })

	// the server, on a clock the test steps
	srvApp := newApp(getenv)
	t.Cleanup(srvApp.close)
	var clockMu sync.Mutex
	now := time.Now().UTC().Truncate(time.Second)
	srvApp.now = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now }
	step := func(d time.Duration) { clockMu.Lock(); now = now.Add(d); clockMu.Unlock() }
	var sentMu sync.Mutex
	var sent []bus.Message
	srvApp.bus = func(_ context.Context, m bus.Message, _ func(string)) error {
		sentMu.Lock()
		defer sentMu.Unlock()
		sent = append(sent, m)
		return nil
	}
	srvApp.serveAddr = addr
	srvApp.hookSock = shortSock(t)
	direct := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := runOnStore(srvApp, addr, split(line), &out, &errb)
		require.Equal(t, 0, code, "%s: %s%s", line, out.String(), errb.String())
		return out.String()
	}
	// the first init on a store with no holder is never refused
	direct("init --readers reader-a --members m1 --owner glenn")
	srv := httptest.NewServer(localHandler{srvApp})
	t.Cleanup(srv.Close)

	// the seat's side holds no store: every coordinator verb goes to the server
	clientEnv := map[string]string{"NOVA_SPRINT_SERVER": srv.Listener.Addr().String(), busRedisEnv: busAddr, "NOVA_SPRINT_ACTOR": name, "HOME": home, "XDG_CONFIG_HOME": home}
	client := newApp(func(k string) string { return clientEnv[k] })
	t.Cleanup(client.close)
	client.hookSock = srvApp.hookSock
	seat := func(line string) (int, string) {
		var out, errb bytes.Buffer
		code := runInto(client, split(line), &out, &errb)
		return code, out.String() + errb.String()
	}
	hookIn := func() (*hlLines, chan int) {
		t.Helper()
		lines := &hlLines{}
		pr, pw := io.Pipe()
		done := make(chan int, 1)
		go func() {
			code := runInto(client, []string{"hook"}, pw, io.Discard)
			_ = pw.CloseWithError(io.EOF)
			done <- code
		}()
		go func() {
			sc := bufio.NewScanner(pr)
			for sc.Scan() {
				lines.add(sc.Text())
			}
		}()
		var chal string
		require.Eventually(t, func() bool {
			for _, l := range lines.all() {
				if strings.HasPrefix(l, "HOOK CHALLENGE ") {
					chal = strings.TrimSuffix(strings.Fields(l)[2], ":")
					return true
				}
			}
			return false
		}, 10*time.Second, 20*time.Millisecond, "the challenge")
		code, out := seat("hook --prove " + chal)
		require.Equal(t, 0, code, out)
		return lines, done
	}
	// pushIDs is the push ids the stream showed, in order. The repeating
	// waiting-judgment warning is not one of them: it carries no session token and
	// is not a replay, so it is skipped.
	pushIDs := func(lines *hlLines) []uint64 {
		var out []uint64
		for _, l := range lines.all() {
			if f := strings.Fields(l); len(f) > 1 && f[0] == "PUSH" {
				if len(f) > 2 && strings.TrimSuffix(f[2], ":") == hookWarnWhat {
					continue
				}
				id, _ := strconv.ParseUint(f[1], 10, 64)
				out = append(out, id)
			}
		}
		return out
	}

	// sessionOf is the session token the PUSH line of id names (#115: an acknowledgement
	// is bound to the hook session that delivered the push)
	sessionOf := func(lines *hlLines, id uint64) string {
		for _, l := range lines.all() {
			f := strings.Fields(l)
			if len(f) < 3 || f[0] != "PUSH" || f[1] != strconv.FormatUint(id, 10) {
				continue
			}
			for _, w := range f[2:] {
				if tok, ok := strings.CutPrefix(strings.TrimSuffix(w, ":"), "session="); ok {
					return tok
				}
			}
		}
		t.Fatalf("no session token on PUSH %d", id)
		return ""
	}

	lines, done := hookIn()
	lines.holdLaterPushes()
	code, out := seat("add --stream s1 --count 1 --one")
	require.Equal(t, 0, code, out)
	require.NoError(t, recordCleanCardCheck(srvApp, addr)) // the rig's check before a start
	code, out = seat("start")
	require.Equal(t, 0, code, out)

	// three bounds and more: each turn the seat acts (a verb, or an acknowledgement of a
	// push it was sent), then 14 minutes pass and the server's look runs
	var acked uint64
	var sawPendingBeforeRelease bool
	for turn := 1; turn <= 4; turn++ {
		code, out = seat("remind --in 1m --note turn-" + strconv.Itoa(turn) + " --for " + name)
		require.Equal(t, 0, code, "turn %d: %s", turn, out)
		step(2 * time.Minute)
		direct("tick")
		require.Eventually(t, func() bool { ids := pushIDs(lines); return len(ids) > 0 && ids[len(ids)-1] > acked },
			10*time.Second, 20*time.Millisecond, "turn %d: a push:\n%s", turn, strings.Join(lines.all(), "\n"))
		// A look records every new push before writing its frames. The first frame
		// can reach the stream before the later frames from that same look, so ACK
		// the visible maximum until the server confirms no recorded push remains.
		// Otherwise a later frame can be pending for the whole silent-bound step.
		var ackOut string
		require.Eventually(t, func() bool {
			ids := pushIDs(lines)
			if len(ids) == 0 {
				return false
			}
			acked = ids[len(ids)-1]
			code, ackOut = seat("hook --ack " + strconv.FormatUint(acked, 10) + " --session " + sessionOf(lines, acked))
			require.Equal(t, 0, code, "turn %d: %s\n%s", turn, ackOut, strings.Join(lines.all(), "\n"))
			if turn == 1 && !sawPendingBeforeRelease && !strings.Contains(ackOut, "unacked=0") {
				sawPendingBeforeRelease = true
				lines.releaseHeldPushes()
			}
			return strings.Contains(ackOut, "unacked=0")
		}, 10*time.Second, 20*time.Millisecond, "turn %d: all delivered pushes acknowledged; last ACK: %s\n%s",
			turn, ackOut, strings.Join(lines.all(), "\n"))
		// and the seat answers the reminder's judgment, so the next reminder is one of its own
		for _, l := range lines.all() {
			if f := strings.Fields(l); len(f) > 2 && f[0] == "PUSH" && f[1] == strconv.FormatUint(acked, 10) {
				seat("ack " + strings.TrimSuffix(f[2], ":") + " --reason seen")
			}
		}
		step(12 * time.Minute)
		time.Sleep(hookLook + 500*time.Millisecond) // the server's look at the stepped clock
		code, out = seat("where")
		require.Equal(t, 0, code, "turn %d, %s of sprint clock: the hook lapsed: %s", turn, time.Duration(turn)*14*time.Minute, out)
		if turn == 1 {
			require.True(t, sawPendingBeforeRelease, "the first visible frame's ACK must expose later recorded pushes")
			require.Greater(t, lines.heldPushCount(), 0, "the fixture held later push frames while continuing to drain the stream")
			require.GreaterOrEqual(t, len(pushIDs(lines)), 2, "the later push is visible before the silent-bound step")
		}
	}
	assert.Equal(t, 1, lines.count("HOOK CHALLENGE "), "a challenge only on the new connection")
	assert.Zero(t, lines.count("HOOK BYE"), "no lapse")
	sentMu.Lock()
	for _, m := range sent {
		assert.NotContains(t, m.Subject, "is gone", "the owner was told of a lapse")
	}
	sentMu.Unlock()

	// every push acknowledged, then unhook and hook again with nothing new since: the new
	// subscription sends nothing (before, it sent every open group again as unacknowledged)
	time.Sleep(hookLook + 500*time.Millisecond)
	ids := pushIDs(lines)
	code, out = seat("hook --ack " + strconv.FormatUint(ids[len(ids)-1], 10) + " --session " + sessionOf(lines, ids[len(ids)-1]))
	require.Equal(t, 0, code, out)
	code, out = seat("unhook")
	require.Equal(t, 0, code, out)
	<-done
	again, _ := hookIn()
	time.Sleep(hookLook + 500*time.Millisecond)
	// replay is #115's durable acknowledgement: a reconnect replays only the pushes not
	// acknowledged. The count is of open pushes: a repeating warning carries no session
	// token and is not a replay, so it is not counted.
	assert.Zero(t, len(pushIDs(again)), "a reconnect replays only the pushes not acknowledged:\n%s", strings.Join(again.all(), "\n"))
	assert.Less(t, time.Since(began), 30*time.Second, "under 30 s")
}
