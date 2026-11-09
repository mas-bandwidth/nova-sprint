package verbs

import (
	"context"
	"fmt"
)

// SetupPrerequisites is step 1: the fleet and nova-tools, observed again on
// every call; it has no checkpoint.
type SetupPrerequisites struct {
	Env ExecEnvironment
	Pin string
	// Installed is nova-tools' installed release (nil: InstalledTools).
	Installed func(context.Context) (string, error)
}

func (s SetupPrerequisites) Tools(ctx context.Context) (SetupCheck, error) {
	ok, why, err := s.Env.Fleet(ctx)
	if err != nil {
		return SetupCheck{}, err
	}
	if !ok {
		return SetupCheck{Line: "UP 1/7 nova-tools: fleet not ready (" + why + ")", Next: "nova-fleet check"}, nil
	}
	installed := s.Installed
	if installed == nil {
		installed = InstalledTools
	}
	out, err := installed(ctx)
	if err != nil {
		return SetupCheck{}, s.unknown(err)
	}
	version, parsed := parseToolsRelease(out)
	if !parsed {
		return SetupCheck{}, s.unknown(fmt.Errorf("%q is not a release vMAJOR.MINOR.PATCH", out))
	}
	enough, err := atLeast(version, s.Pin)
	if err != nil {
		return SetupCheck{}, s.unknown(err)
	}
	if !enough {
		return SetupCheck{}, &OperationError{Problem: fmt.Sprintf("nova-tools %s is below pin %s", version, s.Pin), Fix: toolsFix(s.Pin), Exit: 1}
	}
	return SetupCheck{Verified: true, Line: fmt.Sprintf("UP 1/7 nova-tools: fleet green; nova-tools %s at or above pin %s: VERIFIED", version, s.Pin)}, nil
}
func (s SetupPrerequisites) unknown(err error) error {
	return &OperationError{Problem: "nova-tools version unknown (" + errText(err) + ")", Fix: toolsFix(s.Pin), Exit: 1}
}
