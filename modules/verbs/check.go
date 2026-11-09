package verbs

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// checkReport is check's lines, one per thing checked: CHECK <what> when it
// holds, CHECK WARN <what>: <fix> when it is worth a look, CHECK RED <what>:
// <fix> when it does not hold. Any red line makes the exit non-zero.
type checkReport struct {
	lines []string
	reds  []*Fail
}

func (r *checkReport) ok(format string, a ...any) {
	r.lines = append(r.lines, "CHECK "+oneLine(fmt.Sprintf(format, a...)))
}

func (r *checkReport) warn(what, fix string) {
	r.lines = append(r.lines, "CHECK WARN "+oneLine(what)+": "+oneLine(fix))
}

// red is a line that does not hold: err as every verb's refusal (classify),
// its fix beside it.
func (r *checkReport) red(err error) {
	f := classify(err)
	r.reds = append(r.reds, f)
	r.lines = append(r.lines, "CHECK RED "+oneLine(f.What)+": "+oneLine(f.Fix))
}

// result is the report as the verb's output: done when nothing is red;
// else every line, then the one refusal line naming the first red's fix,
// exit 2 when any red could not be read, else 1.
func (r *checkReport) result(next string) (string, error) {
	if len(r.reds) == 0 {
		return done(next, r.lines...), nil
	}
	exit := exitRefused
	for _, f := range r.reds {
		exit = max(exit, f.Exit)
	}
	what := r.reds[0].What
	if n := len(r.reds); n > 1 {
		what = fmt.Sprintf("%d lines are red; the first: %s", n, what)
	}
	return "", &Fail{What: what, Fix: r.reds[0].Fix, Exit: exit, Detail: strings.Join(r.lines, "\n") + "\n"}
}

// runCheck is `check`, the one doctor, client side (it reads the store and
// calls the edges; no daemon): the plumbing (the fleet, nova-tools against
// the pin, the store at this build's schema, the bus; --integrity rebuilds
// the history) and then the sprint's health (each stream's repository and
// base, the open judgements, the width and why nothing deals, each
// landing's wait). Every line is printed; a red one makes the exit non-zero.
func runCheck(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	if e.Doctor == nil || e.Bus == nil {
		return "", broken("the doctor is not configured", "set NOVA_PG_DSN and NOVA_BUS_REDIS, then check again")
	}
	r := &checkReport{}
	if line := e.unseen(ctx, ""); line != "" { // first: the judgements nobody was woken for
		r.red(refuse(line, unseenFix))
	}
	if err := e.Doctor.Fleet(ctx); err != nil {
		r.red(err)
	} else {
		r.ok("fleet green")
	}
	e.checkTools(ctx, r)
	current := e.checkSchema(ctx, r)
	who, err := e.Bus.Who(ctx)
	if err != nil {
		who = nil
		r.red(RefusalOr(err, broken("nova-bus who failed ("+errText(err)+")", "check NOVA_BUS_REDIS, then nova-bus who")))
	} else {
		r.ok("bus answering (%d present)", len(who))
	}
	if current && in.has("integrity") {
		if err := e.Doctor.History(ctx); err != nil {
			r.red(err)
		} else {
			r.ok("history rebuilds the tables")
		}
	}
	if current {
		e.checkSprint(ctx, r, who)
	}
	return r.result("nova-sprint next --as " + orWord(in.actor, "<you>"))
}

// checkSchema is the store at this build's schema; false skips the
// sprint's lines (there is no sprint to read).
func (e *Engine) checkSchema(ctx context.Context, r *checkReport) bool {
	current, err := e.Doctor.Schema(ctx)
	switch {
	case err != nil:
		r.red(RefusalOr(err, broken("the store did not answer ("+errText(err)+")", "check NOVA_PG_DSN and NOVA_SPRINT_PG_PASSWORD")))
	case !current:
		r.red(refuse("the store is not at this build's schema", "nova-sprint up --as <coordinator>"))
	default:
		r.ok("store at schema")
	}
	return err == nil && current
}

// checkTools is nova-tools' release against this build's pin: equal is
// fine, below is red, above is a warning (this build was checked against
// the pin, not the newer release).
func (e *Engine) checkTools(ctx context.Context, r *checkReport) {
	line, err := e.Doctor.ToolsVersion(ctx)
	v, ok := parseToolsRelease(line)
	switch {
	case err != nil:
		r.red(RefusalOr(err, refuse("nova-tools' version is unknown ("+errText(err)+")", toolsFix(e.ToolPin))))
		return
	case !ok:
		r.red(refuse(fmt.Sprintf("nova-tools' version is unknown (%q is not a release vMAJOR.MINOR.PATCH)", Redact(line)), toolsFix(e.ToolPin)))
		return
	}
	have, err := releaseVersion(v)
	pin, perr := releaseVersion(e.ToolPin)
	if err = cmp.Or(err, perr); err != nil {
		r.red(RefusalOr(err, refuse("nova-tools "+v+" cannot be compared with the pin "+e.ToolPin+" ("+errText(err)+")", toolsFix(e.ToolPin))))
		return
	}
	switch slices.Compare(have[:], pin[:]) {
	case -1:
		r.red(refuse("nova-tools "+v+" is below the pin "+e.ToolPin, toolsFix(e.ToolPin)))
	case 1:
		r.warn("nova-tools "+v+" is above the pin "+e.ToolPin+" (this build was checked against "+e.ToolPin+")",
			"install the nova-sprint release that pins "+v+", or "+toolsFix(e.ToolPin))
	default:
		r.ok("nova-tools %s (pin %s)", v, e.ToolPin)
	}
}

// present is whether name is on the bus (who is nil when the bus did not
// answer: then nothing is said about it).
func present(who map[string]bus.Presence, name string) (on, known bool) {
	if who == nil {
		return false, false
	}
	_, on = who[name]
	return on, true
}
