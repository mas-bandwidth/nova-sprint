//go:build functional

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// --- a server build on a store missing its locked columns (v1.2.10, 13:11Z) ---

// TestStallTheBinaryRefusesAStoreMissingItsLockedColumns is the v1.2.10 switch on a real store:
// the server was switched in without the schema step its TABLES.lock names, so the fleet table
// lacked the finished and redealt columns, the shadow tick passed, and every done-path finish
// was refused for 40 minutes. On a store whose fleet table has the v1.2.9 shape, the binary's
// shadow tick and its run both refuse, exit 2, naming the nova-table commands that take the
// store to the lock; nothing runs on it.
func TestStallTheBinaryRefusesAStoreMissingItsLockedColumns(t *testing.T) {
	t.Parallel()
	r := newStallRig(t)
	r.do("init", "--readers", "reader-a,reader-b", "--members", "m1:1")
	r.do("add", "--stream", "s1", "--count", "1", "--one")
	r.do("start")

	// the v1.2.9 fleet shape: no finished and no redealt column
	c := redis.NewClient(&redis.Options{Addr: r.addr})
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()
	key := ntable.DefKey(sprint.Names{}.Table(sprint.Fleet))
	def, err := c.HGetAll(ctx, key).Result()
	require.NoError(t, err)
	require.Contains(t, def, "col:finished", "the fleet table as init writes it: %v", def)
	drop := func(list string) string {
		var keep []string
		for _, col := range strings.Split(list, ",") {
			if col != "finished" && col != "redealt" {
				keep = append(keep, col)
			}
		}
		return strings.Join(keep, ",")
	}
	require.NoError(t, c.HSet(ctx, key, "order", drop(def["order"]), "hidden", drop(def["hidden"])).Err())
	require.NoError(t, c.HDel(ctx, key, "col:finished", "col:redealt").Err())

	code, out, errs := r.cli("coordinator", "tick", "--shadow")
	assert.Equal(t, 2, code, "the shadow tick refuses the store: %s%s", out, errs)
	assert.Contains(t, out+errs, "nova-table col add fleet finished", "it names the schema step")

	srv := r.startServer(r.bin, "coordinator", "run")
	_, exited := srv.until(10*time.Second, "TICK OK")
	require.True(t, exited, "run refuses the store and exits; it never ticks on it:\n%s", srv.text())
	assert.NotContains(t, srv.text(), "TICK OK", "no tick ran on the store")
	assert.Contains(t, srv.text(), "nova-table col add fleet finished", "run names the schema step")
	assert.NotEqual(t, 0, srv.cmd.ProcessState.ExitCode(), "run exits non-zero:\n%s", srv.text())
}
