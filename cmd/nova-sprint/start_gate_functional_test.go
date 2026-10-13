//go:build functional

package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
)

// The card check gate on a real store (start-refuses-until-a-clean-check; tla/MachineReady.tla
// StartOnlyChecked): a production app (newApp, the gate on) on a private redis-server. A start
// with no check record for the epoch is refused, exit 1, the machine not started; the rig's
// funnel (runOnStore) records the epoch's clean check before a start, as check cards writes it,
// and the same start then passes through the real gate.
func TestStartRefusesWithNoCardCheckOnTheStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(ctx, c))
	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "coordinator",
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	a := newApp(func(k string) string { return env[k] })
	defer a.close()
	require.True(t, a.cardCheckGate, "the production app gates start")
	a.home = func() (string, error) { return home, nil }
	a.forward = func(_ context.Context, addr string, verbs ...[]string) ([]sprintwire.Result, error) {
		t.Fatalf("a verb was forwarded to a sprint server at %s: %q", addr, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	run := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := runOnStore(a, addr, append(strings.Fields(line), "--redis", addr), &out, &errb)
		require.Zero(t, code, "%s: exit %d\n%s%s", line, code, out.String(), errb.String())
		return out.String()
	}
	running := func() bool {
		t.Helper()
		st := &store.Store{B: mustBackend(t, a, addr), Names: sprint.Names{}, Now: a.now}
		m, _, err := st.Machine(ctx)
		require.NoError(t, err)
		return m.Running()
	}
	run("init --readers reader-a --members m1")
	declareFunctionalRoutes(t, c, "flash", "pro")
	run("add --stream s1 --count 1 --one")

	// start straight through runInto, outside the funnel: no check record for the epoch
	var out, errb bytes.Buffer
	code := runInto(a, []string{"start", "--redis", addr}, &out, &errb)
	require.Equal(t, 1, code, "refused: %s%s", out.String(), errb.String())
	assert.Contains(t, errb.String(), "run nova-sprint check cards first: no check record for epoch ")
	assert.Contains(t, errb.String(), "nothing was written")
	assert.False(t, running(), "the machine is not started")

	// through the funnel: the epoch's clean check is recorded, and the real gate lets it start
	run("start")
	assert.True(t, running())
}

// mustBackend is the app's backend for the store at addr.
func mustBackend(t *testing.T, a *app, addr string) store.Backend {
	t.Helper()
	b, err := a.backend(context.Background(), addr, sprint.Names{})
	require.NoError(t, err)
	return b
}
