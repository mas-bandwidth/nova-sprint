//go:build functional

// openConn dials a real loopback port under a wall-clock bound: a functional test
// (RULES 6a: no sockets or real time in a unit test). libraryMatches' unit tests,
// over net.Pipe, are main_cover_test.go.
package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMainCoverOpenConnRefusesAStoreThatIsNotThereAndResolvesTheLogin
// covers openConn (cmd/nova-sprint/main.go:229, the finding's 0.0%). Its main
// path opens a store, and the package's Open exposes no dialer to inject, so
// the path exercised here is the one a store that is not there takes: the
// login is resolved from the environment (both branches: a user with a named
// password variable, and no user) and Open is called, its refusal returned
// unchanged and nothing left open. The successful open needs a live store or a
// redisconn dial seam and is named in the report as not-done.
func TestMainCoverOpenConnRefusesAStoreThatIsNotThereAndResolvesTheLogin(t *testing.T) {
	t.Parallel()
	// a port no store listens on: the dial is refused at once, no process,
	// no bind and nothing started
	const absent = "127.0.0.1:1"
	t.Run("a user with a named password variable is resolved, then the dial is refused", func(t *testing.T) {
		env := map[string]string{
			"NOVA_SPRINT_REDIS_USER":         "sprint-user",
			"NOVA_SPRINT_REDIS_PASSWORD_ENV": "SPRINT_PW",
			"SPRINT_PW":                      "s3cr3t",
		}
		a := &app{getenv: func(k string) string { return env[k] }}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		conn, err := a.openConn(ctx, absent)
		require.Error(t, err)
		assert.Nil(t, conn)
		assert.Contains(t, err.Error(), absent, err.Error())
	})
	t.Run("no user and no password variable: the default login, then the dial is refused", func(t *testing.T) {
		a := &app{getenv: func(string) string { return "" }}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		conn, err := a.openConn(ctx, absent)
		require.Error(t, err)
		assert.Nil(t, conn)
		assert.Contains(t, err.Error(), absent, err.Error())
	})
}
