package worker

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// sdkExec answers the SDK environment's shell with out (or exit 1 when fail)
// and every other command with exit 0, recording each command.
type sdkExec struct {
	mu   sync.Mutex
	out  string
	fail bool
	cmds []edge.Cmd
}

func (e *sdkExec) Run(_ context.Context, c edge.Cmd) (edge.Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c.Argv, c.Env = slices.Clone(c.Argv), slices.Clone(c.Env)
	e.cmds = append(e.cmds, c)
	if len(c.Argv) > 0 && c.Argv[0] == "/bin/sh" {
		if e.fail {
			return edge.Result{ExitCode: 1, Stderr: "env.sh: line 3: syntax error"}, nil
		}
		return edge.Result{Stdout: e.out}, nil
	}
	return edge.Result{}, nil
}

func envValue(env []string, name string) (string, bool) {
	v := ""
	found := false
	for _, kv := range env {
		if k, val, ok := strings.Cut(kv, "="); ok && k == name {
			v, found = val, true // the last wins, as the process sees it
		}
	}
	return v, found
}

// The bench's ~/sdk/env.sh is sourced for every wall command, with the job's
// own environment, and what it exports reaches the job; the wall's own names
// (HOME, TMPDIR, the worker's) keep the wall's values.
func TestSDKEnvReachesTheWall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	out := strings.Join([]string{
		"PATH=/b/sdk/bin:/usr/bin", "DART=dart", "NUGET_PACKAGES=" + home + "/.nuget/packages",
		"HOME=/b", "TMPDIR=/b/tmp", "NOVA_WORKER_ROW=other", "DOTNET_CLI_HOME=/b/sdk/dotnet-home", "PWD=/b", "SHLVL=1", "_=/usr/bin/env",
	}, "\x00") + "\x00"
	fx := &sdkExec{out: out}
	w := &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", SDKEnv: "/b/sdk/env.sh"}}
	if _, err := w.RunTest(context.Background(), Job{Consumer: "c1", Row: "r1", JobDir: dir, WorktreeDir: dir}, "make test", time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(fx.cmds) != 2 {
		t.Fatalf("want the SDK shell then the wall, got %d commands", len(fx.cmds))
	}
	sh, wall := fx.cmds[0], fx.cmds[1]
	if !sh.CleanEnv || !slices.Contains(sh.Argv, "/b/sdk/env.sh") {
		t.Errorf("the SDK shell ran as %q (clean %v), want env.sh sourced in a clean environment", sh.Argv, sh.CleanEnv)
	}
	if v, _ := envValue(sh.Env, "HOME"); v != home {
		t.Errorf("env.sh was sourced with HOME=%q, want the job's home %q", v, home)
	}
	for name, want := range map[string]string{
		"PATH": "/b/sdk/bin:/usr/bin", "DART": "dart", "NUGET_PACKAGES": home + "/.nuget/packages",
		"HOME": home, "TMPDIR": filepath.Join(dir, "tmp"),
	} {
		if v, _ := envValue(wall.Env, name); v != want {
			t.Errorf("the wall's %s is %q, want %q", name, v, want)
		}
	}
	for _, name := range []string{"PWD", "SHLVL", "_"} {
		if v, ok := envValue(wall.Env, name); ok {
			t.Errorf("the wall was given %s=%q from the SDK shell", name, v)
		}
	}
	for _, kv := range []string{"NOVA_WORKER_ROW=other", "DOTNET_CLI_HOME=/b/sdk/dotnet-home"} {
		if slices.Contains(wall.Env, kv) {
			t.Errorf("the SDK shell set the wall's own %s", kv)
		}
	}
	if !slices.Contains(wall.Argv, "DART") || !slices.Contains(wall.Argv, "NUGET_PACKAGES") {
		t.Errorf("the sandbox is not told to pass the SDK's names: %q", wall.Argv)
	}
}

// An env.sh that fails stops the command before the wall runs, naming the
// file: the job never runs without the bench's toolchain environment.
func TestSDKEnvFailureRefusesTheRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fx := &sdkExec{fail: true}
	w := &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", SDKEnv: "/b/sdk/env.sh"}}
	_, err := w.RunHarness(context.Background(), Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}, []string{"/nonexistent/claude"}, nil, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "/b/sdk/env.sh") {
		t.Fatalf("a failing env.sh gave %v, want a refusal naming it", err)
	}
	if len(fx.cmds) != 1 {
		t.Errorf("the wall ran after env.sh failed (%d commands)", len(fx.cmds))
	}
}

// A bench with no SDK environment runs the wall alone.
func TestSDKEnvAbsentRunsTheWallAlone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fx := &sdkExec{}
	w := &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox"}}
	if _, err := w.RunTest(context.Background(), Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}, "make test", time.Minute); err != nil {
		t.Fatal(err)
	}
	if len(fx.cmds) != 1 || fx.cmds[0].Argv[0] == "/bin/sh" {
		t.Errorf("want only the wall, got %d commands", len(fx.cmds))
	}
}
