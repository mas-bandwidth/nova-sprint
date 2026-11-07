package verbs

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// SetupPrerequisites implements the nova-tools and coordinator portions of the
// walk. Every call observes the tools or durable answers again; it has no checkpoint.
type SetupPrerequisites struct {
	Env ExecEnvironment
	Pin string
	// Installed is nova-tools' installed release (nil: InstalledTools).
	Installed func(context.Context) (string, error)
}

func (s SetupPrerequisites) Tools(ctx context.Context) (SetupCheck, error) {
	ok, _, err := s.Env.Fleet(ctx)
	if err != nil {
		return SetupCheck{}, err
	}
	if !ok {
		return SetupCheck{Line: "UP 1/8 nova-tools: fleet not ready", Next: "nova-fleet check"}, nil
	}
	installed := s.Installed
	if installed == nil {
		installed = InstalledTools
	}
	out, err := installed(ctx)
	version, parsed := parseToolsRelease(out)
	if err != nil || !parsed {
		return SetupCheck{}, s.unknown()
	}
	enough, err := atLeast(version, s.Pin)
	if err != nil {
		return SetupCheck{}, s.unknown()
	}
	if !enough {
		return SetupCheck{}, &OperationError{Problem: fmt.Sprintf("nova-tools %s is below pin %s", version, s.Pin), Fix: toolsFix(s.Pin), Exit: 1}
	}
	return SetupCheck{Verified: true, Line: fmt.Sprintf("UP 1/8 nova-tools: fleet green; nova-tools %s at or above pin %s: VERIFIED", version, s.Pin)}, nil
}
func (s SetupPrerequisites) unknown() error {
	return &OperationError{Problem: "nova-tools version unknown", Fix: toolsFix(s.Pin), Exit: 1}
}

var setupActor = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func (s SetupPrerequisites) Coordinator(ctx context.Context, actor string) (SetupCheck, error) {
	if s.Env.Choices.Store == nil {
		return SetupCheck{}, fmt.Errorf("setup choices unavailable")
	}
	saved, found, err := s.Env.Choices.Get(ctx, "coordinator")
	if err != nil {
		return SetupCheck{}, err
	}
	if !found {
		saved = actor
	}
	if saved == "" {
		return SetupCheck{Line: "UP 3/8 coordinator: name is required", Next: "nova-sprint up --as <name>"}, nil
	}
	if !setupActor.MatchString(saved) {
		return SetupCheck{}, fmt.Errorf("invalid coordinator name")
	}
	hooked, recipe, err := s.Env.Hooked(ctx, saved)
	if err != nil {
		return SetupCheck{}, err
	}
	if !hooked {
		fix := "run " + recipe
		if strings.HasPrefix(recipe, "nova-bus hook --as ") {
			fix += " under your session's Monitor tool"
		}
		return SetupCheck{Line: "UP 3/8 coordinator " + saved + ": not hooked", Refusal: &OperationError{Problem: saved + " is not hooked in", Fix: fix, Exit: 1}}, nil
	}
	if !found {
		return SetupCheck{Action: true}, nil
	}
	return SetupCheck{Verified: true, Line: "UP 3/8 coordinator " + saved + ": VERIFIED"}, nil
}
func (s SetupPrerequisites) SaveCoordinator(ctx context.Context, actor string) error {
	if !setupActor.MatchString(actor) {
		return fmt.Errorf("invalid coordinator name")
	}
	return s.Env.Choices.Save(ctx, "coordinator", actor)
}
