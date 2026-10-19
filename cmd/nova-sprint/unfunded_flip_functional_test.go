//go:build functional

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	ntconfig "github.com/mas-bandwidth/nova-tools/pkg/config"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
)

// TestUnfundedFlipFunctional: on a real sprint store (a private Redis) with nova-config's
// provider and route rows applied to it by the config tool's own Redis applier, a member's
// take on the provider's route finishes refused for credit (`provider: class=out-of-credit
// status=402 msg=Insufficient credits`); the tick rests the provider, and one pass of the run
// loop's flip writes the provider row funded=false as the machine, applies it (the store's
// provider hash says false), and the next tick deals nothing to its route.
//
// tla/ProviderFunding.tla BillingFailure, Deal, NoDealToUnfunded
func TestUnfundedFlipFunctional(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	ctx := context.Background()
	require.NoError(t, fn.Load(ctx, c))

	cfg := ntconfig.NewMem()
	_, err := cfg.Insert(ctx, ntconfig.KindProvider, ntconfig.Row{Name: "openrouter", Fields: map[string]string{"billing": "metered", "funded": "true"}}, "glenn")
	require.NoError(t, err)
	_, err = cfg.Insert(ctx, ntconfig.KindRoute, ntconfig.Row{Name: "flash-or", Fields: map[string]string{"tier": "flash", "provider": "openrouter", "model": "m", "deadline": "600", "enabled": "true", "price_input": "0.30", "price_output": "1.20", "price_source": "https://openrouter.ai/models", "price_as_of": "2026-10-01"}}, "glenn")
	require.NoError(t, err)
	ap := &ntconfig.RedisApplier{Client: c}
	for _, kind := range []string{ntconfig.KindProvider, ntconfig.KindRoute} {
		_, err := ntconfig.ApplyInRun(ctx, cfg, ap, kind, "glenn", false, false, nil, func(ntconfig.Op) {})
		require.NoError(t, err, kind)
	}

	ta := newFunctionalTestApp(t, addr)
	run := func(args ...string) string {
		t.Helper()
		return ta.runVerb(args...)
	}
	run("init", "--readers", "reader-a,reader-b", "--members", "m1:8")
	dir := t.TempDir()
	for _, id := range []string{"s1-1", "s1-2"} {
		brief := filepath.Join(dir, id+".md")
		require.NoError(t, os.WriteFile(brief, []byte(passingBrief(id+": a flash card (s1) tier: flash\nTHE TASK.\nwork\n")), 0o600))
		run("add", "--stream", "s1", id, "--one", "--brief-file", brief)
	}
	run("fleet", "beat", "m1")
	run("start")
	run("tick")
	run("tick")
	require.Contains(t, run("card", "s1-1"), "flash-or", "s1-1 is dealt on the provider's route")
	run("take", "--as", "m1", "--max", "1")
	run("finish", "--as", "m1", "s1-1.w1@1", "--epoch", "0", "--failed", "--report", "provider failure: provider: class=out-of-credit status=402 msg=Insufficient credits")
	run("tick") // the provider rests on the refusal

	st, err := ta.a.store(common{redis: addr, actor: sprint.MachineActor})
	require.NoError(t, err)
	flipConfigs.Store(ta.a, flipConfigFn(func(context.Context, *store.Store) (ntconfig.Store, ntconfig.Applier, func(), error) {
		return cfg, ap, func() {}, nil
	}))
	defer flipConfigs.Delete(ta.a)
	var out bytes.Buffer
	ta.a.flipUnfunded(ctx, st, ta.a.flipConfigOf(), &out)
	assert.Contains(t, out.String(), "UNFUNDED-FLIP provider=openrouter funded=false actor=machine")

	row, found, err := cfg.Get(ctx, ntconfig.KindProvider, "openrouter")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "false", row.Fields["funded"])
	hist, err := cfg.History(ctx, ntconfig.KindProvider, "openrouter")
	require.NoError(t, err)
	require.NotEmpty(t, hist)
	assert.Equal(t, sprint.MachineActor, hist[len(hist)-1].Actor, "the flip is the machine's")
	assert.Contains(t, row.Fields["note"], "unfunded by the machine ")
	applied, err := c.HGet(ctx, ntconfig.ProviderKey("openrouter"), "funded").Result()
	require.NoError(t, err)
	assert.Equal(t, "false", applied, "the applied hash says false")

	// a second pass writes nothing
	out.Reset()
	ta.a.flipUnfunded(ctx, st, ta.a.flipConfigOf(), &out)
	assert.Empty(t, out.String())
	hist2, err := cfg.History(ctx, ntconfig.KindProvider, "openrouter")
	require.NoError(t, err)
	assert.Len(t, hist2, len(hist), "one write per provider")

	// the next ticks deal nothing to the unfunded provider's route
	run("tick")
	run("tick")
	snap := &sprint.Snapshot{}
	require.NoError(t, st.RoutesInto(ctx, snap))
	assert.False(t, snap.Providers["openrouter"].Funded)
	// flash-or is the only route: a card dealt anywhere would be dealt on it
	var w whereView
	require.NoError(t, json.Unmarshal([]byte(run("where", "--json")), &w))
	m1 := w.Tables[sprint.Fleet]["m1"]
	assert.Equal(t, "0", m1["ready"], "nothing dealt to the unfunded provider's route: %v", m1)
	assert.Equal(t, "0", m1["working"], "nothing dealt to the unfunded provider's route: %v", m1)
	assert.Contains(t, run("where"), "openrouter unfunded")
}
