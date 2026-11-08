package main

import (
	"context"

	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

// setupCommandWithBus keeps bus authentication separate from database secrets.
func setupCommandWithBus(cfg productionSetupConfig, fleet verbs.CommandOutput) verbs.CommandOutput {
	return setupCommandWithBusRunner(cfg, fleet, runSetupCommand)
}

func setupCommandWithBusRunner(cfg productionSetupConfig, fleet verbs.CommandOutput, run func(context.Context, bool, string, ...string) ([]byte, error)) verbs.CommandOutput {
	p := productionSetup{cfg: cfg, command: fleet}
	required := func(ctx context.Context) (bool, error) {
		// Before the sprint credential exists, there is no sprint-authenticated
		// service inventory. The probe still fails if Redis requires auth.
		if cfg.SprintPassword == "" {
			return false, nil
		}
		rows, err := p.listServices(ctx)
		if err != nil {
			return false, err
		}
		for _, row := range rows {
			if row.Name == "sprint-run" {
				for _, key := range splitServiceKeys(row.Keys) {
					if key == "NOVA_BUS_PASSWORD" {
						return true, nil
					}
				}
			}
		}
		return false, nil
	}
	bus := setupBusCommand(cfg, required, run)
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "nova-bus" {
			return bus(ctx, name, args...)
		}
		return fleet(ctx, name, args...)
	}
}
