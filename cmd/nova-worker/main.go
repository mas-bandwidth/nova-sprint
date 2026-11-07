// Command nova-worker is the per-bench runner service of nova-sprint v2.
package main

import (
	"bufio"
	"cmp"
	"context"
	"flag"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/worker"
	"io"
	"os"
	"os/exec"
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
		secretsBin = fs.String("secrets-bin", "", "nova-secrets binary for sprint verbs")
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
	if err := worker.CheckHost(h, coords); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if strings.TrimSpace(*row) == "" {
		fmt.Fprintln(stderr, "worker refused: missing --as <row>: pass --as <row>")
		return 1
	}
	return execWorker_internal(*row, h, *root, *stateDir, *harness, *workClone, *mirrorDir, *originURL, *secretsBin, *sprintBin, coords, *once, stdout, stderr)
}
func execWorker_internal(row, host, root, stateDir, harnessName, workClone, mirrorDir, originURL, secretsBin, sprintBin string, coords []string, once bool, stdout, stderr io.Writer) int {
	home, _ := os.UserHomeDir()
	root = cmp.Or(root, filepath.Join(home, ".local", "state", "nova-worker", row))
	stateDir = cmp.Or(stateDir, filepath.Join(home, ".local", "state"))
	h, err := worker.NewHarness(harnessName)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	ex, timeouts := edge.NewOSExec(), worker.DefaultTimeouts()
	proxy := &worker.KeyProxy{
		DefaultRoute: "openai",
		Routes: map[string]worker.ProxyRoute{
			"openai": {Name: "openai", UpstreamURL: "https://api.openai.com", AllowedHosts: []string{"api.openai.com", "api.deepseek.com", "api.anthropic.com"}, Key: func() string { return os.Getenv("NOVA_PROVIDER_API_KEY") }},
		},
	}
	srv, proxyAddr, err := proxy.ListenLoopback("127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	defer func() { _ = srv.Close() }()
	var gitOps worker.GitOps
	if workClone != "" {
		gitOps = &worker.OSGitOps{WorkCloneDir: workClone, MirrorDir: mirrorDir, OriginURL: originURL, Exec: ex, Timeouts: timeouts}
	}
	r, err := worker.NewRunner(worker.Runner{
		Host: host, CoordinatorHosts: coords, RootDir: root, StateDir: stateDir,
		Sprint: &worker.CLISprintClient{Exec: ex, Timeout: timeouts.Stage, SecretsBin: secretsBin, SprintBin: sprintBin},
		Git:    gitOps, Forge: &worker.ForgeAdapter{Client: forge.New(forge.Config{Exec: ex, Timeout: timeouts.GH})},
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
	_ = r.StepRow(ctx, row)
	cmd := exec.CommandContext(ctx, "nova-bus", "hook", "--as", row)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	for sc := bufio.NewScanner(pipe); sc.Scan(); {
		go func() { _ = r.StepRow(ctx, row) }()
	}
	_ = cmd.Wait()
	return 0
}
