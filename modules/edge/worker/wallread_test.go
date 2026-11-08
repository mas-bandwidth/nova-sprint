package worker

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// --wall-read grants a toolchain's directory read-only to every wall, and
// refuses whatever would let a job read the worker's credentials, state or
// other jobs' trees, each with one line naming the fix.
func TestWallReadGrantsAToolchainAndRefusesTheRest(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	mk := func(rel string) string {
		d := filepath.Join(home, rel)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		return d
	}
	goroot, modcache, root, secrets := mk("sdk/go1.99.0"), mk("go/pkg/mod"), mk("state/nova-worker/r1"), mk("secrets-elsewhere")
	work, conf := mk("state/nova-worker/r1/clones"), mk(".config/tool")
	own := [][2]string{{"--root", root}, {"--work-root", work}, {"secrets store", secrets}, {"--mirror-root", ""}}
	got, err := CheckWallReads([]string{goroot, modcache, goroot}, home, own)
	if want := []string{resolvePath_internal(goroot), resolvePath_internal(modcache)}; err != nil || !slices.Equal(got, want) {
		t.Fatalf("grants %q %v, want %q", got, err, want)
	}
	for _, c := range []struct{ dir, why string }{
		{"sdk/go", "not an absolute path"},
		{filepath.Join(home, "missing"), "not a directory"},
		{home, "home or holds it"},
		{filepath.Dir(home), "home or holds it"},
		{conf, "dot-directory of the home"},
		{root, "overlaps the worker's own --root"},
		{filepath.Join(home, "state"), "overlaps the worker's own --root"},
		{work, "overlaps the worker's own --root"},
		{secrets, "overlaps the worker's own secrets store"},
	} {
		if _, err := CheckWallReads([]string{goroot, c.dir}, home, own); err == nil || !strings.Contains(err.Error(), c.why) || !strings.Contains(err.Error(), "--wall-read") {
			t.Errorf("--wall-read %s: %v, want a refusal saying %q", c.dir, err, c.why)
		}
	}
	if err := (Config{Row: "r1", Harness: "script", WallReads: []string{"sdk/go"}}).Check(); err == nil || !strings.Contains(err.Error(), "not an absolute path") {
		t.Errorf("Check took a relative --wall-read: %v", err)
	}
}

// A done-when command's wall reads each granted directory and writes none
// of them; the Go toolchain's names reach it only when a grant covers each.
func TestWallReadsGrantsAndCarriesGoEnvOnlyWhenGranted(t *testing.T) {
	t.Parallel()
	const goroot, modcache = "/opt/toolchains/go", "/srv/cache/gomod"
	run := func(reads []string) (argv, env []string) {
		t.Helper()
		fx := cr2OKExec_internal()
		dir := t.TempDir()
		w := &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", NetDeny: true, ReadPaths: reads, GoRoot: goroot, GoModCache: modcache}}
		if _, err := w.RunTest(context.Background(), Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}, "go test ./cmd/nova-worker", time.Minute); err != nil {
			t.Fatal(err)
		}
		c := fx.Calls()[0].Cmd
		return c.Argv, c.Env
	}
	pair := func(argv []string, flag, val string) bool {
		for i := 0; i+1 < len(argv); i++ {
			if argv[i] == flag && argv[i+1] == val {
				return true
			}
		}
		return false
	}
	val := func(env []string, name string) string {
		for _, kv := range env {
			if k, v, _ := strings.Cut(kv, "="); k == name {
				return v
			}
		}
		return ""
	}
	argv, env := run([]string{goroot, modcache})
	for _, d := range []string{goroot, modcache} {
		if !pair(argv, "--read", d) || pair(argv, "--write", d) {
			t.Errorf("%s: want --read and never --write: %q", d, argv)
		}
	}
	for name, want := range map[string]string{"GOROOT": goroot, "GOMODCACHE": modcache, "GOPROXY": "off", "GOTOOLCHAIN": "local"} {
		if val(env, name) != want || !pair(argv, "--env", name) {
			t.Errorf("%s=%q (want %q, passed with --env): %q", name, val(env, name), want, argv)
		}
	}
	if p := val(env, "PATH"); !strings.HasPrefix(p, filepath.Join(goroot, "bin")+string(os.PathListSeparator)) {
		t.Errorf("PATH %q does not start with GOROOT/bin", p)
	}
	_, env = run([]string{goroot})
	if val(env, "GOROOT") != goroot || val(env, "GOMODCACHE") != "" || val(env, "GOPROXY") != "" {
		t.Errorf("an ungranted module cache reached the wall: %q", env)
	}
	argv, env = run([]string{modcache})
	if slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "GO") }) || strings.Contains(val(env, "PATH"), goroot) || pair(argv, "--read", goroot) {
		t.Errorf("an ungranted toolchain reached the wall: %q %q", env, argv)
	}
	if _, notes := goWallGrant_internal(WallConfig{GoRoot: goroot, GoModCache: modcache, ReadPaths: []string{modcache}}); len(notes) != 0 {
		t.Errorf("an ungranted go said it is read: %q", notes)
	}
	if _, notes := goWallGrant_internal(WallConfig{GoRoot: goroot, GoModCache: modcache, ReadPaths: []string{goroot}}); len(notes) != 1 || !strings.Contains(notes[0], "GOROOT="+goroot) || strings.Contains(notes[0], "GOMODCACHE") {
		t.Errorf("the start line for go without its cache: %q", notes)
	}
}

// DetectGo is go env outside the wall: its two lines, none when go is not
// on the worker's PATH, and an error when go is there and fails.
func TestDetectGoReadsGoEnvAndNoGoIsNone(t *testing.T) {
	t.Parallel()
	fx := edge.NewFakeExec()
	fx.Enqueue("go", edge.FakeOutcome{Stdout: "/opt/toolchains/go\n/srv/cache/gomod\n"}, edge.FakeOutcome{ExitCode: 1, Stderr: "go: broken"})
	if root, cache, err := DetectGo(context.Background(), fx, time.Minute); err != nil || root != "/opt/toolchains/go" || cache != "/srv/cache/gomod" {
		t.Fatalf("go env: %q %q %v", root, cache, err)
	}
	if got := fx.Calls()[0].Cmd.Argv; !slices.Equal(got, []string{"go", "env", "GOROOT", "GOMODCACHE"}) {
		t.Errorf("argv %q", got)
	}
	if _, _, err := DetectGo(context.Background(), fx, time.Minute); err == nil || !strings.Contains(err.Error(), "go: broken") {
		t.Errorf("a failing go env: %v", err)
	}
	missing := edge.NewFakeExec()
	missing.SetMissing("go", true)
	if root, cache, err := DetectGo(context.Background(), missing, time.Minute); err != nil || root != "" || cache != "" {
		t.Errorf("no go on PATH: %q %q %v", root, cache, err)
	}
}

// The GOROOT and GOMODCACHE go env names at start are granted read-only
// with no flag, after the --wall-read checks; a detected dir those refuse
// (under a dot-directory of the home) is skipped with one line, and the
// worker still starts.
func TestDetectedGoIsGrantedWithNoFlagsAndARefusedDirIsSkipped(t *testing.T) {
	t.Parallel()
	start := func(goroot, modcache, home string) (WallConfig, []string, error) {
		t.Helper()
		for _, d := range []string{goroot, modcache} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		fx := edge.NewFakeExec()
		fx.Enqueue("go", edge.FakeOutcome{Stdout: goroot + "\n" + modcache + "\n"})
		var lines []string
		cfg, err := osWallConfig_internal(context.Background(), Config{Row: "r1"}, fx, time.Minute, home, [][2]string{{"--root", filepath.Join(home, "state")}}, func(s string) { lines = append(lines, s) })
		return cfg, lines, err
	}
	home := t.TempDir()
	goroot, modcache := filepath.Join(home, "sdk", "go1.99.0"), filepath.Join(home, "go", "pkg", "mod")
	cfg, lines, err := start(goroot, modcache, home)
	rRoot, rCache := resolvePath_internal(goroot), resolvePath_internal(modcache)
	if err != nil || !slices.Equal(cfg.ReadPaths, []string{rRoot, rCache}) {
		t.Fatalf("detected go not granted: %q %v", cfg.ReadPaths, err)
	}
	env := GoWallEnv([]string{"PATH=/usr/bin"}, cfg)
	for _, want := range []string{"GOROOT=" + rRoot, "GOMODCACHE=" + rCache, "GOPROXY=off", "GOTOOLCHAIN=local", "PATH=" + filepath.Join(rRoot, "bin") + string(os.PathListSeparator) + "/usr/bin"} {
		if !slices.Contains(env, want) {
			t.Errorf("wall env %q lacks %s", env, want)
		}
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "GOROOT="+rRoot) || !strings.Contains(lines[0], "GOMODCACHE="+rCache) {
		t.Errorf("start lines %q: want one naming GOROOT= and GOMODCACHE=", lines)
	}

	home = t.TempDir()
	dotRoot := filepath.Join(home, ".local", "go")
	cfg, lines, err = start(dotRoot, filepath.Join(home, "go", "pkg", "mod"), home)
	if err != nil {
		t.Fatalf("a refused detected dir stopped the start: %v", err)
	}
	if slices.Contains(cfg.ReadPaths, resolvePath_internal(dotRoot)) || slices.ContainsFunc(GoWallEnv(nil, cfg), func(kv string) bool { return strings.HasPrefix(kv, "GO") }) {
		t.Errorf("a refused GOROOT reached the wall: %q", cfg.ReadPaths)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "GOROOT") || !strings.Contains(lines[0], "dot-directory") || !strings.Contains(lines[0], "skipped") {
		t.Errorf("start lines %q: want one saying the GOROOT is skipped and why", lines)
	}
}
