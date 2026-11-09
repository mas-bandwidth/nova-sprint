package verbs

import (
	"context"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// SetupInput is up's flags: the coordinator and, for a public board, where.
type SetupInput struct{ Actor, Dash string }

// SetupCheck is one step's probe. Action is true only when the step's next
// idempotent action is possible without another question.
type SetupCheck struct {
	Verified, Action bool
	Line, Next       string
	Refusal          *OperationError
}

// Setup owns each step's effects but step 3's: Verify inspects the actual
// state; Act performs an idempotent action, and never itself proves the
// step (tla/Setup.tla).
type Setup interface {
	Verify(context.Context, int, SetupInput) (SetupCheck, error)
	Act(context.Context, int, SetupInput) error
}

// SetupCompletion is the walk's last line, from what the steps verified.
type SetupCompletion interface {
	Completion(context.Context, SetupInput) (string, error)
}

// Reader is one read of the store, for the setup steps.
type Reader interface {
	Snapshot(context.Context) (store.Snapshot, error)
}

// SetupObservation is a non-secret fact the walk observed, for the trace
// harness; the walk never synthesizes model state.
type SetupObservation struct {
	Kind             string
	Step             int
	Verified, Action bool
	Failed           bool
}

// setupSteps is how many steps the walk has.
const setupSteps = 7

// setupInput is up's flags as the walk's input.
func setupInput(in *input) (SetupInput, error) {
	s := SetupInput{Actor: in.actor, Dash: in.str("dash")}
	switch {
	case !validName(s.Actor):
		return s, refuse("up names its coordinator", "nova-sprint up --as <you>")
	case s.Dash != "" && s.Dash != "tailnet" && !strings.HasPrefix(s.Dash, "public="):
		return s, misuse("up", "--dash is tailnet or public=<machine>[:<port>]")
	case len(s.Dash) > 512:
		return s, misuse("up", "a value is at most 512 bytes")
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

// runUp is `up`: the walk, every step re-verified from step 1, the caller
// on the bus first. A process without the sprint's credential
// (UpStoreOnly) stops silently once step 2 verifies; its caller runs the
// walk again under that credential (LAYER-6 section 5).
func runUp(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	s, err := setupInput(in)
	if err != nil {
		return "", err
	}
	if e.Setup == nil {
		return "", broken("the setup walk is not configured", "nova-sprint check")
	}
	if err := e.onBus(ctx, s.Actor); err != nil {
		return "", err
	}
	e.observe(SetupObservation{Kind: "begin"})
	var b strings.Builder
	step := 1
	for ; step <= setupSteps; step++ {
		if step == 3 && e.UpStoreOnly {
			e.observe(SetupObservation{Kind: "store-only", Step: 2})
			return "", nil
		}
		if step == 3 {
			err = e.coordinatorStep(ctx, s, &b)
		} else {
			err = e.setupStep(ctx, step, s, &b)
		}
		if err != nil {
			e.observe(SetupObservation{Kind: "stop", Step: step, Failed: true})
			return "", err
		}
	}
	out, err := e.completeSetup(ctx, s, &b)
	e.observe(SetupObservation{Kind: map[bool]string{true: "stop", false: "complete"}[err != nil], Step: setupSteps, Failed: err != nil})
	return out, err
}

// coordinatorStep is step 3: the caller hooked into the sprint as its
// coordinator, hooked once when it is not.
func (e *Engine) coordinatorStep(ctx context.Context, s SetupInput, out *strings.Builder) error {
	snap, err := e.snapshot(ctx)
	hooked := err == nil && snap.Rows[s.Actor].Session != "" && snap.Rows[s.Actor].Role == "coordinator"
	e.observe(SetupObservation{Kind: "verify", Step: 3, Verified: hooked, Action: err == nil && !hooked, Failed: err != nil})
	if err == nil && !hooked {
		var text string
		text, err = e.hookAs(ctx, snap, s.Actor, "coordinator")
		e.observe(SetupObservation{Kind: "act", Step: 3, Failed: err != nil})
		if err == nil {
			out.WriteString(text[:strings.LastIndex(text, "next: ")])
			snap, err = e.snapshot(ctx)
			hooked = err == nil && snap.Rows[s.Actor].Session != ""
			e.observe(SetupObservation{Kind: "verify", Step: 3, Verified: hooked, Failed: err != nil})
		}
	}
	if err != nil {
		f := classify(err)
		f.Detail = out.String()
		return f
	}
	out.WriteString("UP 3/7 coordinator " + s.Actor + ": hooked as coordinator: VERIFIED\n")
	return nil
}

func (e *Engine) completeSetup(ctx context.Context, s SetupInput, out *strings.Builder) (string, error) {
	completion, ok := e.Setup.(SetupCompletion)
	if !ok {
		return "", broken("the setup walk has no completion", "nova-sprint check")
	}
	line, err := completion.Completion(ctx, s)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(line, "UP DONE ") || strings.ContainsAny(line, "\r\n") {
		return "", broken("the setup walk's completion is not one UP DONE line", "nova-sprint up again")
	}
	e.upNeeds(ctx, out)
	return done("nova-sprint stream add <name> --repo <org/repo>", out.String()+line, widthAdvice), nil
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
const widthAdvice = "BEFORE CARDS: keep everybody at width: add each stream's cards with --dry-run first and read its CHAIN line (a NEEDS chain of 4 or more is a WARN: flatten it), then nova-sprint check and read its width, waits and NEEDS chain lines before start; flat chains, concrete PATHS, one card per file set (CORRECT-USE, \"Keep everybody at width\")"

// setupStep verifies step, acts once when the step allows it, and
// verifies again. A step that does not verify is a refusal naming its next
// command, printed after the steps that did.
func (e *Engine) setupStep(ctx context.Context, step int, s SetupInput, out *strings.Builder) error {
	check, err := e.verifyStep(ctx, step, s)
	if err == nil && !check.Verified && check.Action {
		err = e.Setup.Act(ctx, step, s)
		e.observe(SetupObservation{Kind: "act", Step: step, Failed: err != nil})
		if err == nil {
			check, err = e.verifyStep(ctx, step, s)
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

func (e *Engine) verifyStep(ctx context.Context, step int, s SetupInput) (SetupCheck, error) {
	check, err := e.Setup.Verify(ctx, step, s)
	e.observe(SetupObservation{Kind: "verify", Step: step, Verified: check.Verified, Action: check.Action, Failed: err != nil})
	return check, err
}

func (e *Engine) observe(o SetupObservation) {
	if e.SetupObserver != nil {
		e.SetupObserver(o)
	}
}
