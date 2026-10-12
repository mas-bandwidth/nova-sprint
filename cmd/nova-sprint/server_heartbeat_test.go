package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The server's heartbeat carries the running binary's own version (the string
// `nova-sprint version` prints) and its source commit, is written within one
// second (store.ServerEvery) and stands for the server only while it is fresh
// (store.ServerTTL, three seconds). The heartbeat is one SET with an expiry,
// the record where --json reads server_version and server from
// (docs/SPEC-SPRINT.md section 14, server_version-heartbeat-w1).
func TestServerHeartbeatCarriesTheBinarysVersion(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ctx := context.Background()
	st, err := ta.a.store(common{redis: "mem:0", actor: sprint.MachineActor})
	require.NoError(t, err)

	// the heartbeat is written once a second and stands for three
	require.LessOrEqual(t, store.ServerEvery, time.Second, "the heartbeat is written within a second")
	require.Equal(t, 3*time.Second, store.ServerTTL, "the heartbeat stands for three seconds")

	srv := &app{now: ta.a.now, serverStarted: ta.now}
	require.NoError(t, st.SetServer(ctx, srv.serverRecord(sprint.MachineActor)))

	rec, ok, err := st.Server(ctx)
	require.NoError(t, err)
	require.True(t, ok, "the heartbeat appears at once")
	assert.Equal(t, buildVersion(), rec.Version, "the heartbeat carries the binary's version")
	assert.Equal(t, sourceCommitExtra(), rec.Commit, "the heartbeat carries the source commit")
	assert.Equal(t, ta.now, rec.Started, "the heartbeat carries the server's start time")
	assert.Equal(t, ta.now, rec.At, "the heartbeat carries the write time")

	// the server stops: no write for past the TTL, and the record is no server
	ta.a.sleep(store.ServerTTL + time.Second)
	_, ok, err = st.Server(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "a heartbeat older than the TTL is gone")
}

// where --json carries the server's version as server_version and its own
// statement of itself as server (version, commit, started, at), and both are
// absent when the heartbeat is missing or older than its TTL, so the dashboard
// hides its version cell rather than show a stale one (docs/SPEC-SPRINT.md
// section 14, server_version-heartbeat-w1).
func TestWhereCarriesTheServerVersion(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ctx := context.Background()
	st, err := ta.a.store(common{redis: "mem:0", actor: sprint.MachineActor})
	require.NoError(t, err)
	require.NoError(t, st.SetServer(ctx, store.ServerRecord{
		Actor: sprint.MachineActor, Version: "v9.9.9", Commit: "commit=abc123", Started: ta.now,
	}))

	var v struct {
		ServerVersion string          `json:"server_version"`
		Server        *serverBeatView `json:"server"`
	}
	ta.json("where", &v)
	assert.Equal(t, "v9.9.9", v.ServerVersion)
	require.NotNil(t, v.Server)
	assert.Equal(t, "v9.9.9", v.Server.Version)
	assert.Equal(t, "commit=abc123", v.Server.Commit)
	assert.Equal(t, ta.now, v.Server.Started)
	assert.Equal(t, ta.now, v.Server.At)

	// the heartbeat goes stale: both fields are absent
	ta.a.sleep(store.ServerTTL + time.Second)
	var stale struct {
		ServerVersion string          `json:"server_version"`
		Server        *serverBeatView `json:"server"`
	}
	ta.json("where", &stale)
	assert.Empty(t, stale.ServerVersion, "a stale heartbeat's version is hidden")
	assert.Nil(t, stale.Server, "a stale heartbeat is absent")
}
