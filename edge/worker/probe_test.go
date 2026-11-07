package worker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// The worker's defects from the first bench runs (directives 42-45, 47-49,
// 52, 53, 55, 59): unit tier, a fake wall, a fake exec, no process, no
// socket, no clock.

// probeSprint is a sprint that answers next with tier models and records
// judgements and unhooks.
type probeSprint struct {
	cr2Sprint
	judgeErr   error
	judgements []store.Judgement
	unhooks    int
	hooks      int
}

func (s *probeSprint) OpenJudgement(_ context.Context, j store.Judgement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.judgements = append(s.judgements, j)
	return s.judgeErr
}
func (s *probeSprint) Unhook(context.Context, string) error { s.unhooks++; return nil }
func (s *probeSprint) Hook(context.Context, string) error   { s.hooks++; return nil }

// listingExec answers `opencode models` with models, and everything else ok.
func listingExec_internal(models ...string) *edge.FakeExec {
	fx := edge.NewFakeExec()
	fx.On("opencode", func(edge.FakeCall) edge.FakeOutcome {
		return edge.FakeOutcome{Stdout: "\x1b[1m" + strings.Join(models, "\n") + "\x1b[0m\n"}
	})
	return fx
}

func probeRunner_internal(t *testing.T, sp SprintClient, wall ReserveWall, ex edge.Exec, proxy *KeyProxy) *Runner {
	t.Helper()
	r, err := NewRunner(Runner{Host: "b1", RootDir: t.TempDir(), Sprint: sp, Wall: wall, Exec: ex, Proxy: proxy, ProxyAddr: "127.0.0.1:7000",
		DefaultHarness: &OpenCodeHarness{}, Now: testClock_internal(), Sleep: noSleep_internal})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func envOf_internal(kv map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
}

func threeProviders_internal(t *testing.T, env map[string]string) *KeyProxy {
	t.Helper()
	ps, err := ParseProviders("deepseek=https://api.deepseek.example@DEEPSEEK_API_KEY,inception=https://api.inception.example@INCEPTION_API_KEY,paperinstruments=https://api.pi.example@PAPERINSTRUMENTS_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	return NewKeyProxy(ps, envOf_internal(env))
}

var allKeys_internal = map[string]string{"DEEPSEEK_API_KEY": "sk-d", "INCEPTION_API_KEY": "sk-i", "PAPERINSTRUMENTS_API_KEY": "sk-p"}

// 27, 36: the probe runs the policy's model for each tier (the first
// candidate the harness serves, passed as --model in provider/model form),
// and a tier with no model is refused by name.
func TestProbeRunsThePolicysModelPerTier(t *testing.T) {
	t.Parallel()
	sp := &probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{Row: "b1", TierModels: map[string][]string{
		"flash": {"anthropic/haiku", "deepseek/deepseek-flash"}, "pro": {"inception/mercury-3"}, "frontier": {"anthropic/fable"},
	}}}}
	var mu sync.Mutex
	var models []string
	wall := &fakeWall{runFn: func(_ int, j Job, argv, _ []string) (WallRunResult, error) {
		mu.Lock()
		models = append(models, argv[slices.Index(argv, "--model")+1])
		mu.Unlock()
		_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
		return WallRunResult{Elapsed: 2 * time.Second}, nil
	}}
	var warned []string
	r := probeRunner_internal(t, sp, wall, listingExec_internal("deepseek/deepseek-flash", "inception/mercury-3", "anthropic/fable"), threeProviders_internal(t, allKeys_internal))
	r.Warn = func(s string) { warned = append(warned, s) }
	if err := r.ProbeRow(context.Background(), "b1", "opencode"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(models, []string{"deepseek/deepseek-flash", "inception/mercury-3"}) {
		t.Fatalf("probed %v: want the first served candidate of each tier", models)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "tier frontier: anthropic/fable: no key-proxy route for anthropic") {
		t.Fatalf("an unserved tier is warned, naming why: %q", warned)
	}
	sp.next.TierModels["heavy"] = nil
	if err := r.ProbeRow(context.Background(), "b1", "opencode"); err == nil || !strings.Contains(err.Error(), "no model for tier heavy") {
		t.Fatalf("a tier the policy gives no model: %v", err)
	}
}

// 36: a model the harness does not list is not served; with none served the
// refusal names the policy's models and the harness's list.
func TestProbeRefusesAPolicyModelTheHarnessDoesNotList(t *testing.T) {
	t.Parallel()
	sp := &probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{TierModels: map[string][]string{"flash": {"deepseek/deepseek-chat"}}}}}
	r := probeRunner_internal(t, sp, &fakeWall{}, listingExec_internal("deepseek/deepseek-flash", "deepseek/deepseek-v4-pro", "inception/mercury-3"), threeProviders_internal(t, allKeys_internal))
	err := r.ProbeRow(context.Background(), "b1", "opencode")
	for _, want := range []string{"deepseek/deepseek-chat: not in the harness's model list", "deepseek/deepseek-flash deepseek/deepseek-v4-pro"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q in %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "mercury") {
		t.Fatalf("the list named is the policy's providers' models only: %v", err)
	}
}

// 26, 29: every route's key comes from the environment; a route whose key
// is absent refuses at the probe, naming the key.
func TestProbeRefusesARouteWithNoKey(t *testing.T) {
	t.Parallel()
	sp := &probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{TierModels: map[string][]string{"flash": {"deepseek/deepseek-flash"}}}}}
	wall := &fakeWall{}
	r := probeRunner_internal(t, sp, wall, listingExec_internal("deepseek/deepseek-flash"), threeProviders_internal(t, map[string]string{"DEEPSEEK_API_KEY": "sk-d"}))
	err := r.ProbeRow(context.Background(), "b1", "opencode")
	for _, want := range []string{"provider inception: INCEPTION_API_KEY is not in the worker's environment", "PAPERINSTRUMENTS_API_KEY", "nova-secrets exec --only INCEPTION_API_KEY"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q in %v", want, err)
		}
	}
	if wall.calls != 0 {
		t.Fatalf("the harness ran with a key missing")
	}
	if len(sp.judgements) != 1 || sp.judgements[0].ID != "j-probe-b1" {
		t.Fatalf("the probe's judgement: %+v", sp.judgements)
	}
}

// 25, 28, 30, 31, 32: a failed probe says the harness's error line (not a
// bare reset), keeps stdout.log, stderr.log and command.txt, names a death
// inside the wall before the harness's log with the command that shows the
// wall, names the harness's known limits, and says the judgement it could
// not open.
func TestProbeFailureIsReadable(t *testing.T) {
	t.Parallel()
	sp := &probeSprint{judgeErr: errors.New("unknown verb"), cr2Sprint: cr2Sprint{next: NextResponse{TierModels: map[string][]string{"flash": {"deepseek/deepseek-flash"}}}}}
	fx := listingExec_internal("deepseek/deepseek-flash")
	fx.On("nova-reserve", func(edge.FakeCall) edge.FakeOutcome {
		return edge.FakeOutcome{ExitCode: 1, Stdout: "", Stderr: "WALL grants=/job net=denied\n\x1b[91mError: \x1b[0mUnexpected server error\n\x1b[0m\n", Duration: 300 * time.Millisecond}
	})
	wall := &OSReserveWall{Exec: fx, LookPath: func(string) (string, error) { return "/opt/oc/bin/opencode", nil },
		Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", NetDeny: true}}
	r := probeRunner_internal(t, sp, wall, fx, threeProviders_internal(t, allKeys_internal))
	err := r.ProbeRow(context.Background(), "b1", "opencode")
	if err == nil {
		t.Fatal("the probe passed")
	}
	msg := err.Error()
	for _, want := range []string{
		"opencode with deepseek/deepseek-flash: exit 1: Error: Unexpected server error",
		"the harness died inside the wall before writing a log; nova-sandbox show --write ",
		"--unix --net-deny --net-allow 127.0.0.1:7000",
		"known limits of opencode: through the key proxy",
		"(and its judgement could not be opened: unknown verb)",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("want %q in %s", want, msg)
		}
	}
	dir := filepath.Join(r.RootDir, "probes", "b1", "deepseek_deepseek-flash")
	for name, want := range map[string]string{"stderr.log": "Unexpected server error", "command.txt": "nova-sandbox --write " + dir} {
		b, rerr := os.ReadFile(filepath.Join(dir, name))
		if rerr != nil || !strings.Contains(string(b), want) {
			t.Fatalf("%s: %q %v: want %q", name, b, rerr, want)
		}
	}
	if !strings.Contains(msg, dir+" (stdout.log, stderr.log, command.txt)") {
		t.Fatalf("the refusal names where the output is: %s", msg)
	}
}

// 24, 32: the wall reads the harness binary's directory and its symlink's
// target's; each adapter's needs reach the argv, and nothing else's.
func TestWallGrantsTheHarnessAndHonoursEachAdaptersNeeds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	real := filepath.Join(dir, "harness-v1", "opencode")
	link := filepath.Join(dir, "bin", "opencode")
	for _, d := range []string{filepath.Dir(real), filepath.Dir(link)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	realDir, err := filepath.EvalSymlinks(filepath.Dir(real))
	if err != nil {
		t.Fatal(err)
	}
	fx := edge.NewFakeExec()
	fx.On("nova-reserve", func(edge.FakeCall) edge.FakeOutcome { return edge.FakeOutcome{} })
	wall := &OSReserveWall{Exec: fx, LookPath: func(string) (string, error) { return link, nil }, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", NetDeny: true}}
	cases := []struct {
		h          Harness
		want, lack []string
	}{
		{&OpenCodeHarness{}, []string{"--read " + filepath.Dir(link), "--read " + realDir, "--unix", "--net-deny --net-allow 127.0.0.1:7000", "--env TMPDIR", "--env OPENAI_BASE_URL", "--env NOVA_WORKER_NONCE"}, []string{"--env PATH", "--env HOME"}},
		{&ClaudeHarness{ConfigDir: "/cfg/claude"}, []string{"--write /cfg/claude", "--env CLAUDE_CONFIG_DIR"}, []string{"--unix", "--net-deny"}},
		{&ScriptHarness{}, []string{"--net-deny"}, []string{"--unix", "--net-allow"}},
	}
	for _, c := range cases {
		j := Job{Consumer: "c1", JobDir: t.TempDir(), ProxyAddr: "127.0.0.1:7000", Model: "deepseek/deepseek-flash", Needs: c.h.Needs()}
		argv, env := c.h.Command(j)
		res, err := wall.RunHarness(context.Background(), j, argv, env, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Join(res.Argv, " ")
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: want %q in %s", c.h.Name(), w, got)
			}
		}
		for _, l := range c.lack {
			if strings.Contains(got, l) {
				t.Errorf("%s: %q must not be in %s", c.h.Name(), l, got)
			}
		}
	}
}

// 29: one proxy, a route per provider, chosen by the base URL's provider
// segment; each upstream gets its own key and the segment is cut.
func TestKeyProxyRoutesByProviderPrefix(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	seen := map[string]string{}
	p := threeProviders_internal(t, allKeys_internal)
	p.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		seen[req.URL.Host+req.URL.Path] = req.Header.Get("Authorization")
		mu.Unlock()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}}, nil
	})
	for _, model := range []string{"deepseek/deepseek-flash", "inception/mercury-3", "paperinstruments/ultramarine-1-flash"} {
		base := ProxyBaseURL("127.0.0.1:7000", model)
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, httptest.NewRequest("POST", base+"/chat/completions", strings.NewReader("{}")))
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", model, rec.Code, rec.Body)
		}
	}
	want := map[string]string{"api.deepseek.example/v1/chat/completions": "Bearer sk-d", "api.inception.example/v1/chat/completions": "Bearer sk-i", "api.pi.example/v1/chat/completions": "Bearer sk-p"}
	for k, v := range want {
		if seen[k] != v {
			t.Fatalf("upstream %s got %q, want %q (all: %v)", k, seen[k], v, seen)
		}
	}
	rec := httptest.NewRecorder()
	q := threeProviders_internal(t, map[string]string{})
	q.ServeHTTP(rec, httptest.NewRequest("POST", "/deepseek/v1/chat/completions", strings.NewReader("{}")))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "DEEPSEEK_API_KEY is not in the worker's environment") {
		t.Fatalf("a missing key at request time: %d %s", rec.Code, rec.Body)
	}
}

// 35: a sprint verb runs directly, inheriting the worker's environment (the
// seat's credentials); never a bare nova-secrets exec, which nova-tools v2
// refuses.
func TestSprintVerbsRunDirectly(t *testing.T) {
	t.Parallel()
	var argvs [][]string
	c := &CLISprintClient{SprintBin: "/usr/local/bin/nova-sprint", Run: func(_ context.Context, argv []string) ([]byte, int, error) {
		argvs = append(argvs, argv)
		return []byte("{}"), 0, nil
	}}
	if err := c.Hook(context.Background(), "b1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Next(context.Background(), "b1"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(argvs[0], []string{"/usr/local/bin/nova-sprint", "hook", "--as", "b1", "--role", "worker"}) || argvs[1][0] != "/usr/local/bin/nova-sprint" {
		t.Fatalf("argv %q", argvs)
	}
	for _, f := range []string{"verbs.go", "serve.go", "proxy.go", "runner.go", "probe.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `"nova-secrets"`) {
			t.Errorf("%s runs nova-secrets: the worker reads its keys and credentials from its environment", f)
		}
	}
}

// Take: a bench's receipt carries the policy's candidates; the job runs the
// first the probe proved, and none proved is a fault naming them.
func TestTakeRunsAProvedCandidateOrFaults(t *testing.T) {
	t.Parallel()
	r := probeRunner_internal(t, &probeSprint{}, &fakeWall{}, nil, nil)
	r.served["inception/mercury-3"] = true
	m, err := r.chooseModel_internal("opencode", TakeInfo{Tier: "pro", Models: []string{"anthropic/sonnet", "inception/mercury-3"}})
	if err != nil || m != "inception/mercury-3" {
		t.Fatalf("model %q err %v", m, err)
	}
	if _, err := r.chooseModel_internal("opencode", TakeInfo{Tier: "heavy", Models: []string{"anthropic/opus"}}); err == nil || !strings.Contains(err.Error(), "serves none of the policy's models for tier heavy (anthropic/opus)") {
		t.Fatalf("no proved candidate: %v", err)
	}
	if m, err := r.chooseModel_internal("opencode", TakeInfo{Model: "m-friend", Models: []string{"x/y"}}); err != nil || m != "m-friend" {
		t.Fatalf("a receipt's own model is run as it is: %q %v", m, err)
	}
}

// 42: a work clone and a mirror per stream repo, made on first use under
// the roots, the repo from the receipt; a second card of the repo reuses
// them; a clone of another repo in the way is refused.
func TestStagePerStreamRepo(t *testing.T) {
	t.Parallel()
	for repo, want := range map[[2]string]string{
		{"mas-bandwidth/nova-tools", "github"}: "https://github.com/mas-bandwidth/nova-tools.git",
		{"/srv/git/x.git", ""}:                 "/srv/git/x.git",
		{"https://h.example/o/r.git", ""}:      "https://h.example/o/r.git",
	} {
		if got, err := CloneURL(repo[0], repo[1]); err != nil || got != want {
			t.Fatalf("CloneURL(%q, %q) = %q, %v", repo[0], repo[1], got, err)
		}
	}
	if _, err := CloneURL("o/r", ""); err == nil || !strings.Contains(err.Error(), "forge \"\" gives no clone URL") {
		t.Fatalf("org/repo on no forge: %v", err)
	}
	roots := t.TempDir()
	fx := edge.NewFakeExec()
	fx.On("git", func(c edge.FakeCall) edge.FakeOutcome {
		args := c.Cmd.Argv[5:] // after git -c hooksPath -c fsmonitor
		switch {
		case args[0] == "clone":
			_ = os.MkdirAll(args[len(args)-1], 0o755)
		case args[0] == "remote" && args[1] == "get-url":
			return edge.FakeOutcome{Stdout: "https://github.com/o/other.git\n"}
		case args[0] == "rev-parse":
			return edge.FakeOutcome{Stdout: strings.Repeat("a", 40) + "\n"}
		}
		return edge.FakeOutcome{}
	})
	g := &OSGitOps{WorkRoot: filepath.Join(roots, "clones"), MirrorRoot: filepath.Join(roots, "mirror"), Exec: fx}
	j := Job{Repo: "o/r", Forge: "github", Kind: "work", Branch: "sprint/s/p1", BaseSHA: strings.Repeat("b", 40), WorktreeDir: filepath.Join(roots, "job", "repo")}
	if _, err := g.StageWorktree(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	var clones []string
	for _, c := range fx.Calls() {
		if c.Cmd.Argv[5] == "clone" {
			clones = append(clones, strings.Join(c.Cmd.Argv[5:], " "))
		}
	}
	mirror, work := filepath.Join(roots, "mirror", "r.git"), filepath.Join(roots, "clones", "r")
	if len(clones) != 3 || clones[0] != "clone --quiet --mirror https://github.com/o/r.git "+mirror ||
		clones[1] != "clone --quiet --no-checkout --reference "+mirror+" https://github.com/o/r.git "+work || !strings.HasPrefix(clones[2], "clone --shared --no-checkout "+work) {
		t.Fatalf("first use: %q", clones)
	}
	g2 := &OSGitOps{WorkRoot: g.WorkRoot, MirrorRoot: g.MirrorRoot, Exec: fx}
	if _, err := g2.StageWorktree(context.Background(), j); err == nil || !strings.Contains(err.Error(), "is of \"https://github.com/o/other.git\", not \"https://github.com/o/r.git\"") {
		t.Fatalf("a work clone of another repo: %v", err)
	}
}

// The binary's order: the bus hook and the self-unhook, the probe, and
// only then the sprint hook: a failed probe never hooks the row in to the
// sprint and stops the bus hook, so no bench is up with no worker able to
// run (and a hooked row is a probed one: up's step 7, admission). A passed
// probe hooks once.
func TestServeRowProbesBeforeTheSprintHook(t *testing.T) {
	t.Parallel()
	sp := &probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{TierModels: map[string][]string{"flash": {}}}}}
	hook := newFakeWakeHook()
	r := probeRunner_internal(t, sp, &fakeWall{}, listingExec_internal("deepseek/deepseek-flash"), threeProviders_internal(t, allKeys_internal))
	r.BusHook = func(context.Context, string) (WakeHook, error) { return hook, nil }
	err := r.ServeRow(context.Background(), "b1", "opencode", true)
	if err == nil || !strings.Contains(err.Error(), "no model for tier flash") {
		t.Fatalf("ServeRow: %v", err)
	}
	if sp.unhooks != 1 || sp.hooks != 0 || hook.stops() != 1 {
		t.Fatalf("unhooks %d (a stale hook's only), sprint hooks %d (none: the probe failed), bus hook stops %d", sp.unhooks, sp.hooks, hook.stops())
	}
	sp.next.TierModels["flash"] = []string{"deepseek/deepseek-flash"}
	wall := &fakeWall{runFn: func(_ int, j Job, _, _ []string) (WallRunResult, error) {
		_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
		return WallRunResult{Elapsed: time.Second}, nil
	}}
	r = probeRunner_internal(t, sp, wall, listingExec_internal("deepseek/deepseek-flash"), threeProviders_internal(t, allKeys_internal))
	r.BusHook = func(context.Context, string) (WakeHook, error) { return newFakeWakeHook(), nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the pass after the hook sees a cancelled context: only the order is under test
	_ = r.ServeRow(ctx, "b1", "opencode", true)
	if sp.hooks != 1 {
		t.Fatalf("a passed probe hooks the row once: %d", sp.hooks)
	}
}
