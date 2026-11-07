package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/store"
	config "github.com/mas-bandwidth/nova-tools/v2/config/lib"
)

// e2e is one end-to-end sprint: the binary for every verb, run.Serve in
// this process (its forge is fakeForge: the binary's run takes only gh),
// on a throwaway PostgreSQL and Redis, with a local bare repository as the
// stream's origin.
type e2e struct {
	t                 *testing.T
	dir, origin, boot string
	adminDSN, busAddr string
	sprintDSN         string
	tr                *transcript
	c                 *cli
	busEnv            []string
	st                *store.PG
	fg                *fakeForge
	board, work       string
	schema            []byte // nova-config's schema.sql
}

const sprintPassword = "e2e-sprint-password" // trust auth: the server never checks it

func TestFunctionalEndToEnd(t *testing.T) {
	for _, tool := range []string{"initdb", "pg_ctl", "pg_dump", "redis-server", "nova-bus", "git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " required")
		}
	}
	x := &e2e{t: t, tr: &transcript{}, fg: &fakeForge{prs: map[string]forge.PRInfo{}}}
	defer x.tr.save(t)
	x.setUp()
	x.up()
	stopRun := x.startRun()
	defer stopRun()
	x.coordinatorHooks()
	x.streamAndCard()
	id, gen, epoch := x.workerTakes()
	head := x.workerFinishes(id, gen, epoch)
	x.readerReads(head)
	x.lands(head)
	x.boardShowsLanded()
	x.stopStopsTheTick()
	x.tr.note("end: the producer landed through the forge; stop held the tick")
}

// setUp builds the binary and starts PostgreSQL, Redis and the origin.
func (x *e2e) setUp() {
	t := x.t
	x.dir = t.TempDir()
	home := filepath.Join(x.dir, "home")
	ghDir := filepath.Join(x.dir, "bin")
	for _, d := range []string{home, ghDir, filepath.Join(x.dir, "gh"), filepath.Join(x.dir, "inbox")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ghDir, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(x.dir, "nova-sprint")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	x.schema = configSchema(t)
	x.adminDSN, x.busAddr = e2ePG(t), e2eRedis(t)
	x.sprintDSN, _ = setupRoleDSN(x.adminDSN, "nova_sprint")
	x.origin = filepath.Join(x.dir, "origin.git")
	x.fg.origin = x.origin
	x.busEnv = x.environment(home, ghDir)
	for _, kv := range [][2]string{{"HOME", home}, {"GIT_CONFIG_NOSYSTEM", "1"}, {"GIT_AUTHOR_NAME", "e2e"}, {"GIT_AUTHOR_EMAIL", "e2e@sprint.test"},
		{"GIT_COMMITTER_NAME", "e2e"}, {"GIT_COMMITTER_EMAIL", "e2e@sprint.test"}, {"NOVA_BUS_REDIS", x.busAddr}} {
		t.Setenv(kv[0], kv[1])
	}
	x.c = &cli{t: t, bin: bin, env: x.busEnv, tr: x.tr}
	x.tr.note("nova-sprint end to end: the binary %s for the verbs, run.Serve in the test process with a fake forge", versionString())
	x.seedOrigin()
}

// environment is every command's: no ambient NOVA_, PG, GH or HOME value,
// then the test's own.
func (x *e2e) environment(home, ghDir string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "NOVA_") || strings.HasPrefix(k, "PG") || strings.HasPrefix(k, "GH_") || strings.HasPrefix(k, "GITHUB_") ||
			strings.HasPrefix(k, "XDG_") || strings.HasPrefix(k, "GIT_") || k == "HOME" || k == "PATH" || k == "CODEX_THREAD_ID" {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "HOME="+home, "PATH="+ghDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"XDG_STATE_HOME="+filepath.Join(x.dir, "xdg-state"), "XDG_CONFIG_HOME="+filepath.Join(x.dir, "xdg-config"),
		"NOVA_BUS_REDIS="+x.busAddr, "NOVA_PG_DSN="+x.adminDSN, "NOVA_SPRINT_PG_PASSWORD="+sprintPassword,
		"NOVA_SPRINT_BACKUP_DIR="+filepath.Join(x.dir, "backups"), "GH_CONFIG_DIR="+filepath.Join(x.dir, "gh"),
		"NOVA_SPRINT_FORGE_LOGIN=sprint-bot", "FAKE_GH_ORIGIN="+x.origin, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@sprint.test", "GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@sprint.test")
}

// seedOrigin is the stream's origin: a bare repository with main at one
// commit.
func (x *e2e) seedOrigin() {
	t, env := x.t, x.busEnv
	x.tr.note("the stream's origin: a local bare repository, main at one commit")
	sh(t, x.tr, env, "git", "init", "-q", "--bare", "-b", "main", x.origin)
	seed := filepath.Join(x.dir, "seed")
	sh(t, x.tr, env, "git", "clone", "-q", x.origin, seed)
	if err := os.WriteFile(filepath.Join(seed, "README"), []byte("e2e\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sh(t, x.tr, env, "git", "-C", seed, "add", "README")
	sh(t, x.tr, env, "git", "-C", seed, "commit", "-q", "-m", "base")
	sh(t, x.tr, env, "git", "-C", seed, "push", "-q", "origin", "HEAD:main")
}

// up is what `up` sets up on a real fleet, done here without the fleet
// services it checks (nova-fleet, model probes, launchd units): the sprint
// role and store through the binary's own setup steps, nova-config's
// schema with a bench and a friend, and the default policy stored.
func (x *e2e) up() {
	t := x.t
	x.tr.note("up, on a fake fleet: the binary's own role and store steps; nova-config seeded; the default policy stored")
	if code, out := x.c.run(false, []string{"NOVA_PG_ADMIN_PASSWORD=e2e-admin"}, "--internal-setup-role-fresh"); code != 0 {
		t.Fatalf("sprint role: exit %d\n%s", code, out)
	}
	x.c.must([]string{"--internal-store-setup"}, `"ready":true`)
	x.seedConfig()
	ctx := context.Background()
	st, err := store.Open(ctx, x.sprintDSN, sprintPassword)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	x.st = st
	r, err := policy.Up(ctx, st, policy.Caller{Actor: "carol", Coordinator: true, OpID: "e2e-policy-up", At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	x.tr.note("policy.Up: active version v%d", r.Version)
}

// seedConfig applies nova-config's schema (nova-tools, the version this
// module requires) and adds a bench (bench1) and a friend (kim).
func (x *e2e) seedConfig() {
	t := x.t
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, x.adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	for _, q := range []string{string(x.schema),
		`GRANT USAGE ON SCHEMA config TO nova_sprint`, `GRANT SELECT ON ALL TABLES IN SCHEMA config TO nova_sprint`,
		`INSERT INTO config.machines (name, "user", roles, cores, os, arch) VALUES ('bench1', 'bench', 'bench', 1, 'linux', 'amd64')`,
		`INSERT INTO config.friends (name, harness, model, width, tiers, enabled) VALUES ('kim', 'claude', 'm-kim', 1, 'flash,frontier,heavy,pro', true)`} {
		if _, err := conn.Exec(ctx, q, pgx.QueryExecModeSimpleProtocol); err != nil {
			t.Fatalf("nova-config seed: %v", err)
		}
	}
	x.tr.note("nova-config: schema applied; machine bench1 (bench, 1 core); friend kim (claude, model m-kim, width 1)")
}

// startRun is run.Serve over the real store, nova-config, bus, policy
// engine and git, the fake forge, and the board on 127.0.0.1:0.
func (x *e2e) startRun() func() {
	t := x.t
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := config.Open(ctx, x.sprintDSN, sprintPassword)
	if err != nil {
		t.Fatal(err)
	}
	ro, err := run.NewConfigRoster(conn, 1)
	if err != nil {
		t.Fatal(err)
	}
	busBin, _ := exec.LookPath("nova-bus")
	ready := make(chan string, 1)
	logf, err := os.Create(filepath.Join(x.dir, "run.log"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := run.Config{Store: x.st, DSN: x.sprintDSN, Password: sprintPassword, Roster: ro,
		Bus: bus.New(bus.Config{Bin: busBin, Env: x.busEnv}), Hook: run.SprintHook{Bin: busBin, Env: x.busEnv, Reply: x.c.bin},
		Policy: policy.NewEngine(policy.RealClock{}), Git: git.New(git.Config{}), Forge: x.fg, Listen: "127.0.0.1:0", Ready: ready,
		Clock: run.WallClock{}, Log: logf, Host: "e2e", PID: os.Getpid(), ReposDir: filepath.Join(x.dir, "run-repos")}
	done := make(chan error, 1)
	go func() { done <- run.Serve(ctx, cfg) }()
	select {
	case x.board = <-ready:
	case err := <-done:
		t.Fatalf("run.Serve ended at start: %v", err)
	case <-time.After(60 * time.Second):
		t.Fatal("run's board never listened")
	}
	x.tr.note("run.Serve started in the test process; the board at http://%s", x.board)
	return func() {
		cancel()
		select {
		case err := <-done:
			x.tr.note("run.Serve ended: %v", err)
		case <-time.After(30 * time.Second):
			x.tr.note("run.Serve did not end within 30s of its context")
		}
		ro.Close()
		_ = logf.Close()
		x.runLogTail()
	}
}

// inbox is name's bus inbox file.
func (x *e2e) inbox(name string) string { return filepath.Join(x.dir, "inbox", name) }

// eventually runs check every half second until it holds or limit passes.
func eventually(t *testing.T, limit time.Duration, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen within %s", what, limit)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// snapshot is the store as run sees it.
func (x *e2e) snapshot() store.Snapshot {
	s, err := x.st.Snapshot(context.Background())
	if err != nil {
		x.t.Fatal(err)
	}
	return s
}

// boardJSON is /api/sprint, or "".
func (x *e2e) boardJSON() string {
	resp, err := http.Get("http://" + x.board + "/api/sprint")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// boardField is a top-level field of the board's envelope, under "data" if
// the page wraps it so.
func boardField(raw, name string) (any, bool) {
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil, false
	}
	if d, ok := m["data"].(map[string]any); ok {
		m = d
	}
	v, ok := m[name]
	return v, ok
}

func fmtAny(v any) string { return fmt.Sprint(v) }
