//go:build functional

package main

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// A store without this build's table functions is refused before any verb
// runs, with the command that loads them; with them loaded, the verb runs.
func TestAStoreWithoutThisBuildsLibraryIsRefused(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	ta := newFunctionalTestApp(t, addr)

	_, code, errb := ta.runVerbOut("init")
	require.Equal(t, 2, code, "init on a store with no library: %d %s", code, errb)
	require.Contains(t, errb, "nova-redis fn load --redis "+addr, "init on a store with no library: %d %s", code, errb)

	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))

	_, code, errb = ta.runVerbOut("init")
	require.Equal(t, 0, code, "init with the library loaded: %s", errb)
}
