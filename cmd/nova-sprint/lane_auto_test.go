package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
	"github.com/stretchr/testify/require"
)

func TestLaneTakeAutoNamesABenchOrWaitsAndNeverQueues(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.mu.Lock()
	ta.now = time.Unix(1000, 0).UTC()
	ta.mu.Unlock()
	b := ta.a.landState()
	b.mu.Lock()
	b.hostName = func() (string, error) { return "coordinator.local", nil }
	b.mu.Unlock()
	// Init uses the fixture's normal command path; afterward every beat names
	// fixed load and cores, never facts from the machine running this test.
	var initOut, initErr bytes.Buffer
	initCode := runInto(ta.a, split("init --members m1,m2"), &initOut, &initErr)
	require.Equal(t, 0, initCode, initOut.String()+initErr.String())
	beat := func() {
		t.Helper()
		st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now}
		zero := 0.0
		for _, m := range []string{"m1", "m2"} {
			_, err := st.Beat(context.Background(), m, &zero, hostload.Source{NCPU: 16})
			require.NoError(t, err)
		}
	}
	run := func(line string) (int, string, string) {
		t.Helper()
		beat()
		var out, stderr bytes.Buffer
		code := runInto(ta.a, ta.withEpoch(split(line)), &out, &stderr)
		return code, out.String(), stderr.String()
	}

	for _, m := range []string{"m1", "m2"} {
		code, out, stderr := run("fleet up " + m)
		require.Equal(t, 0, code, out+stderr)
	}
	code, out, stderr := run("lane take go --machine auto --as alice")
	require.Equal(t, 0, code, stderr)
	require.Contains(t, out, "LANE-TAKE OK go machine=m1 as=alice slots=1/2 until=1970-01-01T00:36:40Z renewed=no")
	require.Contains(t, out, "run on m1, take again within 20m to keep it")
	require.Contains(t, out, "nova-sprint lane give go --machine auto --as alice")
	for _, who := range []string{"bob", "carol", "dave"} {
		code, out, stderr = run("lane take go --machine auto --as " + who)
		require.Equal(t, 0, code, stderr)
		require.Contains(t, out, "LANE-TAKE OK go")
	}
	code, out, stderr = run("lane take go --machine auto --as erin")
	require.Equal(t, 1, code, out)
	waitLine := out + stderr
	require.Contains(t, waitLine, "LANE-TAKE WAIT go as=erin retry_at=1970-01-01T00:17:40Z: every allowed bench is full: m1 2/2, m2 2/2")
	require.Contains(t, waitLine, "nova-sprint lane take go --machine auto --as erin")
	code, out, stderr = run("lane list")
	require.Equal(t, 0, code, stderr)
	require.NotContains(t, out, "erin", "a wait must never add a queue entry")
	code, out, stderr = run("lane take go --machine auto --as frank --wait 30m")
	require.Equal(t, 2, code)
	require.Contains(t, stderr, "--wait is not supported")
	require.Contains(t, stderr, "retry_at")
	code, out, stderr = run("lane take go --machine auto --as frank --dry-run")
	require.Equal(t, 2, code)
	require.Contains(t, stderr, "--dry-run is not supported")
	require.Contains(t, stderr, "dry pick")
	code, out, stderr = run("lane give go --machine auto --as bob")
	require.Equal(t, 0, code, stderr)
	require.Equal(t, "LANE-GIVE OK go machine=auto as=bob gave=yes\n", out)
	code, out, stderr = run("lane give go --machine auto --as bob --dry-run")
	require.Equal(t, 2, code)
	require.Contains(t, stderr, "--dry-run is not supported")
	require.True(t, strings.Contains(stderr, "dry pick"))
	code, out, stderr = run("lane take go --machine auto --as jsonworker --json")
	require.Equal(t, 0, code, stderr)
	var facts map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &facts))
	for _, key := range []string{"machine", "slots", "committed", "until", "renewed", "retry_at", "full", "released"} {
		_, ok := facts[key]
		require.True(t, ok, "JSON facts omit %s: %s", key, out)
	}
}
