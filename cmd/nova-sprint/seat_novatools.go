package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/oneline"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/subproc"
)

// The nova-tools under the seat (docs/SPEC-SPRINT.md, "The nova-tools it runs on"):
// before seat check measures anything it looks for each of sprint.NovaToolsBinaries on
// PATH, runs it with --version, and refuses, exit 1 and one line per binary, when one
// is missing or older than NOVA-TOOLS-VERSION. The judging is sprint.NovaToolsRefusal;
// this file is the lookup, which outside.novaTools replaces in a test.

// novaToolsBudget bounds one binary's --version.
const novaToolsBudget = 10 * time.Second

// probeNovaTool is bin as this machine's PATH has it.
func probeNovaTool(bin string) sprint.NovaToolsProbe {
	p := sprint.NovaToolsProbe{Bin: bin}
	path, err := exec.LookPath(bin)
	if err != nil {
		return p
	}
	p.Found = true
	cmd, cancel := subproc.CommandFor(context.Background(), novaToolsBudget, path, "--version")
	defer cancel()
	out, err := cmd.Output()
	p.Output = string(out)
	if err != nil {
		p.Err = err.Error()
	}
	return p
}

// novaToolsRefusals is every refusal of the nova-tools on this machine: the real
// lookup under the machine's outside, the test's under a fake one (nil there checks
// nothing, as a fake outside's other unset reaches measure nothing).
func (a *app) novaToolsRefusals() []string {
	probe := a.outside.novaTools
	if a.outside.serverAddr == nil {
		probe = probeNovaTool
	}
	if probe == nil {
		return nil
	}
	probes := make([]sprint.NovaToolsProbe, 0, len(sprint.NovaToolsBinaries))
	for _, bin := range sprint.NovaToolsBinaries {
		probes = append(probes, probe(bin))
	}
	return sprint.NovaToolsRefusals(probes, sprint.NovaToolsVersion)
}

// refuseNovaTools prints the refusals under verb and is 1 when there are any, 0 when
// the install passes.
func (a *app) refuseNovaTools(verb string, stderr io.Writer) int {
	refusals := a.novaToolsRefusals()
	for _, r := range refusals {
		fmt.Fprintf(stderr, "%s %s REFUSED: %s\n", prog, verb, oneline.Escape(strings.TrimSpace(r)))
	}
	if len(refusals) > 0 {
		return 1
	}
	return 0
}
