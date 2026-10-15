package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
	"github.com/stretchr/testify/require"
)

func TestBenchGrantExpiryIsPushedByTheServer(t *testing.T) {
	t.Parallel()
	ta, rig, _ := coordSprint(t, "grant-seat")
	rig.Hook(t, "grant-seat", hook.Coordinator)
	ctx := context.Background()
	argv := []string{"grant", "add", "--bench", "bench-a", "--to", "m1", "--for", "30m", "--actor", "grant-seat", "--op", "grant-add-1", "--json"}
	req := sprintwire.Request{Verbs: [][]string{argv}}
	denied := ta.a.serveCtx(ctx, req, false)
	require.Equal(t, 2, denied.Results[0].Code)
	result := ta.a.serveCtx(ctx, req, true)
	require.Equal(t, 0, result.Results[0].Code, "%s", result.Results[0].Stderr)
	var added struct {
		ID       string    `json:"id"`
		Deadline time.Time `json:"deadline"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Results[0].Stdout), &added))
	require.NotEmpty(t, added.ID)
	require.Equal(t, t0.Add(30*time.Minute), added.Deadline)
	replay := ta.a.serveCtx(ctx, req, true)
	require.Equal(t, 0, replay.Results[0].Code, "%s", replay.Results[0].Stderr)
	require.Contains(t, replay.Results[0].Stdout, `"changed":"no"`)
	wrong := append([]string(nil), argv...)
	for i := range wrong {
		if wrong[i] == "grant-seat" {
			wrong[i] = "m1"
		}
	}
	denied = ta.a.serveCtx(ctx, sprintwire.Request{Verbs: [][]string{wrong}}, true)
	require.NotEqual(t, 0, denied.Results[0].Code)
	var sent []bus.Message
	ta.a.bus = func(_ context.Context, m bus.Message, _ func(string)) error { sent = append(sent, m); return nil }
	ta.step(30 * time.Minute)
	st, err := ta.a.store(common{redis: "mem:0"})
	require.NoError(t, err)
	state, _, err := st.Machine(ctx)
	require.NoError(t, err)
	require.NotEqual(t, store.Running, state.State)
	var out, errs bytes.Buffer
	ta.a.runLoop(ctx, st, 20, 1, &out, &errs)
	require.Len(t, sent, 1, "stdout=%s stderr=%s", out.String(), errs.String())
	require.Equal(t, []string{"m1", "grant-seat"}, sent[0].To)
	require.Equal(t, fmt.Sprintf("bench-grant.%s.%d", added.ID, added.Deadline.Unix()), sent[0].Token)
	require.Equal(t, bus.KindStatus, sent[0].Kind)
	require.Equal(t, added.Deadline, ta.a.now(), "one wake does not advance fake clock")
	ta.a.runLoop(ctx, st, 20, 1, &out, &errs)
	require.Len(t, sent, 1)
}

func benchGrantRig(t *testing.T, seat string) (*testApp, *store.Store, string) {
	t.Helper()
	ta, rig, _ := coordSprint(t, seat)
	rig.Hook(t, seat, hook.Coordinator)
	result := ta.a.serveCtx(context.Background(), sprintwire.Request{Verbs: [][]string{{"grant", "add", "--bench", "bench-a", "--to", "m1", "--for", "30m", "--actor", seat, "--json"}}}, true)
	require.Equal(t, 0, result.Results[0].Code, result.Results[0].Stderr)
	var g struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(result.Results[0].Stdout), &g))
	st, err := ta.a.store(common{redis: "mem:0"})
	require.NoError(t, err)
	return ta, st, g.ID
}

func benchGrantWake(t *testing.T, ta *testApp, st *store.Store) {
	t.Helper()
	var out, errs bytes.Buffer
	ta.a.runLoop(context.Background(), st, 20, 1, &out, &errs)
	require.Empty(t, errs.String(), out.String())
}

func TestBenchGrantLostReplyReusesFrozenSenderAndToken(t *testing.T) {
	t.Parallel()
	ta, st, _ := benchGrantRig(t, "grant-replay-seat")
	seen := map[string]bool{}
	copies := map[string]int{}
	var messages []bus.Message
	ta.a.bus = func(_ context.Context, m bus.Message, _ func(string)) error {
		// The callback can take the line: no bus send holds a.serial.
		ta.a.serial.Lock()
		ta.a.serial.Unlock()
		messages = append(messages, m)
		key := m.From + "/" + m.Token
		if !seen[key] {
			seen[key] = true
			for _, to := range m.To {
				copies[to]++
			}
			return fmt.Errorf("reply lost after bus write")
		}
		return nil
	}
	ta.step(30 * time.Minute)
	benchGrantWake(t, ta, st)
	gs, err := st.Grants(context.Background())
	require.NoError(t, err)
	require.False(t, gs.Items[0].Delivery.Sent)
	frozen := *gs.Items[0].Delivery
	// Seat changes after freeze do not retarget a possibly committed token.
	require.NoError(t, st.B.SetCoordinator(context.Background(), "later-seat"))
	benchGrantWake(t, ta, st)
	require.Len(t, messages, 2)
	require.Equal(t, messages[0], messages[1])
	require.Equal(t, map[string]int{"m1": 1, "grant-replay-seat": 1}, copies)
	gs, err = st.Grants(context.Background())
	require.NoError(t, err)
	require.True(t, gs.Items[0].Delivery.Sent)
	require.Equal(t, frozen.Token, gs.Items[0].Delivery.Token)
	require.Equal(t, frozen.From, gs.Items[0].Delivery.From)
	benchGrantWake(t, ta, st)
	require.Len(t, messages, 2)
}

func TestBenchGrantReceiptUnknownAtRetryBoundary(t *testing.T) {
	t.Parallel()
	ta, st, _ := benchGrantRig(t, "grant-unknown-seat")
	sends := 0
	ta.a.bus = func(context.Context, bus.Message, func(string)) error {
		sends++
		return fmt.Errorf("lost before bus write")
	}
	ta.step(30 * time.Minute)
	benchGrantWake(t, ta, st)
	gs, err := st.Grants(context.Background())
	require.NoError(t, err)
	before := gs.Items[0].Delivery.RetryBefore
	require.Equal(t, ta.a.now().Add(bus.DefaultTokenLife), before)
	ta.step(bus.DefaultTokenLife - time.Nanosecond)
	benchGrantWake(t, ta, st)
	require.Equal(t, 2, sends)
	ta.step(time.Nanosecond)
	benchGrantWake(t, ta, st)
	require.Equal(t, 2, sends, "never send at RetryBefore")
	gs, err = st.Grants(context.Background())
	require.NoError(t, err)
	require.True(t, gs.Items[0].Judged)
	notes, _, err := st.B.NotesSince(context.Background(), "", 1000)
	require.NoError(t, err)
	count := 0
	for _, n := range notes {
		if strings.Contains(n.What, "receipt unknown") {
			count++
		}
	}
	require.Equal(t, 1, count)
	benchGrantWake(t, ta, st)
	ta.step(bus.DefaultTokenCleanup)
	benchGrantWake(t, ta, st)
	require.Equal(t, 2, sends, "cleanup cannot restart an expired token")
	notes, _, err = st.B.NotesSince(context.Background(), "", 1000)
	require.NoError(t, err)
	count = 0
	for _, n := range notes {
		if strings.Contains(n.What, "receipt unknown") {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestBenchGrantEarlyExtensionAndLateEnd(t *testing.T) {
	t.Parallel()
	ta, st, id := benchGrantRig(t, "grant-edit-seat")
	edit := func(op string, more ...string) {
		t.Helper()
		argv := append([]string{"grant", op, id, "--actor", "grant-edit-seat"}, more...)
		r := ta.a.serveCtx(context.Background(), sprintwire.Request{Verbs: [][]string{argv}}, true)
		require.Equal(t, 0, r.Results[0].Code, r.Results[0].Stderr)
	}
	ta.step(10 * time.Minute)
	edit("extend", "--for", "30m")
	sends := 0
	ta.a.bus = func(_ context.Context, m bus.Message, _ func(string)) error {
		sends++
		require.Contains(t, m.Subject, t0.Add(40*time.Minute).Format(time.RFC3339))
		return nil
	}
	ta.step(20 * time.Minute)
	benchGrantWake(t, ta, st)
	require.Zero(t, sends, "old deadline cancelled before it passed")
	ta.step(10 * time.Minute)
	edit("end")
	benchGrantWake(t, ta, st)
	require.Equal(t, 1, sends, "end at deadline retains owed expiry")
	benchGrantWake(t, ta, st)
	require.Equal(t, 1, sends)
}

func TestBenchGrantCoordinatorStillNeedsHookProof(t *testing.T) {
	t.Parallel()
	ta, _, _ := coordSprint(t, "grant-no-hook")
	result := ta.a.serveCtx(context.Background(), sprintwire.Request{Verbs: [][]string{{"grant", "add", "--bench", "b1", "--to", "m1", "--for", "1m", "--actor", "grant-no-hook"}}}, true)
	require.NotEqual(t, 0, result.Results[0].Code)
	require.Contains(t, result.Results[0].Stderr, "hook")
	raw, exists, err := ta.m.GetKey(context.Background(), "grants")
	require.NoError(t, err)
	require.False(t, exists)
	require.Empty(t, raw)
}

func TestBenchGrantServerStoreSurvivesClear(t *testing.T) {
	t.Parallel()
	for _, duringSend := range []bool{false, true} {
		t.Run(fmt.Sprintf("during-send-%t", duringSend), func(t *testing.T) {
			t.Parallel()
			ta, st, _ := benchGrantRig(t, fmt.Sprintf("clear-seat-%t", duringSend))
			clear := func() {
				t.Helper()
				fresh, err := ta.a.store(common{redis: "mem:0"})
				require.NoError(t, err)
				_, err = fresh.Clear(context.Background())
				require.NoError(t, err)
			}
			if !duringSend {
				clear()
			}
			sends := 0
			ta.a.bus = func(context.Context, bus.Message, func(string)) error {
				sends++
				if duringSend && sends == 1 {
					clear()
				}
				return nil
			}
			ta.step(30 * time.Minute)
			benchGrantWake(t, ta, st)
			gs, err := st.Grants(context.Background())
			require.NoError(t, err)
			require.Len(t, gs.Items, 1)
			require.NotNil(t, gs.Items[0].Delivery)
			require.True(t, gs.Items[0].Delivery.Sent, "receipt commits in current epoch")
			benchGrantWake(t, ta, st)
			require.Equal(t, 1, sends)
		})
	}
}
