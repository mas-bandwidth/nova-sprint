package cardcontract

import (
	"fmt"
	"strings"
)

// gateRuns are the class-test packages a read only runs some tests of: CI runs them whole on
// every change.
var gateRuns = []string{"./internal/ci", "./internal/docs"}

// THE READ'S GATE (docs/SPEC-CARD-CONTRACT.md, JOB.md). A read judges a diff, and the tests
// it runs are the diff's: the packages it touches, and the packages whose tests read a doc it
// changes (nova-tools#5111). Never ./... and never a class-test package whole: on a 36-thread bench,
// 2026-10-02, sixteen reads each ran `go test ./internal/ci/`, whose tests fork thousands of
// processes and build every command, and the machine spent 85% of its CPU in the kernel. The
// work's own gate ran those tests, and the pull request's CI runs the class-test packages
// (gateRuns) on every change, so a read runs of them only the tests its diff
// reaches: those in a test file it changes, and those in a test file that reads a doc it
// changes.

// GateRun is a package a read runs only some tests of: one CI runs whole on every change.
type GateRun struct {
	Pkg   string   // ./dir
	Tests []string // the Test functions it runs, sorted
}

// Gate is a read's gate: the packages it vets and tests whole, the class-test packages it
// runs some tests of, and whether go.mod or go.sum changed (the whole module is built).
type Gate struct {
	Packages []string // ./dir, sorted
	Runs     []GateRun
	Module   bool
}

// writeReadGate is what a read's JOB.md says it runs: its gate, as commands, in place of the
// card's. Nothing when it has none.
func writeReadGate(b *strings.Builder, s Staged) {
	g := s.Gate
	if g == nil {
		return
	}
	var lines []string
	if len(g.Packages) > 0 {
		pkgs := strings.Join(g.Packages, " ")
		lines = append(lines, "nice -n 19 go vet "+pkgs, "nice -n 19 go test -count=1 -timeout 600s "+pkgs)
	}
	for _, r := range g.Runs {
		lines = append(lines, fmt.Sprintf("nice -n 19 go test -count=1 -timeout 600s -run '^(%s)$' %s", strings.Join(r.Tests, "|"), r.Pkg))
	}
	if g.Module {
		lines = append(lines, "nice -n 19 go build ./...")
	}
	b.WriteString("This is the read's gate, in place of the card's and of any rule that asks for more: the packages the change touches and the tests that read a doc it changes. Never run ./... or a package whole that is not on it: the work's own gate ran those, and the pull request's CI runs " +
		strings.Join(gateRuns, " and ") + " on every change.")
	if len(lines) == 0 {
		b.WriteString(" This change touches nothing a read tests, so the read runs no go command: judge the diff.\n\n")
		return
	}
	b.WriteString("\n\n")
	for _, l := range lines {
		b.WriteString("    " + l + "\n")
	}
	b.WriteString("\n")
}

// gateWords is what a read's JOB.md calls the gate to run: its own, else the card's.
func gateWords(s Staged) string {
	if s.Gate != nil {
		return "the read's gate (below)"
	}
	return "the card's gate"
}
