package main

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	ntconfig "github.com/mas-bandwidth/nova-tools/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memApplier is the Redis side of nova-config's apply over the test's sprint store twin: a
// provider row written is the row the deal reads (store.Mem SetProviders). No socket.
type memApplier struct {
	m     *store.Mem
	views map[string]ntconfig.View
	rev   int64
}

func newMemApplier(m *store.Mem) *memApplier {
	return &memApplier{m: m, views: map[string]ntconfig.View{}}
}

func (a *memApplier) Read(_ context.Context, kind string) (map[string]ntconfig.View, int64, error) {
	if kind != ntconfig.KindProvider {
		return map[string]ntconfig.View{}, 0, nil
	}
	return maps.Clone(a.views), a.rev, nil
}

func (a *memApplier) Prepare(context.Context) error { return nil }

func (a *memApplier) Write(_ context.Context, _ string, row ntconfig.Row, _ ntconfig.View, _, _ string) error {
	a.views[row.Name] = ntconfig.View(maps.Clone(row.Fields))
	a.publish()
	return nil
}

func (a *memApplier) Remove(_ context.Context, _, name, _, _ string) error {
	delete(a.views, name)
	a.publish()
	return nil
}

func (a *memApplier) Stamp(_ context.Context, _ string, _, rev int64) error {
	a.rev = rev
	return nil
}

func (a *memApplier) publish() {
	out := map[string]sprint.Provider{}
	for name, v := range a.views {
		p, _ := store.ProviderOf(name, v)
		out[name] = p
	}
	a.m.SetProviders(out)
}

// fundingApp is a test app whose sprint names the owner glenn, with nova-config's in-memory
// store holding the providers named (funded as given), applied to the sprint store's twin,
// and the store the run loop writes to as the machine.
func fundingApp(t *testing.T, funded map[string]bool) (*testApp, *ntconfig.Mem, *memApplier, *store.Store) {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1 --owner glenn")
	ctx := context.Background()
	cfg := ntconfig.NewMem()
	var routes []sprint.Route
	for p, f := range funded {
		word := "false"
		if f {
			word = "true"
		}
		_, err := cfg.Insert(ctx, ntconfig.KindProvider, ntconfig.Row{Name: p, Fields: map[string]string{"billing": "metered", "funded": word}}, "glenn")
		require.NoError(t, err)
		routes = append(routes, sprint.Route{Name: "pro-" + p, Tier: "pro", Provider: p, Model: "m", Enabled: true, Deadline: 600})
	}
	ta.m.SetRoutes(routes)
	ap := newMemApplier(ta.m)
	_, err := ntconfig.ApplyInRun(ctx, cfg, ap, ntconfig.KindProvider, "glenn", false, false, nil, func(ntconfig.Op) {})
	require.NoError(t, err)
	st, err := ta.a.store(common{redis: "mem:0", actor: sprint.MachineActor})
	require.NoError(t, err)
	return ta, cfg, ap, st
}

// restOnCredit writes a provider's rest as the tick writes one on a take refused for credit
// (provider_funds.go providerRestsDue): open, naming the card and the provider's words.
func restOnCredit(t *testing.T, ta *testApp, st *store.Store, provider string) {
	t.Helper()
	at := ta.a.now().UTC().Format("2006-01-02T15:04:05Z07:00")
	v := at + " open s1-1.w1 out-of-credit out of credit: provider " + provider + " refused card s1-1.w1 on route pro-" + provider + ": class=out-of-credit status=402 msg=Insufficient credits"
	seed := store.Step{Verb: "test seed", Load: []string{sprint.Fleet}, Actor: sprint.MachineActor, Plan: func(s *sprint.Snapshot) sprint.Plan {
		was, had := s.Fleet.Prop(sprint.PropProviderRest(provider))
		return sprint.Plan{Props: []sprint.PropWrite{{Table: sprint.Fleet, Name: sprint.PropProviderRest(provider), Value: v, Was: was, WasAbsent: !had}}}
	}}
	_, err := st.Run(context.Background(), seed)
	require.NoError(t, err)
}

// machineChanges is the config store's history of the provider's row written by the machine.
func machineChanges(t *testing.T, cfg *ntconfig.Mem, provider string) []ntconfig.Change {
	t.Helper()
	hist, err := cfg.History(context.Background(), ntconfig.KindProvider, provider)
	require.NoError(t, err)
	var out []ntconfig.Change
	for _, c := range hist {
		if c.Actor == sprint.MachineActor {
			out = append(out, c)
		}
	}
	return out
}

// TestUnfundedFlipWritesOnceAsMachine: a provider rested on a take refused for credit, its
// row funded=true: one pass of the run loop's flip writes funded=false on its nova-config
// row, as the machine, with the note naming the class, the status, the words and the card,
// and applies it, so the sprint store's row says false; a second pass writes nothing (one
// write per provider). A config store that does not answer leaves the flip owed, said on the
// loop's line, and writes nothing.
//
// tla/ProviderFunding.tla BillingFailure, OneWritePerProvider
func TestUnfundedFlipWritesOnceAsMachine(t *testing.T) {
	t.Parallel()
	ta, cfg, ap, st := fundingApp(t, map[string]bool{"openrouter": true})
	ctx := context.Background()
	restOnCredit(t, ta, st, "openrouter")
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
	assert.Equal(t, "false", row.Fields["funded"], "the row flipped")
	assert.Contains(t, row.Fields["note"], "unfunded by the machine ")
	assert.Contains(t, row.Fields["note"], "out-of-credit 402 Insufficient credits on s1-1.w1")
	changes := machineChanges(t, cfg, "openrouter")
	require.Len(t, changes, 1, "one write, as the machine")
	snap := &sprint.Snapshot{}
	require.NoError(t, st.RoutesInto(ctx, snap))
	assert.False(t, snap.Providers["openrouter"].Funded, "applied: the deal reads funded=false")
	props, err := st.BudgetProps(ctx)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(props[sprint.PropUnfundedOwed("openrouter")], "done "), "the flip is recorded done: %s", props[sprint.PropUnfundedOwed("openrouter")])

	// a second pass writes nothing
	out.Reset()
	ta.a.flipUnfunded(ctx, st, ta.a.flipConfigOf(), &out)
	assert.Empty(t, out.String())
	assert.Len(t, machineChanges(t, cfg, "openrouter"), 1, "a second pass writes nothing")

	// a config store that does not answer: the flip stays owed, the line says so
	tb, cfgB, _, stB := fundingApp(t, map[string]bool{"opencode": true})
	restOnCredit(t, tb, stB, "opencode")
	out.Reset()
	tb.a.flipUnfunded(ctx, stB, func(context.Context, *store.Store) (ntconfig.Store, ntconfig.Applier, func(), error) {
		return nil, nil, nil, errors.New("dial tcp: connection refused")
	}, &out)
	assert.Contains(t, out.String(), "UNFUNDED-FLIP OWED provider=opencode: the config store did not answer")
	propsB, err := stB.BudgetProps(ctx)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(propsB[sprint.PropUnfundedOwed("opencode")], "owed "), "still owed")
	assert.Empty(t, machineChanges(t, cfgB, "opencode"))
}

// TestFundedVerbIsTheOwners: the funded verb is the owner's alone: the coordinator and the
// machine are refused with the owner's remedy, the owner ends the sprint store's rest, and
// nova-config is untouched in all three. The reversed witness: the coordinator's old path
// (sprint.Funded with no owner check, the verb back on classCoordinator) ends the rest.
//
// tla/ProviderFunding.tla OwnerFund, MachineFundAttempt, OnlyOwnerFunds
// (MCProviderFundingBrokenMachineFunds)
func TestFundedVerbIsTheOwners(t *testing.T) {
	t.Parallel()
	ta, cfg, _, st := fundingApp(t, map[string]bool{"openrouter": false})
	ctx := context.Background()
	restOnCredit(t, ta, st, "openrouter")
	resting := func() bool {
		props, err := st.BudgetProps(ctx)
		require.NoError(t, err)
		tb := sprint.NewTable(sprint.Fleet)
		tb.SetProps(props)
		return sprint.ProviderRests(tb)["openrouter"].Resting(ta.a.now())
	}
	before, err := cfg.History(ctx, ntconfig.KindProvider, "openrouter")
	require.NoError(t, err)
	for _, who := range []string{"coordinator", sprint.MachineActor} {
		code, out, errs := ta.do("funded openrouter --reason paid --actor " + who)
		assert.NotEqual(t, 0, code, "%s: %s%s", who, out, errs)
		assert.Contains(t, out+errs, "nova-config provider set openrouter --funded true --actor glenn", who)
		assert.True(t, resting(), "%s ended no rest", who)
	}
	ta.ok("funded openrouter --reason 'paid $50' --actor glenn")
	assert.False(t, resting(), "the owner ends the sprint store's rest")
	after, err := cfg.History(ctx, ntconfig.KindProvider, "openrouter")
	require.NoError(t, err)
	assert.Equal(t, before, after, "funded never writes nova-config")

	// the reversed witness: the coordinator's old path ends the rest
	restOnCredit(t, ta, st, "openrouter")
	require.True(t, resting())
	old := store.FundedStep(sprint.FundedReq{Provider: "openrouter", Reason: "paid", Who: "coordinator"})
	_, err = st.Run(ctx, old)
	require.NoError(t, err)
	assert.False(t, resting(), "witness: on classCoordinator the coordinator ends the rest")
}

// TestRoutesWakeRefusesAnUnfundedProvider: routes wake refuses a provider, or a route of a
// provider, whose row says funded=false, naming the owner's verb, and its rest holds; a
// provider whose row says funded=true is woken as before.
//
// tla/ProviderFunding.tla RouteEnable, NoEnableOnUnfunded (MCProviderFundingBrokenEnableUnfunded)
func TestRoutesWakeRefusesAnUnfundedProvider(t *testing.T) {
	t.Parallel()
	ta, _, _, st := fundingApp(t, map[string]bool{"openrouter": false, "deepseek": true})
	restOnCredit(t, ta, st, "openrouter")
	restOnCredit(t, ta, st, "deepseek")
	for _, target := range []string{"openrouter", "pro-openrouter"} {
		code, out, errs := ta.do("routes wake " + target + " --reason r")
		assert.NotEqual(t, 0, code, "%s: %s%s", target, out, errs)
		assert.Contains(t, out+errs, "provider openrouter unfunded (the owner's to fund)", target)
		assert.Contains(t, out+errs, "nova-config provider set openrouter --funded true --actor glenn", target)
	}
	props, err := st.BudgetProps(context.Background())
	require.NoError(t, err)
	tb := sprint.NewTable(sprint.Fleet)
	tb.SetProps(props)
	assert.True(t, sprint.ProviderRests(tb)["openrouter"].Resting(ta.a.now()), "its rest holds")
	ta.ok("routes wake deepseek --reason r")
}
