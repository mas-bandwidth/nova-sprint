// Command nova-worker is the per-bench runner service of nova-sprint v2.
package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/worker"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func main() { os.Exit(run_internal(os.Args[1:], os.Stdout, os.Stderr)) }
func run_internal(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nova-worker", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		row        = fs.String("as", "", "worker row name (e.g. amd-1.ds)")
		host       = fs.String("host", "", "bench hostname (defaults to os.Hostname)")
		root       = fs.String("root", "", "worker root directory for job worktrees")
		stateDir   = fs.String("state-dir", "", "state directory for logs")
		harness    = fs.String("harness", "claude", "default harness adapter (claude, codex, opencode)")
		workClone  = fs.String("work-clone", "", "bench work clone path")
		mirrorDir  = fs.String("mirror", "", "bench bare mirror path")
		originURL  = fs.String("origin", "", "origin git repository URL or path")
		secretsBin = fs.String("secrets-bin", "nova-secrets", "nova-secrets binary for sprint verbs")
		sprintBin  = fs.String("sprint-bin", "nova-sprint", "nova-sprint binary")
		coordHosts = fs.String("coordinator-hosts", "studio,studio.local", "comma-separated coordinator hosts")
		once       = fs.Bool("once", false, "run one reconciliation/step pass and exit")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	sysHost, _ := os.Hostname()
	h := cmp.Or(strings.TrimSpace(*host), sysHost)
	var coords []string
	for _, c := range strings.Split(*coordHosts, ",") {
		if c = strings.TrimSpace(c); c != "" {
			coords = append(coords, c)
		}
	}
	hAdapter, herr := worker.NewHarness(*harness)
	for _, check := range []error{
		worker.CheckHost(h, coords),
		requireNonEmpty_internal(*row, "worker refused: missing --as <row>: pass --as <row>"),
		herr,
		requireNonEmpty_internal(*workClone, "worker refused: missing --work-clone <dir>: pass --work-clone <dir>"),
	} {
		if check != nil {
			fmt.Fprintln(stderr, check.Error())
			return 1
		}
	}
	return execWorker_internal(*row, h, *root, *stateDir, *harness, hAdapter, *workClone, *mirrorDir, *originURL, *secretsBin, *sprintBin, coords, *once, stdout, stderr)
}
func requireNonEmpty_internal(v, msg string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("%s", msg)
	}
	return nil
}
func execWorker_internal(row, host, root, stateDir, harnessName string, h worker.Harness, workClone, mirrorDir, originURL, secretsBin, sprintBin string, coords []string, once bool, stdout, stderr io.Writer) int {
	home, _ := os.UserHomeDir()
	root = cmp.Or(root, filepath.Join(home, ".local", "state", "nova-worker", row))
	stateDir = cmp.Or(stateDir, filepath.Join(home, ".local", "state"))
	ex, timeouts := edge.NewOSExec(), worker.DefaultTimeouts()
	proxy := &worker.KeyProxy{
		DefaultRoute: "openai",
		Routes: map[string]worker.ProxyRoute{
			"openai": {Name: "openai", UpstreamURL: "https://api.openai.com", AllowedHosts: []string{"api.openai.com", "api.deepseek.com", "api.anthropic.com"}, Key: func() string { return worker.ReadProviderKey(ex, secretsBin, "openai") }},
		},
	}
	srv, proxyAddr, err := proxy.ListenLoopback("127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	defer func() { _ = srv.Close() }()
	gitOps := &worker.OSGitOps{WorkCloneDir: workClone, MirrorDir: mirrorDir, OriginURL: originURL, Exec: ex, Timeouts: timeouts}
	forgeClient := forge.New(forge.Config{Identity: forge.Identity{Name: row, Token: os.Getenv("GH_TOKEN"), ConfigDir: os.Getenv("GH_CONFIG_DIR")}, Exec: ex, Timeout: timeouts.GH})
	r, err := worker.NewRunner(worker.Runner{
		Host: host, CoordinatorHosts: coords, RootDir: root, StateDir: stateDir,
		Sprint: &worker.CLISprintClient{Exec: ex, Timeout: timeouts.Stage, SecretsBin: secretsBin, SprintBin: sprintBin},
		Git:    gitOps, Forge: &worker.ForgeAdapter{Client: forgeClient},
		Wall:  &worker.OSReserveWall{UseSandbox: true, Exec: ex, Config: worker.WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", ReserveWait: timeouts.ReserveWait, NetDeny: true}},
		Procs: worker.OSProcessTable{}, Proxy: proxy, Exec: ex, ProxyAddr: proxyAddr, DefaultHarness: h, Timeouts: timeouts,
	})
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if once {
		if err := r.ProbeRow(ctx, row, harnessName, ""); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		if err := r.StepRow(ctx, row); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
		fmt.Fprintf(stdout, "WORKER PASS %s host=%s\nnext: nothing; wait for the bus\n", row, host)
		return 0
	}
	return runWakeLoop_internal(ctx, r, row, harnessName, stderr)
}
func runWakeLoop_internal(ctx context.Context, r *worker.Runner, row, harnessName string, stderr io.Writer) int {
	if err := r.ProbeRow(ctx, row, harnessName, ""); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if err := r.RunWakeLoop(ctx, row); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return 0
}
