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
	"github.com/mas-bandwidth/nova-sprint/edge"
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
	host, _ := os.Hostname()
	busEnv := toolEnvironment(os.Environ(), "nova-bus")
	return run.Serve(ctx, run.Config{Store: s.store, DSN: dsn, Password: s.creds.password, Roster: ro,
		Bus: busEdge(), Hook: run.SprintHook{Bin: busBinary(), Env: busEnv, Reply: self}, Policy: s.policy,
		Git: s.git, Forge: s.forge, Listen: listen, Clock: run.WallClock{}, Log: os.Stderr, Host: host, PID: os.Getpid(), ReposDir: reposDir()})
}

// Worker is nova-worker's runner (edge/worker) for one row, its sprint
// verbs this binary's own.
func (s services) Worker(ctx context.Context, w verbs.WorkerArgs) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	h, err := worker.NewHarness(cmp.Or(w.Harness, "claude"))
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	w.Host = cmp.Or(strings.TrimSpace(w.Host), host)
	coords := []string{"studio", "studio.local"}
	if err := worker.CheckHost(w.Host, coords); err != nil {
		return err
	}
	ex, timeouts := edge.NewOSExec(), worker.DefaultTimeouts()
	proxy := keyProxy(ex)
	srv, proxyAddr, err := proxy.ListenLoopback("127.0.0.1:0")
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()
	home, _ := os.UserHomeDir()
	fg := forge.New(forge.Config{Identity: forge.Identity{Name: w.Row, Token: os.Getenv("GH_TOKEN"), ConfigDir: os.Getenv("GH_CONFIG_DIR")}, Exec: ex, Timeout: timeouts.GH})
	r, err := worker.NewRunner(worker.Runner{Host: w.Host, CoordinatorHosts: coords,
		RootDir:  cmp.Or(w.Root, filepath.Join(home, ".local", "state", "nova-worker", w.Row)),
		StateDir: cmp.Or(w.StateDir, filepath.Join(home, ".local", "state")),
		Sprint:   &worker.CLISprintClient{Exec: ex, Timeout: timeouts.Stage, SecretsBin: "nova-secrets", SprintBin: self},
		Git:      &worker.OSGitOps{WorkCloneDir: w.WorkClone, MirrorDir: w.Mirror, OriginURL: w.Origin, Exec: ex, Timeouts: timeouts},
		Forge:    &worker.ForgeAdapter{Client: fg},
		Wall:     &worker.OSReserveWall{UseSandbox: true, Exec: ex, Config: worker.WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", ReserveWait: timeouts.ReserveWait, NetDeny: true}},
		Procs:    worker.OSProcessTable{}, Proxy: proxy, Exec: ex, ProxyAddr: proxyAddr, DefaultHarness: h, Timeouts: timeouts})
	if err != nil {
		return err
	}
	if err := r.ProbeRow(ctx, w.Row, cmp.Or(w.Harness, "claude"), ""); err != nil {
		return err
	}
	if w.Once {
		return r.StepRow(ctx, w.Row)
	}
	return r.RunWakeLoop(ctx, w.Row)
}

// keyProxy holds the provider key on loopback; the harness never sees it.
func keyProxy(ex edge.Exec) *worker.KeyProxy {
	return &worker.KeyProxy{DefaultRoute: "openai", Routes: map[string]worker.ProxyRoute{
		"openai": {Name: "openai", UpstreamURL: "https://api.openai.com", AllowedHosts: []string{"api.openai.com", "api.deepseek.com", "api.anthropic.com"},
			Key: func() string { return worker.ReadProviderKey(ex, "nova-secrets", "openai") }},
	}}
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
