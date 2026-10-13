package main

import (
	"context"
	"fmt"
	"io"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

// recheckStartedServer runs only after successful listener startup and its
// remaining startup gates. It uses the epoch captured before startup began.
// Every restart is safe: only conditions that ended close, including rollback.
// tla/JudgmentRecheck.tla ClosedOnlyProven and ExactlyOnce.
func (a *app) recheckStartedServer(ctx context.Context, pinned *store.Store, stdout io.Writer) error {
	res, err := pinned.RecheckJudgments(ctx, "server startup")
	if err != nil {
		return err
	}
	for _, line := range res.Said {
		fmt.Fprintf(stdout, "SERVER NOTE %s\n", oneline.Escape(line))
	}
	return nil
}
