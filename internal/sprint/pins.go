package sprint

import (
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
)

// The pin checks of the machine lint (docs/SPEC-SPRINT.md section 6, the work lint): a
// reader found 43 tests on epoch 15's log that did not pin the change. The machine decides
// both halves of "pins": the TEST line's test must be red (fail, or not exist) at the
// attempt's merge-base with the base tip, and it must be red again when the change's
// non-test hunks are reverted with the test files kept. Green in either run is a finding.

// pinFindings is the two pin checks of one attempt the view v reads, whose brief names the
// test tl: gate-pin-absent when the test runs green at the merge-base (a test that is not
// there pins by absence, "no tests to run"), and gate-pin-broken when it runs green at the
// reverted head. A view with no test-run seam, a brief with no test, or a machine that
// could not run (an error, a git or go that never started) makes no finding: what the
// machine cannot prove is not a refusal.
func pinFindings(v WorkView, tl cardhdr.TestLine) []LintFinding {
	if v.GateRun == nil || tl.Package == "" || tl.Name == "" {
		return nil
	}
	argv := pinTestArgv(tl)
	var out []LintFinding
	if v.MergeBase != "" {
		if res, err := v.GateRun(v.MergeBase, argv); err == nil && res.Code == 0 && !noTestsRan(res.Out) {
			out = append(out, LintFinding{Token: LintPinAbsent,
				What: "the test " + tl.Name + " passes at the merge-base " + shortSHA(v.MergeBase) + ": it does not pin the change (red first is not kept)"})
		}
	}
	if v.Reverted != nil {
		if ref, err := v.Reverted(); err == nil && ref != "" {
			if res, err := v.GateRun(ref, argv); err == nil && res.Code == 0 && !noTestsRan(res.Out) {
				out = append(out, LintFinding{Token: LintPinBroken,
					What: "the test " + tl.Name + " passes with the change's non-test hunks reverted: it does not fail when the change is removed"})
			}
		}
	}
	return out
}

// pinTestArgv is the command that runs the TEST line's test alone: go test's own timeout,
// the line's tags, and -run of its name, in the package it names.
func pinTestArgv(tl cardhdr.TestLine) []string {
	argv := []string{"go", "test", "-count=1", "-timeout", "600s"}
	if tl.Tags != "" {
		argv = append(argv, "-tags", tl.Tags)
	}
	return append(argv, "-run", "^"+tl.Name+"$", tl.Package)
}

// noTestsRan says go test built the package but ran no test of the name: the test is not
// there at that revision, so it pins by absence, never a green run.
func noTestsRan(out string) bool { return strings.Contains(out, "no tests to run") }
