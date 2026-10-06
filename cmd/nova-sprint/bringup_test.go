package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

func TestStartPrintsTheBringUpWithEachThingsStateAndItsCommand(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1,m2 --owner glenn")
	out := ta.ok("start")
	require.Contains(t, out, "START OK")
	for _, want := range []string{
		`BRING-UP sprint-server missing command="nova-sprint run --listen 127.0.0.1:6390"`,
		`BRING-UP store running command="nova-sprint where"`,
		`BRING-UP bus missing command="nova-bus peek --as coordinator"`,
		`BRING-UP judgment-push missing command="nova-sprint inbox --wait --push seat"`,
		`BRING-UP event-watch missing command="nova-sprint watch --events"`,
		`BRING-UP coordinator-beat missing held=no command="nova-sprint friend beat coordinator"`,
		`BRING-UP reader reader-a running width=unbounded tiers=all command="nova-sprint reader up reader-a"`,
		`BRING-UP reader reader-b running width=unbounded tiers=all command="nova-sprint reader up reader-b"`,
		`BRING-UP dashboard missing last=- command="nova-sprint dashboard --listen 127.0.0.1:7390"`,
	} {
		assert.Contains(t, out, want+"\n", out)
	}
	assert.NotContains(t, out, "BRING-UP warning")
	assert.NotContains(t, out, "BRING-UP friend")
	order := []string{"sprint-server", "store", "bus", "judgment-push", "event-watch", "coordinator-beat", "reader reader-a", "reader reader-b", "dashboard"}
	at := -1
	for _, name := range order {
		i := strings.Index(out, "BRING-UP "+name+" ")
		assert.Greater(t, i, at, name)
		at = i
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "BRING-UP ") {
			continue
		}
		for _, bad := range []string{"launchctl", "redis-cli", "sh -c", "|", "$("} {
			assert.NotContains(t, line, bad, line)
		}
		assert.Regexp(t, ` (running|missing|stale) `, line+" ")
	}
	same := ta.ok("check --bring-up")
	for _, want := range []string{"BRING-UP sprint-server missing", "BRING-UP store running", "BRING-UP judgment-push missing"} {
		assert.Contains(t, same, want)
	}
	plain := ta.ok("check")
	assert.Contains(t, plain, "CHECK OK")
	assert.NotContains(t, plain, "BRING-UP")

	seat := ta.ok("seat")
	assert.NotContains(t, seat, "BRING-UP")
	dry := ta.ok("coordinator rowan --reason 'glenn moves the seat' --actor glenn --dry-run")
	assert.Equal(t, "COORDINATOR DRY-RUN holder=rowan from=coordinator by=glenn given; nothing was changed\n", dry)

	hand := ta.ok("handover")
	assert.Contains(t, hand, "HANDOVER OK")
	assert.Greater(t, strings.Index(hand, "BRING-UP sprint-server"), strings.Index(hand, "HANDOVER OK"))

	gave := ta.ok("coordinator rowan --reason 'rowan holds it'")
	assert.Contains(t, gave, "COORDINATOR OK holder=rowan from=coordinator by=coordinator given\n")
	assert.Greater(t, strings.Index(gave, "BRING-UP sprint-server"), strings.Index(gave, "COORDINATOR OK"))
	assert.Contains(t, gave, `command="nova-bus peek --as rowan"`)

	took := newTestApp(t)
	took.ok("init --readers reader-a --members m1 --owner glenn")
	take := took.ok("coordinator rowan --take --approved-by glenn --reason 'taken' --actor rowan")
	assert.Contains(t, take, "COORDINATOR OK holder=rowan from=coordinator by=rowan taken approved_by=glenn\n")
	assert.Contains(t, take, "BRING-UP store running")
	assert.Greater(t, strings.Index(take, "BRING-UP sprint-server"), strings.Index(take, "COORDINATOR OK"))
}

func TestStartJSONDoesNotAppendTheBringUp(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	out := ta.ok("start --json")
	assert.NotContains(t, out, "BRING-UP")
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got), out)
	hand := ta.ok("handover --json")
	assert.NotContains(t, hand, "BRING-UP")
	require.NoError(t, json.Unmarshal([]byte(hand), &got), hand)
}

func TestTheRunLoopRaisesOneJudgmentWhenARunningLineGoesMissing(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("start")
	assert.Equal(t, 0, bringDownCount(t, ta), "the first look records and judges nothing")

	restore := ta.a.hookRunBringUp(&bytes.Buffer{})
	defer restore()
	var errb bytes.Buffer
	st, _, code := ta.a.machineVerb("run", nil, &errb)
	require.NotNil(t, st, "run: %d %s", code, errb.String())
	require.NoError(t, ta.a.markWatch(context.Background(), st))

	var out bytes.Buffer
	ta.a.runLoop(context.Background(), st, 20, 1, &out, &errb)
	assert.Equal(t, 0, bringDownCount(t, ta), "running, recorded, is not a fall")

	// DeleteKeys drops table and sprint keys, not a machine record. A value
	// that is not a time is the same as no record: the watch is missing.
	require.NoError(t, ta.m.SetKey(context.Background(), bringUpWatchKey, "not-a-time"))
	out.Reset()
	ta.a.runLoop(context.Background(), st, 20, 1, &out, &errb)
	raw, _, _ := ta.m.GetKey(context.Background(), bringUpStateKey)
	assert.Equal(t, 1, bringDownCount(t, ta), "stderr=%s state=%s out=%s", errb.String(), raw, out.String())
	assert.Contains(t, bringDownNote(t, ta), "event-watch went from running to missing; run: nova-sprint watch --events")

	out.Reset()
	ta.a.runLoop(context.Background(), st, 20, 1, &out, &errb)
	assert.Equal(t, 1, bringDownCount(t, ta), "staying missing raises nothing more")
}

func TestAMissingPusherPrintsMissingWithItsCommand(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	out := ta.ok("check --bring-up")
	assert.Contains(t, out, `BRING-UP judgment-push missing command="nova-sprint inbox --wait --push seat"`)
	assert.NotContains(t, out, "launchctl")
}

func TestBringUpDownIsOneJudgmentWhenRunningGoesMissing(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.ok("start")
	assert.Equal(t, 0, bringDownCount(t, ta))

	setServerAt(t, ta, ta.a.now())
	out := ta.ok("check --bring-up")
	assert.Contains(t, out, "BRING-UP sprint-server running")
	assert.Equal(t, 0, bringDownCount(t, ta))

	setServerAt(t, ta, ta.a.now().Add(-3*time.Minute))
	out = ta.ok("check --bring-up")
	assert.Contains(t, out, "BRING-UP sprint-server stale")
	assert.Equal(t, 0, bringDownCount(t, ta), "stale is not a fall")

	setServerAt(t, ta, ta.a.now())
	ta.ok("check --bring-up")
	require.NoError(t, ta.m.SetKey(context.Background(), "server", "{}"))
	out = ta.ok("check --bring-up")
	assert.Contains(t, out, "BRING-UP sprint-server missing")
	assert.Equal(t, 1, bringDownCount(t, ta))
	note := bringDownNote(t, ta)
	assert.Contains(t, note, "sprint-server went from running to missing; run: nova-sprint run --listen 127.0.0.1:6390")
	ta.ok("check --bring-up")
	assert.Equal(t, 1, bringDownCount(t, ta), "staying missing raises nothing more")

	setServerAt(t, ta, ta.a.now())
	ta.ok("check --bring-up")
	require.NoError(t, ta.m.SetKey(context.Background(), "server", "{}"))
	ta.ok("check --bring-up")
	assert.Equal(t, 2, bringDownCount(t, ta), "running again, then missing, is another judgment")
}

func TestFriendDaemonAndBeatAndTheCoordinatorsHold(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	st := opened(t, ta)
	_, _, _, err := st.SyncFriends(context.Background(), []store.FriendSpec{{Name: "amy", Width: 1}})
	require.NoError(t, err)
	_, err = st.FriendBeat(context.Background(), "amy")
	require.NoError(t, err)
	out := ta.ok("check --bring-up")
	assert.Contains(t, out, `BRING-UP friend amy daemon running command="nova-friend install --as amy"`)
	assert.Contains(t, out, `BRING-UP friend amy beat running command="nova-sprint friend beat amy"`)

	ta.mu.Lock()
	ta.now = ta.now.Add(10 * time.Second)
	ta.mu.Unlock()
	out = ta.ok("check --bring-up")
	assert.Contains(t, out, "BRING-UP friend amy daemon stale")
	assert.Contains(t, out, "BRING-UP friend amy beat stale")

	held := newTestApp(t)
	held.ok("init --readers reader-a --members m1")
	st = opened(t, held)
	_, _, _, err = st.SyncFriends(context.Background(), []store.FriendSpec{{Name: "coordinator", Width: 1}})
	require.NoError(t, err)
	require.NoError(t, st.SetFriendHeld(context.Background(), "coordinator", true, "glenn", "not taking cards", time.Time{}, 1))
	_, err = st.FriendBeat(context.Background(), "coordinator")
	require.NoError(t, err)
	out = held.ok("check --bring-up")
	assert.Contains(t, out, `BRING-UP coordinator-beat running held=yes command="nova-sprint friend beat coordinator"`)

	free := newTestApp(t)
	free.ok("init --readers reader-a --members m1")
	st = opened(t, free)
	_, _, _, err = st.SyncFriends(context.Background(), []store.FriendSpec{{Name: "coordinator", Width: 1}})
	require.NoError(t, err)
	_, err = st.FriendBeat(context.Background(), "coordinator")
	require.NoError(t, err)
	out = free.ok("check --bring-up")
	assert.Contains(t, out, `BRING-UP coordinator-beat running held=no command="nova-sprint hold coordinator --reason 'the coordinator is not taking cards'"`)
}

func TestDashboardPollPrintsTheLastGoodTime(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	at := ta.a.now().UTC().Format(time.RFC3339)
	ta.a.outside.httpGet = func(context.Context, string) (int, []byte, error) {
		return 200, []byte(`{"at":"` + at + `"}`), nil
	}
	out := ta.ok("check --bring-up")
	assert.Contains(t, out, "BRING-UP dashboard running last="+at+" ")
	assert.Contains(t, out, `command="nova-sprint dashboard --listen 127.0.0.1:7390"`)
	assert.Equal(t, 0, bringDownCount(t, ta))

	old := ta.a.now().Add(-time.Minute).UTC().Format(time.RFC3339)
	ta.a.outside.httpGet = func(context.Context, string) (int, []byte, error) {
		return 200, []byte(`{"at":"` + old + `"}`), nil
	}
	out = ta.ok("check --bring-up")
	assert.Contains(t, out, "BRING-UP dashboard stale last="+old+" ")
	assert.Equal(t, 0, bringDownCount(t, ta))
}

func opened(t *testing.T, ta *testApp) *store.Store {
	t.Helper()
	st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(t, err)
	return st
}

func setServerAt(t *testing.T, ta *testApp, at time.Time) {
	t.Helper()
	b, err := json.Marshal(bringServerRec{Actor: "coordinator", At: at})
	require.NoError(t, err)
	require.NoError(t, ta.m.SetKey(context.Background(), "server", string(b)))
}

func bringDownCount(t *testing.T, ta *testApp) int {
	t.Helper()
	n := 0
	for _, l := range bringLog(t, ta) {
		if l.Note != nil && l.Note.Type == sprint.NBringUpDown {
			n++
		}
	}
	return n
}

func bringDownNote(t *testing.T, ta *testApp) string {
	t.Helper()
	for _, l := range bringLog(t, ta) {
		if l.Note != nil && l.Note.Type == sprint.NBringUpDown {
			return l.Note.What
		}
	}
	t.Fatal("no bring-up judgment")
	return ""
}

func bringLog(t *testing.T, ta *testApp) []sprint.Line {
	t.Helper()
	lines, err := opened(t, ta).Log(context.Background())
	require.NoError(t, err)
	return lines
}
