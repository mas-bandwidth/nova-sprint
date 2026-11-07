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
type FleetMachine struct {
	Name, Address string
	Roles         []string
	Cores         *int
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
			Cores                *int
		} `json:"fields"`
	} `json:"items"`
}

// Machines reads the actual machine list from nova-config, rather than guessing
// from the local host. The roles determine benches and service candidates.
func (f FleetServices) Machines(ctx context.Context) ([]FleetMachine, error) {
	out, err := f.command(ctx, "nova-config", "machine", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("nova-config machine list failed")
	}
	var list configMachines
	if json.Unmarshal(out, &list) != nil || list.Result.Verb != "machine list" || list.Result.Status != "ok" || list.Result.Exit != 0 || list.Facts.Kind != "machine" || list.Facts.Rows != len(list.Items) {
		return nil, fmt.Errorf("nova-config returned invalid machine list")
	}
	result := make([]FleetMachine, 0, len(list.Items))
	seen := map[string]bool{}
	for _, item := range list.Items {
		v := item.Fields
		if item.Kind != "machine" || !fleetName.MatchString(v.Name) || seen[v.Name] || (v.Cores != nil && *v.Cores < 0) {
			return nil, fmt.Errorf("nova-config returned invalid machine list")
		}
		seen[v.Name] = true
		roles := []string{}
		if v.Roles != "" {
			roles = strings.Split(v.Roles, ",")
		}
		for _, role := range roles {
			if role != "bench" && role != "runner" && role != "services" {
				return nil, fmt.Errorf("nova-config returned invalid machine role")
			}
		}
		result = append(result, FleetMachine{Name: v.Name, Address: v.Address, Roles: roles, Cores: v.Cores})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// SprintSeats uses the chosen run service's actual seat plus the existing
// swarm seat for each configured bench. Public pull hosts receive no DSN.
func SprintSeats(machines []FleetMachine, run FleetService, publicHost string) ([]SprintSeat, error) {
	if !fleetName.MatchString(run.Machine) || !fleetName.MatchString(run.Seat) {
		return nil, fmt.Errorf("run seat unavailable")
	}
	if publicHost != "" && publicHost == run.Machine {
		return nil, fmt.Errorf("public dashboard host cannot hold the sprint credential")
	}
	seats := []SprintSeat{{Seat: run.Seat, Machine: run.Machine}}
	seen := map[string]bool{run.Seat + "\x00" + run.Machine: true}
	found := false
	for _, machine := range machines {
		if machine.Name == run.Machine {
			if !slices.Contains(machine.Roles, "services") {
				return nil, fmt.Errorf("run host lacks services role")
			}
			found = true
		}
		if machine.Name == publicHost && slices.Contains(machine.Roles, "bench") {
			return nil, &OperationError{Problem: "public dashboard host is also a bench", Fix: "remove the bench role from " + machine.Name, Exit: 1}
		}
		if machine.Name == publicHost || !slices.Contains(machine.Roles, "bench") {
			continue
		}
		seat := "swarm-" + machine.Name
		if !fleetName.MatchString(seat) {
			return nil, fmt.Errorf("bench seat unavailable")
		}
		key := seat + "\x00" + machine.Name
		if !seen[key] {
			seats = append(seats, SprintSeat{Seat: seat, Machine: machine.Name})
			seen[key] = true
		}
	}
	if !found {
		return nil, fmt.Errorf("run host absent from inventory")
	}
	return seats, nil
}
