package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/verbs"
)

var setupSecretName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// setupBusCommand chooses from key names only; it never reads a secret value.
func setupBusCommand(cfg productionSetupConfig, required func(context.Context) (bool, error), run func(context.Context, bool, string, ...string) ([]byte, error)) verbs.CommandOutput {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "nova-bus" || len(args) != 1 || args[0] != "who" || run == nil || required == nil {
			return nil, fmt.Errorf("bus probe configuration unavailable")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s := cfg.Secrets
		out, err := run(ctx, true, "nova-secrets", "names", "--store", s.Store, "--as", s.Seat)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("bus secret names unavailable")
		}
		present, err := setupBusKeyPresent(out)
		if err != nil {
			return nil, err
		}
		if present {
			return setupBusAuthenticated(ctx, cfg, run)
		}
		declared, err := required(ctx)
		if err != nil {
			return nil, fmt.Errorf("bus service credentials unavailable")
		}
		if declared || cfg.BusUser != "" {
			return nil, &verbs.OperationError{Problem: "NOVA_BUS_PASSWORD is missing from the control seat", Fix: "restore the bus credential to the control seat and rerun nova-sprint up", Exit: 1}
		}
		return run(ctx, true, "env", "NOVA_BUS_REDIS="+cfg.BusRedis, "NOVA_BUS_USER=", "nova-bus", "who")
	}
}

func setupBusKeyPresent(out []byte) (bool, error) {
	if len(out) == 0 {
		return false, nil
	}
	if len(out) > 1<<20 || out[len(out)-1] != '\n' {
		return false, fmt.Errorf("bus secret names invalid")
	}
	present := false
	for _, name := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if !setupSecretName.MatchString(name) {
			return false, fmt.Errorf("bus secret names invalid")
		}
		if name == "NOVA_BUS_PASSWORD" {
			present = true
		}
	}
	return present, nil
}

func setupBusAuthenticated(ctx context.Context, cfg productionSetupConfig, run func(context.Context, bool, string, ...string) ([]byte, error)) ([]byte, error) {
	s := cfg.Secrets
	args := []string{"exec", "--only", "NOVA_BUS_PASSWORD", "--store", s.Store, "--key", s.KeyFile, "--as", s.Seat, "--", "env", "NOVA_BUS_REDIS=" + cfg.BusRedis, "NOVA_BUS_USER=" + cfg.BusUser, "nova-bus", "who"}
	return run(ctx, true, "nova-secrets", args...)
}
