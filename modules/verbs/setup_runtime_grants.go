package verbs

import (
	"context"
	"fmt"
	"path/filepath"
)

// FleetGrants copies encrypted key grants, never plaintext, to an existing
// pinned machine seat and refreshes only the selected machine.
type FleetGrants struct {
	Store, KeyFile, Pins, ControlSeat string
	Command                           CommandOutput
}

func (g FleetGrants) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	if g.Command != nil {
		return g.Command(ctx, name, args...)
	}
	return toolOutput(ctx, name, args...)
}
func (g FleetGrants) valid(seat, machine, name string) bool {
	return fleetName.MatchString(seat) && fleetName.MatchString(machine) && fleetKey.MatchString(name) && fleetName.MatchString(g.ControlSeat) && filepath.IsAbs(g.Store) && filepath.IsAbs(g.KeyFile) && filepath.IsAbs(g.Pins)
}
func (g FleetGrants) invalid(seat, machine, name string) error {
	return fmt.Errorf("invalid fleet grant: seat %q (%t), machine %q (%t), key name %q (%t), control seat %q (%t), store/key/pins absolute %t/%t/%t",
		Redact(seat), fleetName.MatchString(seat), Redact(machine), fleetName.MatchString(machine), Redact(name), fleetKey.MatchString(name),
		Redact(g.ControlSeat), fleetName.MatchString(g.ControlSeat), filepath.IsAbs(g.Store), filepath.IsAbs(g.KeyFile), filepath.IsAbs(g.Pins))
}
func (g FleetGrants) Grant(ctx context.Context, seat, machine, name string) error {
	if !g.valid(seat, machine, name) {
		return g.invalid(seat, machine, name)
	}
	_, err := g.command(ctx, "nova-secrets", "grant", name, "--to", seat, "--pins", g.Pins, "--store", g.Store, "--key", g.KeyFile, "--as", g.ControlSeat)
	if err != nil {
		return fmt.Errorf("nova-secrets grant failed: %w", err)
	}
	return nil
}
func (g FleetGrants) Refresh(ctx context.Context, seat, machine, name string) error {
	if !g.valid(seat, machine, name) {
		return g.invalid(seat, machine, name)
	}
	_, err := g.command(ctx, "nova-fleet", "secrets-refresh", "--", "-l", machine, "-e", "nova_seal_store="+g.Store)
	if err != nil {
		return cause("nova-fleet secrets-refresh failed", err)
	}
	return nil
}
