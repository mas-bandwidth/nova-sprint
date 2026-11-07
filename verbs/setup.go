package verbs

import (
	"context"
	"fmt"
	"strings"
)

// SetupInput contains only non-secret choices. A service key is acquired by
// the setup adapter at a hidden terminal prompt and sent directly to secrets.
type SetupInput struct{ Actor, Dash, Services, Key, RunOn, Version, Continuation string }

// SetupCheck describes one probe. Action is true only when the supplied
// choices make the next idempotent action possible without another question.
type SetupCheck struct {
	Verified, Action bool
	Line, Next       string
	Refusal          *OperationError
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

// SetupHandoff correlates the admin bootstrap with a fresh sprint-credential walk.
type SetupHandoff interface {
	PrepareHandoff(context.Context, SetupInput) (string, bool, error)
	ValidateHandoff(context.Context, string, SetupInput) error
}

type SetupCompletion interface {
	Completion(context.Context) (string, error)
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
		case "--continuation":
			in.Continuation = value
		default:
			return in, false
		}
		i++
	}
	if in.Continuation != "" && !validContinuationID(in.Continuation) {
		return in, false
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
	result, stop := e.setupStep(ctx, 1, in, &out, "bootstrap")
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
	closed := false
	defer func() {
		if !closed {
			_ = lease.Close()
		}
	}()
	monitored, cancel := lease.Monitor(ctx)
	defer cancel()
	result, id := e.walkSetup(monitored, lease, in)
	cancel()
	err = lease.Close()
	closed = true
	e.observeSetup(SetupObservation{Kind: "lease-closed", Scope: "handoff", Failed: err != nil})
	if err != nil {
		return operationalFailure("up", err)
	}
	if id == "" {
		return result
	}
	if err := ctx.Err(); err != nil {
		return operationalFailure("up", err)
	}
	if e.ReportContinuation == nil {
		return unavailable("up")
	}
	err = e.ReportContinuation(ctx, id, in)
	e.observeSetup(SetupObservation{Kind: "handoff-signaled", Scope: "handoff", Failed: err != nil})
	if err != nil {
		return operationalFailure("up", err)
	}
	return Result{"", 0}
}
func (e *Engine) walkSetup(ctx context.Context, lease SetupLease, in SetupInput) (result Result, continuation string) {
	e.observeSetup(SetupObservation{Kind: "begin", Scope: "walk"})
	currentStep := 0
	defer func() {
		kind := "stop"
		if continuation != "" {
			kind = "handoff-prepared"
		} else if result.Exit == 0 {
			kind = "complete"
		}
		e.observeSetup(SetupObservation{Kind: kind, Scope: "walk", Step: currentStep, Failed: result.Exit != 0})
	}()
	handoff, hasHandoff := e.Setup.(SetupHandoff)
	if result, stop := e.validateSetupHandoff(ctx, lease, in, handoff, hasHandoff); stop {
		return result, ""
	}
	var out strings.Builder
	for step := 1; step <= 8; step++ {
		currentStep = step
		if err := context.Cause(ctx); err != nil {
			return operationalFailure("up", err), ""
		}
		if err := lease.Check(ctx); err != nil {
			return operationalFailure("up", err), ""
		}
		result, stop := e.setupStep(ctx, step, in, &out, "walk")
		if err := context.Cause(ctx); err != nil {
			return operationalFailure("up", err), ""
		}
		if err := lease.Check(ctx); err != nil {
			return operationalFailure("up", err), ""
		}
		if stop {
			return result, ""
		}
		if step == 2 && in.Continuation == "" && hasHandoff {
			id, needed, err := handoff.PrepareHandoff(ctx, in)
			e.observeSetup(SetupObservation{Kind: "prepare-handoff", Scope: "walk", Step: step, Action: needed, Failed: err != nil})
			if err != nil {
				return operationalFailure("up", err), ""
			}
			if needed {
				if !validContinuationID(id) {
					return unavailable("up"), ""
				}
				if err = lease.Check(ctx); err != nil {
					return operationalFailure("up", err), ""
				}
				return Result{}, id
			}
		}
	}
	return e.completeSetup(ctx, lease, &out), ""
}
func (e *Engine) validateSetupHandoff(ctx context.Context, lease SetupLease, in SetupInput, handoff SetupHandoff, available bool) (Result, bool) {
	if in.Continuation == "" {
		return Result{}, false
	}
	if err := lease.Check(ctx); err != nil {
		return operationalFailure("up", err), true
	}
	if !available {
		return refused("up", "setup continuation is unavailable", "nova-sprint up --as <name>"), true
	}
	err := handoff.ValidateHandoff(ctx, in.Continuation, in)
	e.observeSetup(SetupObservation{Kind: "validate-handoff", Scope: "walk", Failed: err != nil})
	if err != nil {
		return operationalFailure("up", err), true
	}
	if err := lease.Check(ctx); err != nil {
		return operationalFailure("up", err), true
	}
	return Result{}, false
}

func validContinuationID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (e *Engine) completeSetup(ctx context.Context, lease SetupLease, out *strings.Builder) Result {
	completion, ok := e.Setup.(SetupCompletion)
	if !ok {
		return unavailable("up")
	}
	line, err := completion.Completion(ctx)
	if err != nil {
		return operationalFailure("up", err)
	}
	if err = context.Cause(ctx); err != nil {
		return operationalFailure("up", err)
	}
	if err = lease.Check(ctx); err != nil {
		return operationalFailure("up", err)
	}
	if !strings.HasPrefix(line, "UP DONE ") || strings.ContainsAny(line, "\r\n") {
		return unavailable("up")
	}
	fmt.Fprintln(out, line)
	out.WriteString("next: nova-sprint stream add <name> --repo <owner/name>\n")
	return Result{out.String(), 0}
}

func (e *Engine) setupStep(ctx context.Context, step int, in SetupInput, out *strings.Builder, scope string) (Result, bool) {
	check, err := e.Setup.Verify(ctx, step, in)
	e.observeSetup(SetupObservation{Kind: "verify", Scope: scope, Step: step, Verified: check.Verified, Action: check.Action, Failed: err != nil})
	if err != nil {
		r := operationalFailure("up", err)
		return Result{out.String() + r.Output, r.Exit}, true
	}
	if !check.Verified && check.Action {
		err = e.Setup.Act(ctx, step, in)
		e.observeSetup(SetupObservation{Kind: "act", Scope: scope, Step: step, Failed: err != nil})
		if err != nil {
			r := operationalFailure("up", err)
			return Result{out.String() + r.Output, r.Exit}, true
		}
		check, err = e.Setup.Verify(ctx, step, in)
		e.observeSetup(SetupObservation{Kind: "verify", Scope: scope, Step: step, Verified: check.Verified, Action: check.Action, Failed: err != nil})
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
	if check.Refusal != nil {
		r := operationalFailure("up", check.Refusal)
		return Result{out.String() + r.Output, r.Exit}, true
	}
	if check.Next == "" {
		check.Next = "nova-sprint up"
	}
	fmt.Fprintln(out, "next: "+check.Next)
	return Result{out.String(), 1}, true
}

// SetupObservation records only non-secret facts observed by the actual walker.
// A trace harness supplies its own projection of fake outside state and saved
// answers; the walker never synthesizes model state or reads external services.
type SetupObservation struct {
	Kind, Scope              string
	Step                     int
	Verified, Action, Failed bool
}

func (e *Engine) observeSetup(o SetupObservation) {
	if e.SetupObserver != nil {
		e.SetupObserver(o)
	}
}

// ParseSetup validates non-secret setup arguments for the outer wrapper.
func ParseSetup(args []string, actor string) (SetupInput, bool) { return parseSetup(args, actor) }
