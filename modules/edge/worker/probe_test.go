package worker

import (
	"context"
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

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// The worker's defects from the first bench runs (directives 42-45, 47-49,
// 52, 53, 55, 59): unit tier, a fake wall, a fake exec, no process, no
// socket, no clock.

// probeSprint is a sprint that answers next with tier models and records
// hooks and unhooks.
type probeSprint struct {
	cr2Sprint
	unhooks int
	hooks   int
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

// The listing's home, tmp and data are fresh each time: a database a
// previous opencode left (2.2.6's, which 1.18.20 refuses: "Database is not
// empty and has no session table") is gone before `opencode models` runs.
func TestProbeListsModelsInAFreshDataDir(t *testing.T) {
	t.Parallel()
	sp := &probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{TierModels: map[string][]string{"flash": {"deepseek/deepseek-flash"}}}}}
	r := probeRunner_internal(t, sp, &fakeWall{}, listingExec_internal("deepseek/deepseek-flash"), threeProviders_internal(t, allKeys_internal))
	var stale []string
	for _, sub := range []string{"home", "tmp", filepath.Join("data", "opencode")} {
		p := filepath.Join(r.RootDir, "probes", "b1", sub, "stale")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		stale = append(stale, p)
	}
	if _, err := r.listModels_internal(context.Background(), "b1", &OpenCodeHarness{}, []string{"deepseek/deepseek-flash"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range stale {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived the listing: %v", p, err)
		}
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
}

// Defect 30: nova-sprint has no verb a worker can open a judgement with, so
// a failed probe runs no sprint verb for one and its refusal is the probe's
// cause alone, with no "could not be opened" tail (`nova-sprint check`
// surfaces the refusal from the worker's last log line).
func TestProbeRefusalRunsNoJudgementVerb(t *testing.T) {
	t.Parallel()
	var calls [][]string
	cli := &CLISprintClient{Run: func(_ context.Context, argv []string) ([]byte, int, error) {
		calls = append(calls, slices.Clone(argv))
		return []byte("SPRINT REFUSED " + argv[1] + ": no acting name\n"), 1, nil
	}}
	r := probeRunner_internal(t, cli, &fakeWall{}, listingExec_internal("deepseek/deepseek-flash"), threeProviders_internal(t, map[string]string{"DEEPSEEK_API_KEY": "sk-d"}))
	err := r.ProbeRow(context.Background(), "amd-1", "opencode")
	if err == nil || !strings.Contains(err.Error(), "worker refused: probe failed for row amd-1: ") {
		t.Fatalf("want the probe's refusal, got %v", err)
	}
	if strings.Contains(err.Error(), "judgement") {
		t.Fatalf("the refusal names a judgement: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("a failed probe ran sprint verbs: %q", calls)
	}
}

// A probe whose harness exits 0 with chatter on stderr and no verdict says
// the verdict, not only the chatter: the live refusal read "exit 0: Wrote
// file successfully." (hetzner2, 2026-11-09) and never why it failed.
func TestProbeFailureSaysTheVerdict(t *testing.T) {
	t.Parallel()
	sp := &probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{TierModels: map[string][]string{"flash": {"deepseek/deepseek-flash"}}}}}
	wall := &fakeWall{runFn: func(int, Job, []string, []string) (WallRunResult, error) {
		return WallRunResult{Stderr: []byte("> build · mercury-3\n← Write result.json\nWrote file successfully.\n"), Elapsed: 2 * time.Second}, nil
	}}
	r := probeRunner_internal(t, sp, wall, listingExec_internal("deepseek/deepseek-flash"), threeProviders_internal(t, allKeys_internal))
	err := r.ProbeRow(context.Background(), "b1", "opencode")
	for _, want := range []string{"exit 0: Wrote file successfully.", "no verdict: the harness exited 0 without writing result.json"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q in %v", want, err)
		}
	}
}

// 25, 28, 30, 31, 32: a failed probe says the harness's error line (not a
// bare reset), keeps stdout.log, stderr.log and command.txt, names a death
// inside the wall before the harness's log with the command that shows the
// wall, and names the harness's known limits.
func TestProbeFailureIsReadable(t *testing.T) {
	t.Parallel()
	sp := &probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{TierModels: map[string][]string{"flash": {"deepseek/deepseek-flash"}}}}}
	fx := listingExec_internal("deepseek/deepseek-flash")
	fx.On("nova-reserve", func(edge.FakeCall) edge.FakeOutcome {
		return edge.FakeOutcome{ExitCode: 1, Stdout: "", Stderr: "WALL grants=/job net=denied\n\x1b[91mError: \x1b[0mUnexpected server error\n\x1b[0m\n", Duration: 300 * time.Millisecond}
	})
	wall := &OSReserveWall{Exec: fx, LookPath: func(string) (string, error) { return "/opt/oc/bin/opencode", nil },
		Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", NetDeny: true, GOOS: "linux"}}
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
		"known limits of opencode: opencode 2.x starts a background service",
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
	wall := &OSReserveWall{Exec: fx, LookPath: func(string) (string, error) { return link, nil }, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", NetDeny: true, GOOS: "linux"}}
	cases := []struct {
		h          Harness
		want, lack []string
	}{
		{&OpenCodeHarness{}, []string{"--read " + filepath.Dir(link), "--read " + realDir, "--unix", "--net-deny --net-allow 127.0.0.1:7000", "--env TMPDIR", "--env OPENCODE_CONFIG_CONTENT", "--env NOVA_WORKER_NONCE"}, []string{"--env PATH", "--env HOME"}},
		{&ClaudeHarness{ConfigDir: "/cfg/claude"}, []string{"--write /cfg/claude", "--env CLAUDE_CONFIG_DIR"}, []string{"--unix", "--net-deny"}},
		{&ScriptHarness{}, []string{"--net-deny"}, []string{"--unix", "--net-allow"}},
	}
	for _, c := range cases {
		j := Job{Consumer: "c1", JobDir: t.TempDir(), ProxyAddr: "127.0.0.1:7000", Routes: []string{"deepseek"}, Model: "deepseek/deepseek-flash", Needs: c.h.Needs()}
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

// nova-sandbox refuses --unix on macOS (bad_unix, exit 2), where a socket
// inside a --write grant is reachable without it: an adapter's Unix need is
// passed on Linux only, or every macOS row's probe fails and the row is held.
func TestWallPassesUnixOnLinuxOnly(t *testing.T) {
	t.Parallel()
	j := Job{Consumer: "c1", JobDir: "/job", ProxyAddr: "127.0.0.1:7000", Needs: (&OpenCodeHarness{}).Needs()}
	for goos, want := range map[string]bool{"linux": true, "darwin": false} {
		got := strings.Join(sandboxFlags_internal(WallConfig{NetDeny: true, GOOS: goos}, j, nil), " ")
		if strings.Contains(got, "--unix") != want || !strings.Contains(got, "--net-deny --net-allow 127.0.0.1:7000") {
			t.Errorf("%s: want --unix %t and the proxy's --net-allow: %s", goos, want, got)
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

// Take: a receipt carries the one model its deal named (defect 57); the job
// runs it, and one the probe did not prove (when it proved any) is a fault
// naming it and the fix.
func TestTakeRunsTheDealsModelOrFaults(t *testing.T) {
	t.Parallel()
	r := probeRunner_internal(t, &probeSprint{}, &fakeWall{}, nil, nil)
	if m, err := r.chooseModel_internal("opencode", TakeInfo{Tier: "pro", Model: "inception/mercury-3"}); err != nil || m != "inception/mercury-3" {
		t.Fatalf("no probe yet: the deal's model as it is: %q %v", m, err)
	}
	r.served["inception/mercury-3"] = true
	m, err := r.chooseModel_internal("opencode", TakeInfo{Tier: "pro", Model: "inception/mercury-3"})
	if err != nil || m != "inception/mercury-3" {
		t.Fatalf("model %q err %v", m, err)
	}
	if _, err := r.chooseModel_internal("opencode", TakeInfo{Tier: "heavy", Model: "anthropic/opus"}); err == nil || !strings.Contains(err.Error(), "does not serve anthropic/opus, the model the deal named for tier heavy (its probe proved inception/mercury-3)") {
		t.Fatalf("an unproved model: %v", err)
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
		if got, _, err := cloneURL_internal(Job{Repo: repo[0], Forge: repo[1]}); err != nil || got != want {
			t.Fatalf("cloneURL_internal(%q, %q) = %q, %v", repo[0], repo[1], got, err)
		}
	}
	roots := t.TempDir()
	fx := edge.NewFakeExec()
	fx.On("git", func(c edge.FakeCall) edge.FakeOutcome {
		args := c.Cmd.Argv[gitArg0_internal:] // after gitPrefix_internal
		switch {
		case args[0] == "init":
			_ = os.MkdirAll(args[len(args)-1], 0o755)
		case args[0] == "remote" && args[1] == "get-url":
			return edge.FakeOutcome{Stdout: "https://github.com/o/other.git\n"}
		case args[0] == "rev-parse":
			return edge.FakeOutcome{Stdout: strings.Repeat("a", 40) + "\n"}
		}
		return edge.FakeOutcome{}
	})
	g := &OSGitOps{WorkRoot: filepath.Join(roots, "clones"), MirrorRoot: filepath.Join(roots, "mirror"), Exec: fx}
	j := Job{Repo: "o/r", Forge: "github", Kind: "work", Branch: "sprint/s/p1", BaseSHA: strings.Repeat("b", 40), WorktreeDir: filepath.Join(roots, "job", "repo"), Consumer: "p1.w1"}
	if _, err := g.StageWorktree(context.Background(), j); err == nil || !strings.Contains(err.Error(), "p1.w1 has no commit identity in its receipt") {
		t.Fatalf("a job with no identity is staged: %v", err)
	}
	j.CommitName, j.CommitEmail = testCommitName, testCommitEmail
	if err := os.RemoveAll(roots); err != nil {
		t.Fatal(err)
	}
	before := len(fx.Calls())
	if _, err := g.StageWorktree(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	var clones, config []string
	for _, c := range fx.Calls()[before:] {
		switch c.Cmd.Argv[gitArg0_internal] {
		case "clone", "init", "remote", "fetch":
			clones = append(clones, strings.Join(c.Cmd.Argv[gitArg0_internal:], " "))
		case "config":
			if c.Cmd.Argv[gitArg0_internal+1] != "--local" { // the config guard's read
				config = append(config, strings.Join(c.Cmd.Argv[gitArg0_internal+1:], " "))
			}
		}
	}
	if strings.Join(config, ";") != "user.name "+testCommitName+";user.email "+testCommitEmail {
		t.Fatalf("the job's clone is configured as %q, not the sprint's identity", config)
	}
	mirror, work := filepath.Join(roots, "mirror", "r.git"), filepath.Join(roots, "clones", "r")
	// First use makes the mirror and the work clone empty, never a clone of
	// the repo (staging fetches shallow, into them, with Fetch), and the
	// job's tree shares the work clone's objects.
	wt := filepath.Join(roots, "job", "repo")
	want := []string{"init --quiet --bare " + mirror, "init --quiet " + work, "remote add origin https://github.com/o/r.git",
		"init --quiet " + wt, "remote add origin " + work, "remote set-url origin https://github.com/o/r.git"}
	if strings.Join(clones, ";") != strings.Join(want, ";") {
		t.Fatalf("first use: %q, want %q", clones, want)
	}
	alt, err := os.ReadFile(filepath.Join(wt, ".git", "objects", "info", "alternates"))
	if err != nil || string(alt) != filepath.Join(work, ".git", "objects")+"\n"+filepath.Join(mirror, "objects")+"\n" {
		t.Fatalf("the job's tree borrows %q (%v), want the work clone's objects then the mirror's", alt, err)
	}
	g2 := &OSGitOps{WorkRoot: g.WorkRoot, MirrorRoot: g.MirrorRoot, Exec: fx}
	if _, err := g2.StageWorktree(context.Background(), j); err == nil || !strings.Contains(err.Error(), "is of \"https://github.com/o/other.git\", not \"https://github.com/o/r.git\"") {
		t.Fatalf("a work clone of another repo: %v", err)
	}
}

// The binary's order: the bus hook and the self-unhook, the probe, and
// only then the sprint hook: a failed probe never hooks the row in to the
// sprint and stops the bus hook, so no bench is up with no worker able to
// run (and a hooked row is a probed one: up's step 7, admission); the row
// is held and probed again. A passed probe hooks once.
func TestServeRowProbesBeforeTheSprintHook(t *testing.T) {
	t.Parallel()
	sp := &probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{TierModels: map[string][]string{"flash": {}}}}}
	hook := newFakeWakeHook()
	r := probeRunner_internal(t, sp, &fakeWall{}, listingExec_internal("deepseek/deepseek-flash"), threeProviders_internal(t, allKeys_internal))
	r.BusHook = func(context.Context, string) (WakeHook, error) { return hook, nil }
	held, stopHeld := context.WithCancel(context.Background())
	var warned string
	r.Warn = func(s string) {
		if strings.Contains(s, "held") {
			warned = s
			stopHeld()
		}
	}
	if err := r.ServeRow(held, "b1", "opencode"); err != nil || !strings.Contains(warned, "no model for tier flash") {
		t.Fatalf("ServeRow: %v, warned %q", err, warned)
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
	_ = r.ServeRow(ctx, "b1", "opencode")
	if sp.hooks != 1 {
		t.Fatalf("a passed probe hooks the row once: %d", sp.hooks)
	}
}
