package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/dash"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/edge/worker"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/verbs"
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
		Git: s.git, Forge: s.forge, Listen: listen, Clock: run.WallClock{}, Log: os.Stderr, Host: host, PID: os.Getpid(), ReposDir: reposDir()})
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

// Worker is nova-worker's runner (edge/worker) for one row, its sprint
// verbs this binary's own.
func (s services) Worker(ctx context.Context, w verbs.WorkerArgs) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	w.Host = cmp.Or(strings.TrimSpace(w.Host), host)
	w.SprintBin = self
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

// Dash is the board: a pulled copy of run's on a public host (--pull), or
// the store's own on the tailnet.
func (s services) Dash(ctx context.Context, listen, pull string) error {
	if pull != "" {
		cache, err := dash.NewPullCache(pull, nil, nil)
		if err != nil {
			return err
		}
		return serveBoard(ctx, listen, cache.Handler(), cache.Run)
	}
	if _, err := s.creds.sprint(); err != nil {
		return err
	}
	var st store.Store = s.store
	dash.Version = versionString()
	cache, err := dash.NewRecovering(ctx, st)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("dashboard startup: %v; retrying", err)
	}
	return serveBoard(ctx, listen, dash.Handler(cache), func(ctx context.Context) error { cache.Run(ctx, st); return ctx.Err() })
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
