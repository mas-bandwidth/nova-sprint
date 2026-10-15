//go:build functional

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// A clear is a blank slate (docs/SPEC-SPRINT.md section 13): it advances to an
// empty epoch with no work, merge, stream or reader rows, and the fleet's
// members carry over. So after init, an add of three streams and a clear, the
// drawn view still has all four tables (work, readers, merge, fleet) with the
// fleet member m1 carried over, and no old stream row in any of them; a stream
// added after the clear is drawn. add admits a card on every form, so it cannot
// open a stream with no cards, and no cardless stream is drawn on an empty
// epoch. The frame is the one `nova-table watch --view sprint` draws
// (ntable.RenderTables).
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
		code := runOnStore(a, addr, args, &out, &errb)
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
	// The empty epoch: the view still draws all four tables, the fleet's member
	// m1 carries over, and no old stream row is drawn in any of them.
	for _, table := range []string{"work", "merge", "readers", "fleet"} {
		require.True(t, has(got, table), "after a clear the %s table is drawn:\n%s", table, got)
	}
	require.Contains(t, got, "\nm1 ", "after a clear the fleet member m1 carries over:\n%s", got)
	for _, stream := range []string{"\na ", "\nb ", "\nc "} {
		require.NotContains(t, got, stream, "after a clear the old stream row %q is not drawn:\n%s", stream, got)
	}
	// add admits a card on every form, so it cannot open a stream with no cards:
	// the stream it opens after the clear is the one the view draws.
	run("add", "--stream", "b", "--count", "1", "--one")
	got = frame()
	require.True(t, has(got, "work"), "every stream is drawn, with a card or none:\n%s", got)
	require.NotContains(t, got, "\na ", "every stream is drawn, with a card or none:\n%s", got)
	require.Contains(t, got, "\nb ", "every stream is drawn, with a card or none:\n%s", got)
	require.NotContains(t, got, "\nc ", "every stream is drawn, with a card or none:\n%s", got)
}
