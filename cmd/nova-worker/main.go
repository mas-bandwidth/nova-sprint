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
	var coords, provHosts string
	fs.StringVar(&c.Row, "as", "", "worker row name")
	fs.StringVar(&c.Host, "host", "", "bench hostname (defaults to os.Hostname)")
	fs.StringVar(&c.Root, "root", "", "worker root directory for job worktrees")
	fs.StringVar(&c.StateDir, "state-dir", "", "state directory for logs")
	fs.StringVar(&c.Harness, "harness", "claude", "default harness adapter (claude, codex, opencode)")
	fs.StringVar(&c.WorkClone, "work-clone", "", "bench work clone path")
	fs.StringVar(&c.Mirror, "mirror", "", "bench bare mirror path")
	fs.StringVar(&c.Origin, "origin", "", "origin git repository URL or path")
	fs.StringVar(&c.SecretsBin, "secrets-bin", "nova-secrets", "nova-secrets binary for sprint verbs")
	fs.StringVar(&c.SprintBin, "sprint-bin", "nova-sprint", "nova-sprint binary")
	fs.StringVar(&coords, "coordinator-hosts", "", "comma-separated machines nova-config gives the coordinator role (required: the worker refuses to run on them)")
	fs.StringVar(&c.ProviderUpstream, "provider-upstream", "", "API-rate provider base URL the key proxy forwards to (empty: no proxy route)")
	fs.StringVar(&provHosts, "provider-hosts", "", "comma-separated hosts the proxy may forward to (required with --provider-upstream)")
	fs.StringVar(&c.ProviderKey, "provider-key", "", "nova-secrets name of the provider key (required with --provider-upstream)")
	fs.BoolVar(&c.Once, "once", false, "run one reconciliation/step pass and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2 // the flag set has written err and the usage on stderr
	}
	sysHost, _ := os.Hostname()
	c.Host = cmp.Or(strings.TrimSpace(c.Host), sysHost)
	c.CoordinatorHosts, c.ProviderHosts = worker.SplitList(coords), worker.SplitList(provHosts)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := worker.Serve(ctx, c, func(s string) { fmt.Fprintln(stdout, s) }); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return 0
}
