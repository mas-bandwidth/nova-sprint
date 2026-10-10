package sprint

import (
	"context"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// The machine lint: the checks the machine makes of a finished attempt that the work lint
// cannot read from the commit alone, run beside the work lint's checks (worklint.go,
// WorkLint) before any reader is asked (docs/SPEC-SPRINT.md section 6, the work lint).
// Measured on the sprint log of epoch 15, readers found 43 tests that did not pin the
// change (green before it, or unable to fail) and 44 changes whose new code nothing
// called. Both are decidable by the machine. Each check is a token with its remedy beside
// the work-lint tokens (WorkLintRules):
//
//   - gate-pin-absent: the TEST line's test runs green at the merge-base with the base
//     tip, so it passes without the change (red first is not kept).
//   - gate-pin-broken: the test still runs green with the change's non-test hunks
//     reverted, so it does not fail when the change is removed. Its test files are kept.
//   - gate-reach-unreached: an exported function or method the change adds has no
//     reference from a non-test file in the package it is added to (go/parser); a verb is
//     reached when the command's verb table names it.
const (
	LintPinAbsent = "gate-pin-absent"
	LintPinBroken = "gate-pin-broken"
	LintUnreached = "gate-reach-unreached"
)

// GateTestRun runs a command at ref (a commit-ish of the clone) and returns the machine's
// result; an error says the machine could not run it (no bench answered, a git or go that
// failed to start), which is no finding. It is the pin check's seam: the bench-run verb in
// production, a fake in the tests. WorkLintGit.Gate is one; WorkView.GateRun is one bound
// to the lint's own context and its view.
type GateTestRun func(ctx context.Context, ref string, argv []string) (BenchResult, error)

// GateLintFindings is the machine lint's findings for one finished attempt the view v
// reads: the pin checks (pins.go) and the reach check (reach.go). The pin checks run only
// when the brief names a TEST line and the view carries a test-run seam (v.GateRun); the
// reach check reads the head's files alone, so it runs whatever the machine. nil passes.
func GateLintFindings(in WorkLintInput, v WorkView) []LintFinding {
	var out []LintFinding
	if tl, ok := gateTestLine(in.Brief); ok {
		out = append(out, pinFindings(v, tl)...)
	}
	out = append(out, reachFindings(v)...)
	return out
}

// gateTestLine is the brief's TEST line, the zero TestLine when the brief names none or
// one cardhdr.ParseTest refuses (a card with no test is the brief lint's to refuse).
func gateTestLine(brief string) (cardhdr.TestLine, bool) {
	raw, ok := swarm.CardHeaderValue([]byte(brief), "TEST")
	if !ok {
		return cardhdr.TestLine{}, false
	}
	tl, why := cardhdr.ParseTest(raw)
	if why != "" {
		return cardhdr.TestLine{}, false
	}
	return tl, true
}
