package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// FleetMachine is the setup-relevant, non-secret subset of a nova-config row.
// Address is its fleet (MagicDNS) address; Public is the optional public
// address nova-config holds for it (nova-tools 2.0.3, machine set --public),
// "" when unset.
type FleetMachine struct {
	Name, Address, Public string
	Roles                 []string
	Cores                 *int
}
type configMachines struct {
	Result struct {
		Verb, Status string
		Exit         int
	} `json:"result"`
	Facts struct {
		Kind string
		Rows int
	} `json:"facts"`
	Items []struct {
		Kind   string `json:"kind"`
		Fields struct {
			Name, Address, Roles string
			Public               string `json:"public"`
			Cores                *int
		} `json:"fields"`
	} `json:"items"`
}

// Machines reads the actual machine list from nova-config, rather than guessing
// from the local host. The roles determine benches and service candidates.
func (f FleetServices) Machines(ctx context.Context) ([]FleetMachine, error) {
	out, err := f.command(ctx, "nova-config", "machine", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("nova-config machine list --json: %w", err)
	}
	var list configMachines
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("nova-config machine list --json: %s", ParseFailure(err, out))
	}
	if list.Result.Verb != "machine list" || list.Result.Status != "ok" || list.Result.Exit != 0 || list.Facts.Kind != "machine" || list.Facts.Rows != len(list.Items) {
		return nil, fmt.Errorf("nova-config machine list --json: result %q %q exit %d, facts %q rows %d for %d items; first bytes %q",
			list.Result.Verb, list.Result.Status, list.Result.Exit, list.Facts.Kind, list.Facts.Rows, len(list.Items), firstBytes(out))
	}
	result := make([]FleetMachine, 0, len(list.Items))
	seen := map[string]bool{}
	for _, item := range list.Items {
		v := item.Fields
		if item.Kind != "machine" || !fleetName.MatchString(v.Name) || seen[v.Name] || (v.Cores != nil && *v.Cores < 0) {
			return nil, fmt.Errorf("nova-config machine list --json: item kind %q name %q is not a machine the sprint can name (duplicate, bad name or negative cores)", item.Kind, v.Name)
		}
		seen[v.Name] = true
		// Roles are nova-config's to validate: the sprint reads the tokens
		// it uses (bench, runner, services) and ignores every other.
		roles := []string{}
		for _, role := range strings.Split(v.Roles, ",") {
			if role = strings.TrimSpace(role); role == "bench" || role == "runner" || role == "services" {
				roles = append(roles, role)
			}
		}
		result = append(result, FleetMachine{Name: v.Name, Address: v.Address, Public: strings.TrimSpace(v.Public), Roles: roles, Cores: v.Cores})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// SprintSeats uses the chosen run service's actual seat plus the existing
// swarm seat for each configured bench. The public dashboard service gets
// no secret (a pull-only record without seat or keys, validPublicTarget):
// that is the safety, so a bench that also serves the public board keeps
// its seat for its worker (Glenn, 2026-11-07); the run machine never
// serves it.
func SprintSeats(machines []FleetMachine, run FleetService, publicHost string) ([]SprintSeat, error) {
	if !fleetName.MatchString(run.Machine) || !fleetName.MatchString(run.Seat) {
		return nil, fmt.Errorf("run seat unavailable: sprint-run machine %q seat %q is not a fleet name", Redact(run.Machine), Redact(run.Seat))
	}
	if publicHost != "" && publicHost == run.Machine {
		return nil, fmt.Errorf("public dashboard host cannot hold the sprint credential: %s also runs sprint-run", publicHost)
	}
	seats := []SprintSeat{{Seat: run.Seat, Machine: run.Machine}}
	seen := map[string]bool{run.Seat + "\x00" + run.Machine: true}
	found := false
	for _, machine := range machines {
		if machine.Name == run.Machine {
			if !slices.Contains(machine.Roles, "services") {
				return nil, fmt.Errorf("run host %s lacks services role (roles %s)", machine.Name, strings.Join(machine.Roles, ","))
			}
			found = true
		}
		if !slices.Contains(machine.Roles, "bench") {
			continue
		}
		seat := machine.Name
		if !fleetName.MatchString(seat) {
			return nil, fmt.Errorf("bench seat unavailable: machine %q is not a fleet name", Redact(seat))
		}
		key := seat + "\x00" + machine.Name
		if !seen[key] {
			seats = append(seats, SprintSeat{Seat: seat, Machine: machine.Name})
			seen[key] = true
		}
	}
	if !found {
		return nil, fmt.Errorf("run host %s absent from inventory of %d machines", run.Machine, len(machines))
	}
	return seats, nil
}
