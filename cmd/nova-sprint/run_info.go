package main

import (
	"context"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

type pgRunInfo struct {
	dsn, password string
	inspect       func(context.Context, string, string) (run.ChooserStatus, error)
}

func (i pgRunInfo) Inspect(ctx context.Context) (verbs.RunStatus, error) {
	if i.password == "" {
		return verbs.RunStatus{}, fmt.Errorf("run status: NOVA_SPRINT_PG_PASSWORD is required")
	}
	dsn, err := setupRoleDSN(i.dsn, "nova_sprint")
	if err != nil {
		return verbs.RunStatus{}, fmt.Errorf("run status: configure a passwordless NOVA_PG_DSN")
	}
	inspect := i.inspect
	if inspect == nil {
		inspect = run.InspectChooser
	}
	status, err := inspect(ctx, dsn, i.password)
	if err != nil {
		return verbs.RunStatus{}, err
	}
	return verbs.RunStatus{Held: status.Held, Operation: status.Operation, Host: status.Host, ApplicationName: status.ApplicationName, PID: status.PID, BackendPID: status.BackendPID, Since: status.Since}, nil
}
