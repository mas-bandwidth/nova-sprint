package verbs

import (
	"context"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// SetupInput is up's non-secret choices. A service key is read by the setup
// adapter at a hidden terminal prompt and sent straight to nova-secrets.
// TakeOver with Reason is --take-over --reason: the coordinator's seat moved
// to the caller, with nothing from the old coordinator (takeOver).
type SetupInput struct {
	Actor, Dash, Services, Key, RunOn, Version, Continuation, Reason string
	TakeOver                                                         bool
}

// SetupCheck is one step's probe. Action is true only when the choices make
// the step's next idempotent action possible without another question.
type SetupCheck struct {
	Verified, Action bool
	Line, Next       string
	Refusal          *OperationError
}

// Setup owns each step's effects: Verify inspects the actual state; Act
// persists an answer or performs an idempotent action, and never itself
// proves the step (tla/Setup.tla).
type Setup interface {
	Verify(context.Context, int, SetupInput) (SetupCheck, error)
	Act(context.Context, int, SetupInput) error
}

// SetupLock serializes a whole walk across hosts, on the database.
type SetupLock interface {
	Acquire(context.Context) (SetupLease, error)
}

// SetupLease is a held walk: Monitor cancels the context when the lease is
// lost, Check says whether it is still held.
type SetupLease interface {
	Monitor(context.Context) (context.Context, context.CancelFunc)
	Check(context.Context) error
	Close() error
}

// SetupHandoff correlates the admin bootstrap with the walk under the
// sprint's own credential.
type SetupHandoff interface {
	PrepareHandoff(context.Context, SetupInput) (string, bool, error)
	ValidateHandoff(context.Context, string, SetupInput) error
}

// SetupCompletion is the walk's last line.
type SetupCompletion interface {
	Completion(context.Context) (string, error)
}

// Reader is one read of the store, for the setup steps.
type Reader interface {
	Snapshot(context.Context) (store.Snapshot, error)
}

// SetupObservation is a non-secret fact the walk observed, for the trace
// harness; the walk never synthesizes model state.
type SetupObservation struct {
	Kind, Scope              string
	Step                     int
	Verified, Action, Failed bool
}

// The walk's steps, in order (docs/SETUP.md, LAYER-6 section 5): the fleet
// first (nova-tools green, then the two nova-fleet plays every bench needs,
// runner-packages before netwatch so a bench can run gates as soon as it
// is dealt to), then the sprint's own setup from the store up. A step's
// number is its place in this list; "UP k/10" lines and tla/Setup.tla's
// Steps use the same numbering.
const (
	StepTools = 1 + iota
	StepPackages
	StepNetwatch
	StepStore
	StepCoordinator
	StepRun
	StepDashboard
	StepServices
	StepWorkers
	StepSmoke
	// SetupSteps is how many steps the walk has.
	SetupSteps = StepSmoke
)

// setupInput is up's flags as the walk's input.
func setupInput(in *input) (SetupInput, error) {
	s := SetupInput{Actor: in.actor, Dash: in.str("dash"), Services: in.str("services"), Key: in.str("key"),
		RunOn: in.str("run-on"), Version: in.str("version"), Continuation: in.str("continuation"),
		TakeOver: in.has("take-over"), Reason: in.str("reason")}
	switch {
	case !validName(s.Actor):
		return s, refuse("up names its coordinator", "nova-sprint up --as <you>")
	case s.Continuation != "" && !validContinuationID(s.Continuation):
		return s, misuse("up", "--continuation is the 64-hex id up printed")
	case s.Dash != "" && s.Dash != "tailnet" && !strings.HasPrefix(s.Dash, "public="):
		return s, misuse("up", "--dash is tailnet or public=<machine>[:<port>]")
	case s.TakeOver && s.Reason == "":
		return s, refuse("--take-over needs --reason", takeOverCommand(s.Actor))
	case s.Reason != "" && !s.TakeOver:
		return s, misuse("up", "--reason goes with --take-over")
	}
	for _, v := range []string{s.Services, s.Key, s.RunOn, s.Version, s.Dash, s.Reason} {
		if len(v) > 512 {
			return s, misuse("up", "a value is at most 512 bytes")
		}
	}
	return s, nil
}

// ParseSetup is up's flags for the outer wrapper (cmd/nova-sprint), through
// the one parser.
func ParseSetup(args []string, actor string) (SetupInput, bool) {
	in, f := parse(find("up"), args, actor)
	if f != nil {
		return SetupInput{}, false
	}
	s, err := setupInput(in)
	return s, err == nil
}

func validContinuationID(id string) bool {
	return len(id) == 64 && strings.Trim(id, "0123456789abcdef") == ""
}

// runUp is `up`: the walk, every step re-verified from step 1, under the
// setup lock; the caller on the bus first; hooked in as coordinator once
// the store exists (after StepStore).
func runUp(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	s, err := setupInput(in)
	if err != nil {
		return "", err
	}
	if e.Setup == nil || e.Lock == nil {
		return "", broken("the setup walk is not configured", "nova-sprint check")
	}
	if err := e.handoffValid(ctx, s, "bootstrap"); err != nil {
		return "", err
	}
	if err := e.onBus(ctx, s.Actor); err != nil {
		return "", err
	}
	var out strings.Builder
	if err := e.setupStep(ctx, 1, s, &out, "bootstrap"); err != nil {
		return "", err
	}
	return e.upLeased(ctx, s)
}

// upLeased is the walk under the setup lease; a walk that hands off to the
// sprint's own credential reports its continuation and prints nothing (the
// continuation prints the walk).
func (e *Engine) upLeased(ctx context.Context, s SetupInput) (string, error) {
	lease, err := e.Lock.Acquire(ctx)
	if err != nil {
		return "", err
	}
	monitored, cancel := lease.Monitor(ctx)
	out, id, werr := e.walk(monitored, lease, s)
	cancel()
	err = lease.Close()
	e.observe(SetupObservation{Kind: "lease-closed", Scope: "handoff", Failed: err != nil})
	if werr != nil || err != nil || id == "" {
		return out, firstErr(werr, err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if e.ReportContinuation == nil {
		return "", broken("the setup handoff is not configured", "nova-sprint check")
	}
	err = e.ReportContinuation(ctx, id, s)
	e.observe(SetupObservation{Kind: "handoff-signaled", Scope: "handoff", Failed: err != nil})
	return "", err
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// walk is steps 1 to SetupSteps with the lease checked around each, the
// handoff prepared after the store step, and the coordinator hooked in
// after it.
func (e *Engine) walk(ctx context.Context, lease SetupLease, s SetupInput) (out string, continuation string, err error) {
	e.observe(SetupObservation{Kind: "begin", Scope: "walk"})
	step := 0
	defer func() { e.observeEnd(step, continuation, err) }()
	if err = e.handoffValid(ctx, s, "walk"); err != nil {
		return "", "", err
	}
	handoff, hasHandoff := e.Setup.(SetupHandoff)
	var b strings.Builder
	for step = 1; step <= SetupSteps; step++ {
		if err = e.leased(ctx, lease, step, s); err != nil {
			return "", "", err
		}
		if err = e.setupStep(ctx, step, s, &b, "walk"); err != nil {
			return "", "", err
		}
		if err = e.leased(ctx, lease, 0, s); err != nil {
			return "", "", err
		}
		if step == StepStore && s.Continuation == "" && hasHandoff {
			if id, err := e.prepareHandoff(ctx, lease, s, handoff); err != nil || id != "" {
				return "", id, err
			}
		}
		if step == StepStore {
			if err = e.upHook(ctx, lease, s, &b); err != nil {
				return "", "", err
			}
		}
	}
	return e.completeSetup(ctx, lease, &b)
}

func (e *Engine) observeEnd(step int, continuation string, err error) {
	kind := "complete"
	if continuation != "" {
		kind = "handoff-prepared"
	} else if err != nil {
		kind = "stop"
	}
	e.observe(SetupObservation{Kind: kind, Scope: "walk", Step: step, Failed: err != nil})
}

// leased is the lease still held and the context live; from the
// coordinator step the caller must be the hooked coordinator.
func (e *Engine) leased(ctx context.Context, lease SetupLease, step int, s SetupInput) error {
	if step >= StepCoordinator {
		if _, err := e.hooked(ctx, &input{verb: "up", actor: s.Actor}, roleCoordinator, store.Scope{}); err != nil {
			return err
		}
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	return lease.Check(ctx)
}

// handoffValid checks a continuation's handoff, when up was given one.
func (e *Engine) handoffValid(ctx context.Context, s SetupInput, scope string) error {
	if s.Continuation == "" {
		return nil
	}
	handoff, ok := e.Setup.(SetupHandoff)
	if !ok {
		return refuse("this build has no setup continuation", "nova-sprint up --as "+s.Actor)
	}
	err := handoff.ValidateHandoff(ctx, s.Continuation, s)
	e.observe(SetupObservation{Kind: map[string]string{"bootstrap": "prevalidate-handoff", "walk": "validate-handoff"}[scope], Scope: scope, Failed: err != nil})
	return err
}

func (e *Engine) prepareHandoff(ctx context.Context, lease SetupLease, s SetupInput, handoff SetupHandoff) (string, error) {
	id, needed, err := handoff.PrepareHandoff(ctx, s)
	e.observe(SetupObservation{Kind: "prepare-handoff", Scope: "walk", Step: StepStore, Action: needed, Failed: err != nil})
	if err != nil || !needed {
		return "", err
	}
	if !validContinuationID(id) {
		return "", broken("the setup handoff made a bad continuation id", "nova-sprint up --as "+s.Actor)
	}
	return id, lease.Check(ctx)
}

// upHook hooks the caller in as coordinator once the store exists, unless
// it already is; with --take-over, from the recorded coordinator.
func (e *Engine) upHook(ctx context.Context, lease SetupLease, s SetupInput, out *strings.Builder) error {
	snap, err := e.snapshot(ctx)
	if err != nil {
		return err
	}
	if row := snap.Rows[s.Actor]; row.Session != "" && row.Role == "coordinator" {
		return nil
	}
	hook := e.hookAs
	if s.TakeOver {
		hook = func(ctx context.Context, snap store.Snapshot, name, _ string) (string, error) {
			return e.takeOver(ctx, snap, name, s.Reason)
		}
	}
	text, err := hook(ctx, snap, s.Actor, "coordinator")
	if err != nil {
		return err
	}
	out.WriteString(text[:strings.LastIndex(text, "next: ")])
	return lease.Check(ctx)
}

func (e *Engine) completeSetup(ctx context.Context, lease SetupLease, out *strings.Builder) (string, string, error) {
	completion, ok := e.Setup.(SetupCompletion)
	if !ok {
		return "", "", broken("the setup walk has no completion", "nova-sprint check")
	}
	line, err := completion.Completion(ctx)
	if err == nil {
		err = firstErr(context.Cause(ctx), lease.Check(ctx))
	}
	if err != nil {
		return "", "", err
	}
	if !strings.HasPrefix(line, "UP DONE ") || strings.ContainsAny(line, "\r\n") {
		return "", "", broken("the setup walk's completion is not one UP DONE line", "nova-sprint up again")
	}
	e.upNeeds(ctx, out)
	return done("nova-sprint stream add <name> --repo <org/repo>", out.String()+line, widthAdvice), "", nil
}

// upNeeds names, before UP DONE, what keeps the sprint from working that
// the walk does not set: the commit identity (no worker hooks without it)
// and a bench model for each tier a bench serves (no bench is dealt
// without it), each with its fix, as check's red lines say them. A store
// Migrate upgraded has neither until the coordinator sets them. What it
// cannot check (the store or the policy unreadable) is named the same way,
// so UP DONE never stands on a check that was skipped.
func (e *Engine) upNeeds(ctx context.Context, out *strings.Builder) {
	snap, err := e.snapshot(ctx)
	if err != nil {
		f := classify(err)
		out.WriteString("UP NEEDS identity and bench models unchecked: " + oneLine(f.What) + ": " + oneLine(f.Fix) + "\n")
		return
	}
	_, identity := identityCheck(snap)
	policy := &checkReport{} // its ok lines are check's to print; its reds are needs
	e.checkPolicy(ctx, policy, snap)
	for _, f := range append([]*Fail{identity}, policy.reds...) {
		if f != nil {
			out.WriteString("UP NEEDS " + oneLine(f.What) + ": " + oneLine(f.Fix) + "\n")
		}
	}
}

// widthAdvice is what UP DONE tells a new coordinator before the first
// card: the fleet works at width only when the cards let it.
const widthAdvice = "BEFORE CARDS: keep everybody at width: add each stream's cards with --dry-run first and read its CHAIN line (a NEEDS chain of 4 or more is a WARN: flatten it), then nova-sprint check and read its width, waits and NEEDS chain lines before start; flat chains (CORRECT-USE, \"Keep everybody at width\")"

// setupStep verifies step, acts once when the choices allow it, and
// verifies again. A step that does not verify is a refusal naming its next
// command, printed after the steps that did.
func (e *Engine) setupStep(ctx context.Context, step int, s SetupInput, out *strings.Builder, scope string) error {
	check, err := e.verifyStep(ctx, step, s, scope)
	if err == nil && !check.Verified && check.Action {
		err = e.Setup.Act(ctx, step, s)
		e.observe(SetupObservation{Kind: "act", Scope: scope, Step: step, Failed: err != nil})
		if err == nil {
			check, err = e.verifyStep(ctx, step, s, scope)
		}
	}
	if err != nil {
		f := classify(err)
		f.Detail = out.String()
		return f
	}
	if check.Line != "" {
		out.WriteString(check.Line + "\n")
	}
	if check.Verified {
		return nil
	}
	f := refuse("step "+strconv.Itoa(step)+" is not verified", orWord(check.Next, "nova-sprint up --as "+s.Actor))
	if check.Refusal != nil {
		f = classify(check.Refusal)
	}
	f.Detail = out.String()
	return f
}

func (e *Engine) verifyStep(ctx context.Context, step int, s SetupInput, scope string) (SetupCheck, error) {
	check, err := e.Setup.Verify(ctx, step, s)
	e.observe(SetupObservation{Kind: "verify", Scope: scope, Step: step, Verified: check.Verified, Action: check.Action, Failed: err != nil})
	return check, err
}

func (e *Engine) observe(o SetupObservation) {
	if e.SetupObserver != nil {
		e.SetupObserver(o)
	}
}
