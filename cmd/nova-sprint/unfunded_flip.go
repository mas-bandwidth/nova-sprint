package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	ntconfig "github.com/mas-bandwidth/nova-tools/pkg/config"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/redis/go-redis/v9"
)

// The flip to unfunded, the run loop's half (internal/sprint/unfunded.go has the plan's; the
// owner, 2026-10-19: "when a route is not funded, it must FLIP into unfunded *automatically*
// in the config"). Each pass, outside the tick's plan, the loop reads the fleet table's
// properties and the applied provider rows (no card) and, when a provider's credit refusal
// owes a flip (sprint.FlipsDue), records it (sprint.UnfundedOwed: one per provider, never per
// route) and writes it through the normal config path: nova-config's store, the provider row
// set to funded=false by the machine with its note, then the provider kind applied, so the
// next tick's deal reads funded=false and draws none of its routes. A config store that does
// not answer leaves the flip owed, said on the loop's line and in the doctor, and tried again
// next pass; the provider rests in the sprint's store meanwhile. A row already false writes
// nothing (OneWritePerProvider).
//
// tla/ProviderFunding.tla BillingFailure, OneWritePerProvider; docs/SPEC-SPRINT.md section 5

// flipConfigBound is how long one pass waits for nova-config's store, as fleet sync does.
const flipConfigBound = 30 * time.Second

// flipConfigFn opens nova-config's store and the Redis side of its apply for one pass; the
// func it returns closes them.
type flipConfigFn func(ctx context.Context, st *store.Store) (ntconfig.Store, ntconfig.Applier, func(), error)

// flipConfigs is a test's config store by app (config.NewMem and a fake applier): a unit
// test opens no socket. An app with none opens the live store (openFlipConfig).
var flipConfigs sync.Map

// flipConfigOf is the app's config opener: the test's, else the live one.
func (a *app) flipConfigOf() flipConfigFn {
	if f, ok := flipConfigs.Load(a); ok {
		return f.(flipConfigFn)
	}
	return a.openFlipConfig
}

// openFlipConfig is nova-config's Postgres store by the config tool's own address rules
// (config.ResolveDSN, config.OpenPG; a schema behind this binary's refused), and the Redis
// applier over the sprint store's own Redis, where the deal reads the provider rows.
func (a *app) openFlipConfig(ctx context.Context, st *store.Store) (ntconfig.Store, ntconfig.Applier, func(), error) {
	r, ok := st.B.(*store.Redis)
	if !ok {
		return nil, nil, nil, errors.New("the sprint's store is not Redis: there is no provider row to apply on it")
	}
	c, ok := r.C.(*redis.Client)
	if !ok {
		return nil, nil, nil, errors.New("the sprint's Redis is not a single client: the apply writes through one")
	}
	dsn, err := ntconfig.ResolveDSN("", a.getenv)
	if err != nil {
		return nil, nil, nil, err
	}
	pg, err := ntconfig.OpenPG(ctx, dsn)
	if err != nil {
		return nil, nil, nil, err
	}
	have, err := pg.Version(ctx)
	if err != nil {
		pg.Close()
		return nil, nil, nil, err
	}
	if all, err := ntconfig.Migrations(); err == nil && have < len(all) {
		pg.Close()
		return nil, nil, nil, fmt.Errorf("schema config is at version %d and this binary carries %d; run: nova-config migrate", have, len(all))
	}
	return pg, &ntconfig.RedisApplier{Client: c, Now: a.now}, func() { pg.Close() }, nil
}

// flipUnfunded is one pass of the flip: nothing read past the fleet table's properties and
// the provider rows unless a flip is due; then the owed flips recorded (the plan step), each
// written through nova-config as the machine and applied, and recorded done. It says one
// line for each flip written or left owed.
func (a *app) flipUnfunded(ctx context.Context, st *store.Store, open flipConfigFn, stdout io.Writer) {
	at := a.now().Format("15:04:05")
	props, err := st.BudgetProps(ctx)
	if err != nil {
		return // the tick says a store that does not answer
	}
	snap := &sprint.Snapshot{}
	if err := st.RoutesInto(ctx, snap); err != nil {
		return
	}
	if !sprint.FlipsDue(props, snap.Providers, a.now()) {
		return
	}
	// the plan step: one flip owed per provider whose credit rest finds its row funded=true
	// (sprint.UnfundedOwed; tla/ProviderFunding.tla BillingFailure)
	owe := store.Step{Verb: "unfunded owed", Load: []string{sprint.Fleet}, Routes: true, Actor: sprint.MachineActor, Plan: sprint.UnfundedOwed}
	if _, err := st.Run(ctx, owe); err != nil {
		fmt.Fprintf(stdout, "%s UNFUNDED-FLIP FAIL the flips owed were not recorded: %s\n", at, oneline.Escape(err.Error()))
		return
	}
	if props, err = st.BudgetProps(ctx); err != nil {
		return
	}
	owed := sprint.FlipsOwed(props)
	if len(owed) == 0 {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, flipConfigBound)
	defer cancel()
	cfg, ap, closeCfg, err := open(cctx, st)
	if err != nil {
		for _, f := range owed {
			fmt.Fprintf(stdout, "%s UNFUNDED-FLIP OWED provider=%s: the config store did not answer: %s; its rest holds in the sprint's store; tried again next pass (nova-sprint doctor names it)\n", at, f.Provider, oneline.Escape(err.Error()))
		}
		return
	}
	defer closeCfg()
	for _, f := range owed {
		note, err := flipOne(cctx, cfg, ap, f, a.now())
		if err != nil {
			fmt.Fprintf(stdout, "%s UNFUNDED-FLIP OWED provider=%s: %s; its rest holds in the sprint's store; tried again next pass\n", at, f.Provider, oneline.Escape(err.Error()))
			continue
		}
		done := store.Step{Verb: "unfunded flipped", Load: []string{sprint.Fleet}, Actor: sprint.MachineActor,
			Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.UnfundedFlipped(s, f.Provider, f.Rest, note) }}
		if _, err := st.Run(ctx, done); err != nil {
			fmt.Fprintf(stdout, "%s UNFUNDED-FLIP FAIL provider=%s flipped but not recorded: %s; the next pass finds the row false and writes nothing\n", at, f.Provider, oneline.Escape(err.Error()))
			continue
		}
		fmt.Fprintf(stdout, "%s UNFUNDED-FLIP provider=%s funded=false actor=%s note=%s\n", at, f.Provider, sprint.MachineActor, oneline.Field(note))
	}
}

// flipOne writes one flip: the provider row set to funded=false as the machine, with the
// note, when it still says funded=true; nothing when it says false already (one write per
// provider, never per route: OneWritePerProvider); then the provider kind applied, so the
// sprint store's row says false too. It returns the note recorded.
func flipOne(ctx context.Context, cfg ntconfig.Store, ap ntconfig.Applier, f sprint.UnfundedFlip, now time.Time) (string, error) {
	row, found, err := cfg.Get(ctx, ntconfig.KindProvider, f.Provider)
	if err != nil {
		return "", err
	}
	if !found {
		return "nova-config has no provider row " + f.Provider + ": nothing written", nil
	}
	note := "the row said funded=false already: nothing written"
	if row.Fields["funded"] == "true" {
		note = f.ConfigNote(now)
		if _, _, err := cfg.Update(ntconfig.WithReason(ctx, note), ntconfig.KindProvider, f.Provider, map[string]string{"funded": "false", "note": note}, sprint.MachineActor); err != nil {
			return "", err
		}
	}
	if _, err := ntconfig.ApplyInRun(ctx, cfg, ap, ntconfig.KindProvider, sprint.MachineActor, false, false, nil, func(ntconfig.Op) {}); err != nil {
		return "", fmt.Errorf("nova-config apply --kind provider: %w", err)
	}
	return note, nil
}
