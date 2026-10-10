package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/oneline"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/subproc"
)

// The machine gate's binding (docs/SPEC-SPRINT.md section 6, the machine gate): every
// finished attempt the work lint passes is run on a Linux bench through the bench-run verb
// (`nova-ci bench run`, card bench-run-verb), at its head, once, before any reader is
// asked. The benches are named by GateBenchEnv (a comma list, the first tried and the
// second the fallback); with none named the gate is off and every attempt is asked as it
// was before, so a sprint with no bench runs as today. rules lists the gate beside the
// lint's checks (cmd/nova-sprint/gaterun.go's rulesWithGate, worklint.go's rulesWithLint).

// GateBenchEnv names the benches the machine gate runs on, in the order they are tried:
// `NOVA_SPRINT_GATE_BENCH=bench-a,bench-b`. The first is the host; the second the fallback
// the bench-run verb tries only when the first does not answer.
const GateBenchEnv = "NOVA_SPRINT_GATE_BENCH"

// GateNovaCIEnv names the nova-ci binary the gate runs the bench-run verb with; the empty
// value is `nova-ci` from PATH.
const GateNovaCIEnv = "NOVA_SPRINT_NOVA_CI"

func init() {
	hosts := gateBenchHosts(os.Getenv(GateBenchEnv))
	if len(hosts) == 0 {
		return
	}
	sprint.DefaultGate = sprint.NewBenchGate(sprint.BenchGateGit{
		Clone: landClone,
		Hosts: hosts,
		Bench: novaCIBenchRun,
		Env:   os.Environ(),
	})
}

// gateBenchHosts is the bench list GateBenchEnv names: trimmed, without blanks or
// duplicates, in order.
func gateBenchHosts(v string) []string {
	var out []string
	for _, h := range strings.Split(v, ",") {
		if h = strings.TrimSpace(h); h != "" && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

// novaCIBenchRun is the BenchRun seam over `nova-ci bench run`: it materializes the
// attempt's tree at head in a scratch directory from the lander's clone, copies that tree
// to the bench (the verb's own make, copy, exec, remove), and returns the command's exit
// status and combined output. A run that never reached the command (no bench answered, the
// copy failed, the verb refused) is an error, which the gate reads as Waiting, never red.
func novaCIBenchRun(ctx context.Context, hosts []string, clone, head string, argv []string) (sprint.BenchResult, error) {
	dir, err := gateTree(ctx, clone, head)
	if err != nil {
		return sprint.BenchResult{}, err
	}
	defer os.RemoveAll(dir)
	args := []string{"bench", "run", "--host", hosts[0]}
	if len(hosts) > 1 {
		args = append(args, "--fallback", hosts[1])
	}
	args = append(args, "--dir", dir, "--")
	args = append(args, argv...)
	bin := os.Getenv(GateNovaCIEnv)
	if bin == "" {
		bin = "nova-ci"
	}
	cmd := subproc.Context(ctx, bin, args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	text := string(out)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return sprint.BenchResult{Host: hosts[0], Code: 0, Out: text}, nil
	case errors.As(err, &exit):
		// the bench-run verb's 2 is a run that never reached the command (usage, no
		// bench answered, the copy failed); any other status is the command's own
		if exit.ExitCode() == 2 {
			return sprint.BenchResult{Host: hosts[0]}, fmt.Errorf("the bench did not run the command (%s): %s", hosts[0], gateLastLine(text))
		}
		return sprint.BenchResult{Host: hosts[0], Code: exit.ExitCode(), Out: text}, nil
	default:
		return sprint.BenchResult{}, fmt.Errorf("nova-ci bench run: %w", err)
	}
}

// gateTree materializes head of clone in a fresh scratch directory, and returns it: a git
// worktree, so the clone's own checkout is not touched and the tree is exactly the
// attempt's commit. The caller removes the directory.
func gateTree(ctx context.Context, clone, head string) (string, error) {
	base, err := os.MkdirTemp("", "nova-gate-")
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "tree")
	if err := os.Remove(base); err != nil {
		return "", err
	}
	cmd := subproc.Context(ctx, "git", "-C", clone, "worktree", "add", "--detach", dir, head)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(base)
		return "", fmt.Errorf("gate worktree at %s: %s", head, gateLastLine(string(out)))
	}
	return dir, nil
}

// gateLastLine is the last non-empty line of s, for a one-line refusal.
func gateLastLine(s string) string {
	last := ""
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			last = l
		}
	}
	return oneline.Escape(last)
}
