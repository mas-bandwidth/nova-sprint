package verbs

import (
	"context"
	"fmt"
	"slices"
)

// Completion reports only re-observed setup state and durable answers; it is
// called after all eight steps verify and never reports an unverified worker.
func (r RuntimeSetup) Completion(ctx context.Context) (string, error) {
	host, hostSaved, err := r.answer(ctx, "run-on", "", "")
	if err != nil {
		return "", err
	}
	choice, dashSaved, err := r.answer(ctx, "dashboard", "", "")
	if err != nil {
		return "", err
	}
	actor, actorSaved, err := r.answer(ctx, "coordinator", "", "")
	if err != nil {
		return "", err
	}
	if !hostSaved || !dashSaved || !actorSaved || !fleetName.MatchString(host) || !setupActor.MatchString(actor) {
		return "", fmt.Errorf("setup answers unavailable: run-on saved %t valid %t, dashboard saved %t, coordinator saved %t valid %t", hostSaved, fleetName.MatchString(host), dashSaved, actorSaved, setupActor.MatchString(actor))
	}
	for _, step := range []int{3, 4, 5, 7} {
		check, err := r.Verify(ctx, step, SetupInput{})
		if err != nil {
			return "", err
		}
		if !check.Verified {
			return "", fmt.Errorf("setup state changed before completion: step %d no longer verifies", step)
		}
	}
	target, ok := r.Dashboards[choice]
	if !ok {
		return "", fmt.Errorf("dashboard configuration unavailable: no dashboard target for choice %q", Redact(choice))
	}
	publicHost := ""
	if target.Public {
		if target.Service == nil {
			return "", fmt.Errorf("public dashboard host unavailable: choice %q names no service", Redact(choice))
		}
		publicHost = target.Service.Machine
	}
	machines, err := r.Services.Machines(ctx)
	if err != nil {
		return "", err
	}
	if _, err = SprintSeats(machines, r.RunService, publicHost); err != nil {
		return "", err
	}
	workers := 0
	for _, machine := range machines {
		if slices.Contains(machine.Roles, "bench") {
			workers++
		}
	}
	return fmt.Sprintf("UP DONE run on %s; dashboard %s; %d workers up; coordinator %s", host, target.URL, workers, actor), nil
}
