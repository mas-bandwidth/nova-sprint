//go:build functional

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
)

// A real server on a private store writes its heartbeat, and where --json on
// that store carries the server's own version as server_version plus server
// (version, commit, started, at). The environment is scrubbed: HOME and
// XDG_CONFIG_HOME in a TempDir and no NOVA_*; every verb names the store with
// an explicit --redis. The heartbeat write costs one SET with an expiry a
// second, which this measures (docs/SPEC-SPRINT.md section 14,
// server_version-heartbeat-w1).
func TestServerHeartbeatOnAStoreWhereJSONReads(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()
	require.NoError(t, fn.Load(ctx, c))

	home := t.TempDir()
	env := map[string]string{"HOME": home, "XDG_CONFIG_HOME": home}
	a := newApp(func(k string) string { return env[k] })
	t.Cleanup(a.close)
	a.home = func() (string, error) { return home, nil }

	do := func(args ...string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := runOnStore(a, addr, append(args, "--redis", addr), &out, &errb)
		require.Equal(t, 0, code, "%v: %s%s", args, out.String(), errb.String())
		return out.String()
	}
	do("init", "--readers", "reader-a", "--members", "m1")

	// the run loop is the server: three ticks, the heartbeat written before each
	st, _, code := a.machineVerb("run", []string{"--redis", addr}, &bytes.Buffer{})
	require.NotNil(t, st, "run: %d", code)
	var out, errb bytes.Buffer
	a.runLoop(ctx, st, 0, 3, &out, &errb)

	// where --json states the server's own version and its statement of itself
	var v struct {
		ServerVersion string `json:"server_version"`
		Server        *struct {
			Version string    `json:"version"`
			Commit  string    `json:"commit"`
			At      time.Time `json:"at"`
		} `json:"server"`
	}
	require.NoError(t, json.Unmarshal([]byte(do("where", "--json")), &v))
	assert.Equal(t, buildVersion(), v.ServerVersion, "the server's own version")
	require.NotNil(t, v.Server, "the server's statement of itself")
	assert.Equal(t, buildVersion(), v.Server.Version)
	assert.Equal(t, sourceCommitExtra(), v.Server.Commit)
	assert.False(t, v.Server.At.IsZero(), "the write time")

	// one heartbeat write is one SET with an expiry a second
	conn := a.conns[addr]
	require.NotNil(t, conn)
	trips := &tripCounter{}
	conn.Client().AddHook(trips)
	before := trips.n.Load()
	require.NoError(t, st.SetServer(ctx, store.ServerRecord{Actor: sprint.MachineActor, Version: buildVersion()}))
	assert.Equal(t, int64(1), trips.n.Load()-before, "a heartbeat write is one SET")
	_ = c
}
