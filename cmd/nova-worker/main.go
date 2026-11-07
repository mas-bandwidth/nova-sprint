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
	"time"
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
		coordHosts = fs.String("coordinator-hosts", "", "comma-separated machines nova-config gives the coordinator role (required: the worker refuses to run on them)")
		provUp     = fs.String("provider-upstream", "", "API-rate provider base URL the key proxy forwards to (empty: no proxy route)")
		provHosts  = fs.String("provider-hosts", "", "comma-separated hosts the proxy may forward to (required with --provider-upstream)")
		provKey    = fs.String("provider-key", "", "nova-secrets name of the provider key (required with --provider-upstream)")
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
	var hosts []string
	for _, c := range strings.Split(*provHosts, ",") {
		if c = strings.TrimSpace(c); c != "" {
			hosts = append(hosts, c)
		}
	}
	hAdapter, herr := worker.NewHarness(*harness)
	for _, check := range []error{
		worker.CheckHost(h, coords),
		requireNonEmpty_internal(*row, "worker refused: missing --as <row>: pass --as <row>"),
		herr,
		requireNonEmpty_internal(*workClone, "worker refused: missing --work-clone <dir>: pass --work-clone <dir>"),
		providerFlags_internal(*provUp, hosts, *provKey),
	} {
		if check != nil {
			fmt.Fprintln(stderr, check.Error())
			return 1
		}
	}
	route := worker.ProxyRoute{Name: "provider", UpstreamURL: *provUp, AllowedHosts: hosts}
	return execWorker_internal(*row, h, *root, *stateDir, *harness, hAdapter, *workClone, *mirrorDir, *originURL, *secretsBin, *sprintBin, *provKey, route, coords, *once, stdout, stderr)
}
func providerFlags_internal(upstream string, hosts []string, key string) error {
	if upstream == "" && (len(hosts) > 0 || key != "") {
		return fmt.Errorf("worker refused: --provider-hosts and --provider-key need --provider-upstream <url>: pass it, or drop them")
	}
	if upstream != "" && (len(hosts) == 0 || key == "") {
		return fmt.Errorf("worker refused: --provider-upstream needs --provider-hosts <host,host> and --provider-key <nova-secrets name>: pass both")
	}
	return nil
}
func requireNonEmpty_internal(v, msg string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("%s", msg)
	}
	return nil
}
func execWorker_internal(row, host, root, stateDir, harnessName string, h worker.Harness, workClone, mirrorDir, originURL, secretsBin, sprintBin, providerKey string, route worker.ProxyRoute, coords []string, once bool, stdout, stderr io.Writer) int {
	home, _ := os.UserHomeDir()
	root = cmp.Or(root, filepath.Join(home, ".local", "state", "nova-worker", row))
	stateDir = cmp.Or(stateDir, filepath.Join(home, ".local", "state"))
	ex, timeouts := edge.NewOSExec(), worker.DefaultTimeouts()
	proxy := &worker.KeyProxy{DefaultRoute: route.Name, Routes: map[string]worker.ProxyRoute{}}
	if route.UpstreamURL != "" {
		route.Key = func() string { return worker.ReadProviderKey(ex, secretsBin, providerKey) }
		proxy.Routes[route.Name] = route
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
		Git:    gitOps, Forges: map[string]worker.ForgeOps{"github": &worker.ForgeAdapter{Client: forgeClient}},
		Now: time.Now, Sleep: time.Sleep,
		BusHook: worker.OSBusHook{Env: worker.BusEnv(os.Environ()), ClaimWait: timeouts.BusClaim}.Start,
		Wall:    &worker.OSReserveWall{UseSandbox: true, Exec: ex, Config: worker.WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", ReserveWait: timeouts.ReserveWait, NetDeny: true}},
		Procs:   worker.OSProcessTable{}, Proxy: proxy, Exec: ex, ProxyAddr: proxyAddr, DefaultHarness: h, Timeouts: timeouts,
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
		if err := r.RunOnce(ctx, row); err != nil {
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
