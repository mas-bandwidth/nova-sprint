package verbs

import (
	"context"

	"github.com/mas-bandwidth/nova-sprint/run"
)

// PGSetupRun reads the chooser's owner API and nova-bus; service records alone
// never establish that run is alive or that the requested host holds the lock.
type PGSetupRun struct {
	DSN, Password string
	Env           ExecEnvironment
	Inspect       func(context.Context, string, string) (run.ChooserStatus, error)
}

func (p PGSetupRun) Ready(ctx context.Context, host string) (bool, error) {
	inspect := p.Inspect
	if inspect == nil {
		inspect = run.InspectChooser
	}
	status, err := inspect(ctx, p.DSN, p.Password)
	if err != nil {
		return false, err
	}
	if !status.Held || status.Operation != "run" || status.Host != host {
		return false, nil
	}
	hooked, _, err := p.Env.Hooked(ctx, "sprint")
	return hooked, err
}
