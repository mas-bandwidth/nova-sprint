//go:build functional

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// A clear advances to an empty epoch, so the stored view draws the blank slate:
// after init, an add of three streams and a clear, the frame still draws the
// four tables, the fleet carrying its members over, and holds no row for the
// streams a, b or c the old epoch had. A stream added after the clear is drawn
// again; the old streams are not. The test's old point (a stream with no cards
// is drawn) cannot be kept on this path: no verb adds a stream without a card
// (add wants ids, --count, --sentinel or a brief), so the stream added after
// the clear carries one. The frame is the one `nova-table watch --view sprint`
// draws (ntable.RenderTables).
func TestTheViewAfterAClearDrawsNoOldStream(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	ctx := context.Background()
	require.NoError(t, fn.Load(ctx, c))
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "coordinator"}
	a := newApp(func(k string) string { return env[k] })
	defer a.close()
	run := func(args ...string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := a.run(args, &out, &errb)
		require.Equal(t, 0, code, "%v: %d %s", args, code, errb.String())
	}
	frame := func() string {
		t.Helper()
		v, err := ntable.ViewGet(ctx, c, "sprint")
		require.NoError(t, err)
		var tables []ntable.Table
		for _, name := range v.Tables {
			tb, err := ntable.Read(ctx, c, name)
			require.NoError(t, err)
			tables = append(tables, tb)
		}
		return ntable.RenderTables("", tables, ntable.RenderOpts{})
	}
	has := func(text, table string) bool {
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, table+" ") || line == table {
				return true
			}
		}
		return false
	}

	run("init", "--readers", "reader-a,reader-b", "--members", "m1")
	run("add", "--stream", "a,b,c", "--count", "1", "--one")
	got := frame()
	require.True(t, has(got, "work"), "streams with a card each: the work table is drawn:\n%s", got)
	require.Contains(t, got, "\na ", "streams with a card each: the work table is drawn:\n%s", got)
	run("clear", "--confirm", "sprint")
	got = frame()
	// the empty epoch draws the four tables; the fleet holds the member carried over
	for _, table := range []string{"work", "merge", "readers", "fleet"} {
		require.True(t, has(got, table), "after a clear the %s table is drawn:\n%s", table, got)
	}
	require.Contains(t, got, "\nm1 ", "after a clear the fleet carries its members over:\n%s", got)
	for _, stream := range []string{"\na ", "\nb ", "\nc "} {
		require.NotContains(t, got, stream, "after a clear the old stream row %q is gone (blank slate):\n%s", stream, got)
	}
	run("add", "--stream", "b", "--count", "1", "--one")
	got = frame()
	require.True(t, has(got, "work"), "a stream added after the clear is drawn:\n%s", got)
	require.Contains(t, got, "\nb ", "a stream added after the clear is drawn:\n%s", got)
	require.NotContains(t, got, "\na ", "the old streams are not drawn again:\n%s", got)
	require.NotContains(t, got, "\nc ", "the old streams are not drawn again:\n%s", got)
}
