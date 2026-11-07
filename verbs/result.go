package verbs

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Result is a verb's whole output and its exit status (LAYER-6 section 3):
// 0 done, its last line `next: ...`; 1 refused, one line naming the command
// that makes the prerequisite true; 2 could not run (bad usage, or the
// store, the bus or an edge failed), one line in the same form.
type Result struct {
	Output string
	Exit   int
}

// The three exit kinds.
const (
	exitDone    = 0
	exitRefused = 1
	exitFailed  = 2
)

// Fail is every verb's error: what is not true, the command that makes it
// true, and the exit kind. It prints as the one refusal line, `SPRINT
// REFUSED <verb>: <what>: <fix>`, whatever the kind.
type Fail struct {
	What, Fix string
	Detail    string // printed before the refusal line: what a check found
	Exit      int
	cause     error // the sentinel it stands for (ErrNotSetUp), for errors.Is
}

func (f *Fail) Error() string { return f.What + ": " + f.Fix }
func (f *Fail) Unwrap() error { return f.cause }

// ErrNotSetUp is the store with no sprint credential (up mints
// NOVA_SPRINT_PG_PASSWORD) or without its schema: the fix is up, never a
// hand-run secrets command.
var ErrNotSetUp = errors.New("the sprint is not set up")

// notSetUp is ErrNotSetUp's refusal (exit 1), naming up.
func notSetUp(actor string) *Fail {
	return &Fail{What: "the sprint is not set up", Fix: "nova-sprint up --as " + orWord(actor, "<you>"), Exit: exitRefused, cause: ErrNotSetUp}
}

// refuse is a prerequisite that does not hold (exit 1).
func refuse(what, fix string) *Fail { return &Fail{What: what, Fix: fix, Exit: exitRefused} }

// broken is a verb that could not run: an I/O failure, or a bug (exit 2).
func broken(what, fix string) *Fail { return &Fail{What: what, Fix: fix, Exit: exitFailed} }

// misuse is bad usage of verb (exit 2): its fix is the verb's help.
func misuse(verb, what string) *Fail { return broken(what, "nova-sprint help "+verb) }

// OperationError is the setup walk's refusal (the up machinery's adapters
// return it): its Exit 2 is a failure, anything else a refusal.
type OperationError struct {
	Problem, Fix string
	Exit         int
}

func (e *OperationError) Error() string { return e.Problem + ": " + e.Fix }

// classify is every error as a Fail, so no error leaves a verb unclassified:
// a Fail as it is; the setup walk's, the brief lint's, policy's and the card
// machine's refusals in their own words; the store's invalid op as a bug;
// anything else an I/O failure whose fix is the one doctor.
func classify(err error) *Fail {
	var f *Fail
	var op *OperationError
	var lint *BriefLintError
	var pr *policy.Refusal
	switch {
	case errors.As(err, &f):
		return f
	case errors.Is(err, ErrNotSetUp):
		return notSetUp("")
	case errors.As(err, &op):
		if op.Exit == exitFailed {
			return broken(op.Problem, op.Fix)
		}
		return refuse(op.Problem, op.Fix)
	case errors.As(err, &lint):
		return refuse(lint.Error(), "fix the brief and run nova-sprint add again")
	case errors.As(err, &pr):
		return refuse(pr.What, pr.Fix)
	case errors.Is(err, store.ErrInvalid):
		return broken("nova-sprint built an invalid op ("+clip(err.Error())+")", "take it to the project's own tracker with this line")
	}
	return broken(clip(err.Error()), "run nova-sprint check, then the verb again")
}

// moved is the card machine's or the steps' refusal of a move, with the
// command that shows the record it was refused on.
func moved(r *card.Refusal, subject string) *Fail {
	return refuse(r.Reason, "nova-sprint show "+subject)
}

// line is the one refusal line.
func (f *Fail) line(verb string) string {
	return "SPRINT REFUSED " + oneLine(verb) + ": " + oneLine(f.What) + ": " + oneLine(f.Fix) + "\n"
}

// done is a success: its lines, then the `next:` line.
func done(next string, lines ...string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(strings.TrimRight(l, "\n"))
		b.WriteByte('\n')
	}
	b.WriteString("next: " + next + "\n")
	return b.String()
}

// waitNext is the next line when the next move is someone else's.
const waitNext = "nothing; wait for the bus"

func oneLine(s string) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
}

// clip is a foreign error's text on one line, bounded.
func clip(s string) string {
	s = oneLine(s)
	if len(s) > 240 {
		s = s[:240] + "..."
	}
	return s
}

func fmtCard(id string, gen int) string { return fmt.Sprintf("%s@%d", id, gen) }
