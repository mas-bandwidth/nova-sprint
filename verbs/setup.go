package verbs

import (
	"context"
	"fmt"
	"strings"
)

// SetupInput contains only non-secret choices. A service key is acquired by
// the setup adapter at a hidden terminal prompt and sent directly to secrets.
type SetupInput struct{ Actor, Dash, Services, Key, RunOn, Version string }

// SetupCheck describes one probe. Action is true only when the supplied
// choices make the next idempotent action possible without another question.
type SetupCheck struct {
	Verified, Action bool
	Line, Next       string
}

// Setup owns the effects of each setup step. Verify must inspect actual state;
// Act may persist an answer or perform an idempotent action, but does not
// itself prove the step verified. See tla/Setup.tla.
// SetupLock serializes an entire walk across hosts on the database.
type SetupLock interface {
	Acquire(context.Context) (SetupLease, error)
}
type SetupLease interface {
	Monitor(context.Context) (context.Context, context.CancelFunc)
	Check(context.Context) error
	Close() error
}

type Setup interface {
	Verify(context.Context, int, SetupInput) (SetupCheck, error)
	Act(context.Context, int, SetupInput) error
}

func parseSetup(args []string, actor string) (SetupInput, bool) {
	in := SetupInput{Actor: actor}
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) || args[i+1] == "" {
			return in, false
		}
		value := args[i+1]
		switch args[i] {
		case "--as":
			in.Actor = value
		case "--dash":
			in.Dash = value
		case "--services":
			in.Services = value
		case "--key":
			in.Key = value
		case "--run-on":
			in.RunOn = value
		case "--version":
			in.Version = value
		default:
			return in, false
		}
		i++
	}
	if in.Dash != "" && in.Dash != "tailnet" && !strings.HasPrefix(in.Dash, "public=") {
		return in, false
	}
	return in, true
}

func (e *Engine) up(ctx context.Context, args []string) Result {
	in, ok := parseSetup(args, e.Actor)
	if !ok {
		return refused("up", "invalid arguments", "nova-sprint help up")
	}
	if e.Setup == nil {
		return unavailable("up")
	}
	var out strings.Builder
	result, stop := e.setupStep(ctx, 1, in, &out)
	if stop {
		return result
	}
	if e.SetupLock == nil {
		return unavailable("up")
	}
	lease, err := e.SetupLock.Acquire(ctx)
	if err != nil {
		return operationalFailure("up", err)
	}
	defer lease.Close()
	ctx, stopMonitor := lease.Monitor(ctx)
	defer stopMonitor()
	out.Reset() // step 1 is re-probed under the lock, not repeated in output.
	for step := 1; step <= 8; step++ {
		if err := lease.Check(ctx); err != nil {
			return operationalFailure("up", err)
		}
		result, stop := e.setupStep(ctx, step, in, &out)
		if err := context.Cause(ctx); err != nil {
			return operationalFailure("up", err)
		}
		if err := lease.Check(ctx); err != nil {
			return operationalFailure("up", err)
		}
		if stop {
			return result
		}
	}
	out.WriteString("UP DONE\nnext: nova-sprint stream add <name> --repo <owner/name>\n")
	return Result{out.String(), 0}
}

func (e *Engine) setupStep(ctx context.Context, step int, in SetupInput, out *strings.Builder) (Result, bool) {
	check, err := e.Setup.Verify(ctx, step, in)
	if err != nil {
		r := operationalFailure("up", err)
		return Result{out.String() + r.Output, r.Exit}, true
	}
	if !check.Verified && check.Action {
		if err = e.Setup.Act(ctx, step, in); err != nil {
			r := operationalFailure("up", err)
			return Result{out.String() + r.Output, r.Exit}, true
		}
		check, err = e.Setup.Verify(ctx, step, in)
		if err != nil {
			r := operationalFailure("up", err)
			return Result{out.String() + r.Output, r.Exit}, true
		}
	}
	if check.Line != "" {
		fmt.Fprintln(out, check.Line)
	}
	if check.Verified {
		return Result{}, false
	}
	if check.Next == "" {
		check.Next = "nova-sprint up"
	}
	fmt.Fprintln(out, "next: "+check.Next)
	return Result{out.String(), 1}, true
}
