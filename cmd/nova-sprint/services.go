package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/dash"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/worker"
	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/run"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

// services are the long-running verbs: run (the chooser), worker (a
// bench's runner) and dash (the board).
type services struct {
	creds  credentials
	store  *lazyStore
	roster *lazyRoster
	policy *policy.Engine
	git    *git.Client
	forge  *forge.Client
	admit  func(context.Context, store.Snapshot, string) (map[string]string, error) // the verbs' admission, re-proved by run at each base move
}

// Run is run.Serve over the real seams (SEAMS 8).
func (s services) Run(ctx context.Context, listen string) error {
	dsn, err := s.creds.sprint()
	if err != nil {
		return err
	}
	ro, err := s.roster.open(ctx, 1)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	host, err := runMachine(ctx, ro)
	if err != nil {
		return err
	}
	busEnv := toolEnvironment(os.Environ(), "nova-bus")
	return run.Serve(ctx, run.Config{Store: s.store, DSN: dsn, Password: s.creds.password, Roster: ro,
		Bus: busEdge(), Hook: run.SprintHook{Bin: busBinary(), Env: busEnv, Reply: self}, Policy: s.policy,
		Git: s.git, Forge: s.forge, Listen: listen, Clock: run.WallClock{}, Log: os.Stderr, Host: host, PID: os.Getpid(), ReposDir: reposDir(), Version: versionString(), Admit: s.admit})
}

// runMachine is the machine run records the chooser lock under: the
// NOVA_SPRINT_MACHINE up hands the service, else the nova-config machine
// whose name is the hostname's first label (never the raw hostname).
func runMachine(ctx context.Context, ro *run.ConfigRoster) (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("run: hostname: %w", err)
	}
	machines, err := ro.MachineNames(ctx)
	if err != nil {
		return "", fmt.Errorf("run: nova-config machine names: %w", err)
	}
	return run.ResolveMachine(os.Getenv(run.MachineEnv), hostname, machines)
}

// Worker is the worker's runner (edge/worker) for one row, its sprint
// verbs this binary's own.
func (s services) Worker(ctx context.Context, w verbs.WorkerArgs) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	w.Host, _ = os.Hostname()
	w.SprintBin, w.Version = self, versionString()
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("worker: home directory (a relative --work-root, --mirror-root, --root or --state-dir is under it): %w", err)
	}
	w.WorkRoot, w.MirrorRoot, w.Root, w.StateDir = underHome(home, w.WorkRoot), underHome(home, w.MirrorRoot), underHome(home, w.Root), underHome(home, w.StateDir)
	return worker.Serve(ctx, w, func(line string) { fmt.Println(line) })
}

// underHome is dir, a relative one taken under home ("" stays "": the
// worker's own default). up's worker service records name their
// directories relative to the bench login's home (setup_workload.go).
func underHome(home, dir string) string {
	if dir == "" || filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(home, dir)
}

// Dash is the board on a public host: a pulled copy of run's (run serves
// its own on the tailnet).
func (s services) Dash(ctx context.Context, listen, pull string) error {
	cache, err := dash.NewPullCache(pull, nil, nil)
	if err != nil {
		return err
	}
	return serveBoard(ctx, listen, cache.Handler(), cache.Run)
}

func serveBoard(ctx context.Context, address string, handler http.Handler, poll func(context.Context) error) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("dashboard: listen: %w", err)
	}
	return serveBoardOn(ctx, listener, handler, poll)
}

// serveBoardOn serves until poll or the server ends, then shuts down.
func serveBoardOn(ctx context.Context, listener net.Listener, handler http.Handler, poll func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 2)
	go func() { done <- poll(ctx) }()
	go func() { done <- server.Serve(listener) }()
	first := <-done
	cancel()
	shutdown, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
		return fmt.Errorf("dashboard: shutdown: %w", err)
	}
	<-done
	if errors.Is(first, context.Canceled) || errors.Is(first, http.ErrServerClosed) {
		return nil
	}
	return first
}
