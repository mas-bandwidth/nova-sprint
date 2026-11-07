// Command nova-worker is the per-bench runner service of nova-sprint v2
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
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
		harness    = fs.String("harness", "script", "default harness adapter (claude, codex, opencode, script)")
		workClone  = fs.String("work-clone", "", "bench work clone path")
		mirrorDir  = fs.String("mirror", "", "bench bare mirror path")
		originURL  = fs.String("origin", "", "origin git repository URL or path")
		coordHosts = fs.String("coordinator-hosts", "studio,studio.local", "comma-separated coordinator hosts")
		once       = fs.Bool("once", false, "run one reconciliation/step pass and exit")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	h := strings.TrimSpace(*host)
	if h == "" {
		h, _ = os.Hostname()
	}
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
	return execWorker_internal(*row, h, *root, *stateDir, *harness, *workClone, *mirrorDir, *originURL, coords, *once, stdout, stderr)
}
func execWorker_internal(row, host, root, stateDir, harnessName, workClone, mirrorDir, originURL string, coords []string, once bool, stdout, stderr io.Writer) int {
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".local", "state", "nova-worker", row)
	}
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".local", "state")
	}
	h, err := worker.NewHarness(harnessName)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	cli := &worker.CLISprintClient{
		Run: func(ctx context.Context, argv []string) ([]byte, int, error) {
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
			out, err := cmd.CombinedOutput()
			code := 0
			if cmd.ProcessState != nil {
				code = cmd.ProcessState.ExitCode()
			}
			return out, code, err
		},
	}
	var gitOps worker.GitOps
	if workClone != "" {
		gitOps = &worker.OSGitOps{WorkCloneDir: workClone, MirrorDir: mirrorDir, OriginURL: originURL}
	}
	r, err := worker.NewRunner(worker.Runner{
		Host:             host,
		CoordinatorHosts: coords,
		RootDir:          root,
		StateDir:         stateDir,
		Sprint:           cli,
		Git:              gitOps,
		Wall:             &worker.OSReserveWall{},
		DefaultHarness:   h,
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
	sc := bufio.NewScanner(pipe)
	for sc.Scan() {
		_ = r.StepRow(ctx, row)
	}
	_ = cmd.Wait()
	return 0
}
