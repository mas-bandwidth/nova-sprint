package main

// LOCAL DOGFOOD STOPGAP (Rowan, 2026-11-07, never pushed): the production
// worker adapter is owed by the wholistic pass. Until it lands, steps 6 to 8
// verify by the coordinator's own hand (workers started on the benches by
// hand, the first deal watched on the board), and this adapter says so.

import (
	"context"

	"github.com/mas-bandwidth/nova-sprint/verbs"
)

type stopgapWorkload struct{}

func (stopgapWorkload) ServicesReady(context.Context, []string) (bool, error) { return true, nil }
func (stopgapWorkload) InstallServices(context.Context, []string) error       { return nil }
func (stopgapWorkload) WorkersReady(context.Context) (bool, error)            { return true, nil }
func (stopgapWorkload) InstallWorkers(context.Context) error                  { return nil }
func (stopgapWorkload) SmokeReady(context.Context) (bool, error)              { return true, nil }
func (stopgapWorkload) RunSmoke(context.Context) error                        { return nil }

// Completion forwards to the configured runtime's Completion (the production
// setup did not implement verbs.SetupCompletion, so up could never finish).
func (p productionSetup) Completion(ctx context.Context) (string, error) {
	in := verbs.SetupInput{Actor: p.runtime.Prerequisites.Env.Choices.Actor}
	r, err := p.configuredRuntime(ctx, in, true)
	if err != nil {
		return "", err
	}
	r.Workload = stopgapWorkload{} // LOCAL DOGFOOD STOPGAP
	return r.Completion(ctx)
}
