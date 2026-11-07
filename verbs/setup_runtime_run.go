package verbs

import (
	"context"
	"fmt"
	"strings"

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
	if !status.Held || status.Operation != "run" || strings.Split(status.Host, ".")[0] != strings.Split(host, ".")[0] {
		return false, fmt.Errorf("chooser lock: held=%v operation=%q host=%q, wanted run on %q", status.Held, status.Operation, status.Host, host)
	}
	hooked, _, err := p.Env.Hooked(ctx, "sprint")
	if err == nil && !hooked {
		return false, fmt.Errorf("the actor is not hooked to sprint on the bus")
	}
	return hooked, err
}
