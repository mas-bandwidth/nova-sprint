// Command nova-worker is the per-bench runner service of nova-sprint v2;
// `nova-sprint worker` is the same worker (edge/worker.Serve).
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mas-bandwidth/nova-sprint/edge/worker"
)

func main() { os.Exit(run_internal(os.Args[1:], os.Stdout, os.Stderr)) }

func run_internal(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nova-worker", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var c worker.Config
	fs.StringVar(&c.Row, "as", "", "worker row name (a machine nova-config gives the bench role)")
	fs.StringVar(&c.Host, "host", "", "bench hostname (defaults to os.Hostname)")
	fs.StringVar(&c.Root, "root", "", "worker root directory for job and probe directories")
	fs.StringVar(&c.StateDir, "state-dir", "", "state directory for logs")
	fs.StringVar(&c.Harness, "harness", "claude", "harness adapter (claude, codex, opencode)")
	fs.StringVar(&c.WorkRoot, "work-root", "", "directory of the work clones, one per stream repo, made on first use (default <root>/clones)")
	fs.StringVar(&c.MirrorRoot, "mirror-root", "", "directory of the bare mirrors, <repo>.git as mirror-refresh keeps them (default ~/nova-bench/mirror)")
	fs.StringVar(&c.SprintBin, "sprint-bin", "nova-sprint", "nova-sprint binary (run directly: the worker's environment holds the sprint's credentials)")
	fs.StringVar(&c.Providers, "providers", "", "key proxy routes, <name>=<upstream url>@<KEY NAME>[,...]; each key is read from the environment")
	fs.BoolVar(&c.BeatOnly, "beat-only", false, "serve no jobs: only push the row's heartbeat, for a row hooked another way (a friend's)")
	fs.BoolVar(&c.Once, "once", false, "run one reconciliation/step pass and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2 // the flag set has written err and the usage on stderr
	}
	sysHost, err := os.Hostname()
	if err != nil && strings.TrimSpace(c.Host) == "" {
		fmt.Fprintln(stderr, "worker refused: the hostname cannot be read ("+err.Error()+"): pass --host <bench>")
		return 1
	}
	c.Host = cmp.Or(strings.TrimSpace(c.Host), sysHost)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := worker.Serve(ctx, c, func(s string) { fmt.Fprintln(stdout, s) }); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return 0
}
