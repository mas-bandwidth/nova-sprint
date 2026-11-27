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
)

// Unit tier: no real sockets, no real processes, no clock sleeps.

// A proxied ClaudeHarness strips the provider prefix for --model, points
// ANTHROPIC_BASE_URL at /<route> (without /v1, since claude appends
// /v1/messages), sets placeholder ANTHROPIC_AUTH_TOKEN, ANTHROPIC_MODEL,
// ANTHROPIC_SMALL_FAST_MODEL, job-confined config/tmp dirs, and disables
// nonessential traffic inside a --net-deny --net-allow <proxy> wall.
func TestProxiedClaudeInkyCommandAndWall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := &ClaudeHarness{Proxied: true}
	j := Job{
		Model:     "inky/inkling-2-mini",
		Consumer:  "p1.w1",
		Row:       "macbook",
		JobDir:    dir,
		ProxyAddr: "127.0.0.1:7000",
		Routes:    []string{"inky"},
	}
	argv, env := h.Command(j)
	wantArgv := []string{"claude", "--print", "--dangerously-skip-permissions", "--disallowedTools", "Task", "--model", "inkling-2-mini"}
	if len(argv) < len(wantArgv) || !slices.Equal(argv[:len(wantArgv)], wantArgv) {
		t.Fatalf("argv = %q, want prefix %q", argv, wantArgv)
	}
	for _, kv := range []string{
		"ANTHROPIC_BASE_URL=http://127.0.0.1:7000/inky",
		"ANTHROPIC_AUTH_TOKEN=" + ProxyKey("p1.w1"),
		"ANTHROPIC_MODEL=inkling-2-mini",
		"ANTHROPIC_SMALL_FAST_MODEL=inkling-2-mini",
		"CLAUDE_CONFIG_DIR=" + filepath.Join(dir, "home", ".claude"),
		"CLAUDE_CODE_TMPDIR=" + filepath.Join(dir, "tmp"),
		"DISABLE_AUTOUPDATER=1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
	} {
		if !slices.Contains(env, kv) {
			t.Errorf("env missing %s: %v", kv, env)
		}
	}
	if err := h.WallRefusal(j); err != nil {
		t.Fatalf("WallRefusal(inky/inkling-2-mini): %v", err)
	}
	j.Needs = h.Needs()
	if j.Needs.NetOpen || !j.Needs.Proxy || j.Needs.Provider != "" {
		t.Fatalf("proxied ClaudeHarness Needs = %+v", j.Needs)
	}
	flags := strings.Join(sandboxFlags_internal(WallConfig{NetDeny: true}, j, env), " ")
	if !strings.Contains(flags, "--net-deny --net-allow 127.0.0.1:7000") || strings.Count(flags, "--net-allow") != 1 {
		t.Fatalf("wall flags = %q", flags)
	}

	// HarnessDirs resolves ~/.local/bin/claude symlink and its target version dir.
	fakeHome := t.TempDir()
	realBin := filepath.Join(fakeHome, ".local", "share", "claude", "versions", "claude-2.1")
	linkBin := filepath.Join(fakeHome, ".local", "bin", "claude")
	for _, d := range []string{filepath.Dir(realBin), filepath.Dir(linkBin)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBin, linkBin); err != nil {
		t.Fatal(err)
	}
	dirs := HarnessDirs("claude", func(name string) (string, error) {
		if name != "claude" {
			t.Fatalf("unexpected lookPath(%q)", name)
		}
		return linkBin, nil
	})
	realDir, err := filepath.EvalSymlinks(filepath.Dir(realBin))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(dirs, filepath.Dir(linkBin)) || !slices.Contains(dirs, realDir) {
		t.Fatalf("HarnessDirs = %v, want %s and %s", dirs, filepath.Dir(linkBin), realDir)
	}

	// Unproxied ClaudeHarness on its own OAuth login never injects ANTHROPIC_*.
	if _, unproxiedEnv := (&ClaudeHarness{}).Command(Job{Model: "anthropic/opus", Consumer: "p1.w1", JobDir: dir}); slices.ContainsFunc(unproxiedEnv, func(kv string) bool {
		return strings.HasPrefix(kv, "ANTHROPIC_")
	}) {
		t.Fatalf("unproxied ClaudeHarness leaked ANTHROPIC_*: %v", unproxiedEnv)
	}
}

// Blocking #1: on a worker whose only route is inky, an unrouted anthropic/* or
// deepseek/* model is refused at chooseModel_internal (even before probe),
// refused by ClaudeHarness.WallRefusal, never given ANTHROPIC_BASE_URL in
// Command, and refused with HTTP 502 by KeyProxy instead of falling back to inky.
func TestUnroutedModelOnInkyWorkerIsRefusedEverywhere(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ps, err := ParseProviders("inky=http://127.0.0.1:8096@INKY_SERVE_KEY")
	if err != nil {
		t.Fatal(err)
	}
	proxy := NewKeyProxy(ps, envOf_internal(map[string]string{"INKY_SERVE_KEY": "inky-secret-fixture"}))
	h := &ClaudeHarness{Proxied: true}

	// Runner with len(proved) == 0 (no probe run yet) still refuses unrouted models.
	r := probeRunner_internal(t, &probeSprint{}, &fakeWall{}, nil, proxy)
	r.DefaultHarness = h
	for _, unrouted := range []string{"anthropic/opus", "deepseek/deepseek-flash"} {
		if _, err := r.chooseModel_internal("claude", TakeInfo{Tier: "pro", Model: unrouted}); err == nil || !strings.Contains(err.Error(), "no key-proxy route") {
			t.Fatalf("chooseModel(%s) before probe = %v, want route refusal", unrouted, err)
		}
		j := Job{Model: unrouted, Consumer: "p1.w1", Row: "macbook", JobDir: dir, ProxyAddr: "127.0.0.1:7000", Routes: []string{"inky"}}
		if err := h.WallRefusal(j); err == nil || !strings.Contains(err.Error(), "no key-proxy route") {
			t.Fatalf("WallRefusal(%s) = %v, want route refusal", unrouted, err)
		}
		if _, env := h.Command(j); slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "ANTHROPIC_") }) {
			t.Fatalf("Command(%s) injected ANTHROPIC_* without a route: %v", unrouted, env)
		}
	}

	// KeyProxy with a single inky route refuses /anthropic/v1/messages and /deepseek/v1/chat/completions (never falls back to inky).
	for _, path := range []string{"/anthropic/v1/messages", "/deepseek/v1/chat/completions"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7000"+path, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+ProxyKey("p1.w1"))
		proxy.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("KeyProxy %s returned %d (%s), want 502 refusal", path, rec.Code, rec.Body.String())
		}
	}
}

// Plain unproxied claude and codex workers (even with a non-nil empty KeyProxy,
// as Serve creates in production) accept empty and bare model IDs ("", "opus",
// "gpt-5") without demanding a key-proxy route or provider/ prefix.
func TestUnproxiedClaudeAndCodexAcceptBareAndEmptyModels(t *testing.T) {
	t.Parallel()
	proxy := NewKeyProxy(nil, nil) // non-nil KeyProxy with zero routes, matching Serve when --providers is empty
	for _, tc := range []struct {
		harness string
		h       Harness
		models  []string
	}{
		{"claude", &ClaudeHarness{}, []string{"", "opus", "anthropic/opus"}},
		{"codex", &CodexHarness{}, []string{"", "gpt-5", "openai/gpt-5"}},
	} {
		r := probeRunner_internal(t, &probeSprint{}, &fakeWall{}, nil, proxy)
		r.DefaultHarness = tc.h
		for _, m := range tc.models {
			got, err := r.chooseModel_internal(tc.harness, TakeInfo{Tier: "pro", Model: m})
			if err != nil || got != m {
				t.Fatalf("unproxied %s chooseModel(%q) = (%q, %v), want (%q, nil)", tc.harness, m, got, err, m)
			}
		}
	}
}

// Blocking #2: --harness claude is proxied only with Anthropic-compatible
// routes (such as inky), refuses OpenAI-only routes (deepseek, inception,
// openrouter, :openai) with a 1-line fix, and stays open-net when --providers
// is empty or when the worker runs --harness opencode.
func TestClaudeHarnessProxyConfigAndOpenAIOnlyRefusal(t *testing.T) {
	t.Parallel()
	tokEnv := func(k string) (string, bool) {
		return "ghp_fixture", k == "GH_GATE_TOKEN"
	}
	for _, bad := range []string{
		"deepseek=https://api.deepseek.example@DEEPSEEK_API_KEY",
		"inception=https://api.inception.example@INCEPTION_API_KEY",
		"openrouter=https://openrouter.example/api@OPENROUTER_API_KEY",
		"custom=http://127.0.0.1:8096@CUSTOM_KEY:openai",
	} {
		err := (Config{Row: "b1", Harness: "claude", Providers: bad, LookupEnv: tokEnv}).Check()
		if err == nil || !strings.Contains(err.Error(), "cannot route through OpenAI-only provider") || strings.Contains(err.Error(), "\n") {
			t.Fatalf("Check(--harness claude --providers %s) = %v, want 1-line OpenAI-only refusal", bad, err)
		}
	}

	// --harness opencode with OpenAI routes succeeds; --harness claude with no --providers keeps open network.
	if err := (Config{Row: "b1", Harness: "opencode", Providers: "deepseek=https://api.deepseek.example@DEEPSEEK_API_KEY", LookupEnv: tokEnv}).Check(); err != nil {
		t.Fatalf("opencode with deepseek refused: %v", err)
	}
	openClaude := &ClaudeHarness{}
	if err := configureHarnessProxy_internal(openClaude, nil); err != nil || openClaude.Proxied || !openClaude.Needs().NetOpen || openClaude.Needs().Provider != "anthropic" {
		t.Fatalf("claude without --providers altered open-net Needs: proxied=%v needs=%+v err=%v", openClaude.Proxied, openClaude.Needs(), err)
	}

	// --harness claude with inky (or explicit :anthropic) enables Proxied.
	if err := (Config{Row: "macbook", Harness: "claude", Providers: "inky=http://127.0.0.1:8096@INKY_SERVE_KEY", LookupEnv: tokEnv}).Check(); err != nil {
		t.Fatalf("claude with inky refused: %v", err)
	}
	ps, err := ParseProviders("inky=http://127.0.0.1:8096@INKY_SERVE_KEY:anthropic")
	if err != nil {
		t.Fatal(err)
	}
	proxiedClaude := &ClaudeHarness{}
	if err := configureHarnessProxy_internal(proxiedClaude, ps); err != nil || !proxiedClaude.Proxied {
		t.Fatalf("configureHarnessProxy_internal(inky) = %v, proxied=%v", err, proxiedClaude.Proxied)
	}
}

// SanitizeJobWallEnv pins ANTHROPIC_BASE_URL to the exact j.ProxyAddr host:port
// (refusing other loopback ports and unproxied SanitizeWallEnv calls), refuses
// non-placeholder ANTHROPIC_AUTH_TOKEN, strips ANTHROPIC_API_KEY, and refuses
// unsafe model names or /tmp.
func TestSanitizeWallEnvRefusesUntrustedAnthropicVars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := "ANTHROPIC_BASE_URL=http://127.0.0.1:7000/inky"
	if got := SanitizeJobWallEnv([]string{good}, dir, "127.0.0.1:7000"); !slices.Contains(got, good) {
		t.Fatalf("exact proxyAddr rejected valid %q: %v", good, got)
	}
	if got := SanitizeWallEnv([]string{good}, dir); slices.Contains(got, good) {
		t.Fatalf("unproxied SanitizeWallEnv admitted %q without proxyAddr: %v", good, got)
	}
	for _, bad := range []string{
		"ANTHROPIC_BASE_URL=https://api.anthropic.com",
		"ANTHROPIC_BASE_URL=http://10.0.0.1:7000/inky",
		"ANTHROPIC_BASE_URL=http://127.0.0.1:7001/inky",
		"ANTHROPIC_BASE_URL=http://localhost:7000/inky",
		"ANTHROPIC_BASE_URL=http://127.0.0.1:7000/inky/v1",
		"ANTHROPIC_BASE_URL=http://127.0.0.1:7000/..",
		"ANTHROPIC_BASE_URL=http://user@127.0.0.1:7000/inky",
		"ANTHROPIC_AUTH_TOKEN=sk-ant-real-secret",
		"ANTHROPIC_AUTH_TOKEN=not-the-placeholder",
		"ANTHROPIC_API_KEY=" + ProxyKey("p1.w1"),
		"ANTHROPIC_MODEL=inkling 2",
		"ANTHROPIC_SMALL_FAST_MODEL=../bad",
		"CLAUDE_CODE_TMPDIR=/tmp",
		"DISABLE_AUTOUPDATER=0",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=0",
	} {
		if got := SanitizeJobWallEnv([]string{bad}, dir, "127.0.0.1:7000"); slices.Contains(got, bad) {
			t.Errorf("SanitizeJobWallEnv admitted untrusted %q: %v", bad, got)
		}
	}
}

// KeyProxy forwards 4 concurrent Claude /inky/v1/messages lanes to
// <upstream>/v1/messages with Authorization: Bearer <INKY_SERVE_KEY>, strips
// trailing /v1 on --providers URLs, and passes clean over-cap HTTP 400 JSON
// refusals through without panic, crash, or truncation.
func TestKeyProxyForwardsInkyMessagesAcrossFourLanesAndHandlesOverCap(t *testing.T) {
	t.Parallel()
	ps, err := ParseProviders("inky=http://127.0.0.1:8096/v1@INKY_SERVE_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].Upstream != "http://127.0.0.1:8096" {
		t.Fatalf("ParseProviders = %+v", ps)
	}
	p := NewKeyProxy(ps, func(k string) (string, bool) {
		return "inky-bearer-fixture", k == "INKY_SERVE_KEY"
	})

	const lanes = 4
	var (
		mu       sync.Mutex
		inFlight int
		allIn    = make(chan struct{})
		seen     = map[string]string{}
	)
	p.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, rerr := io.ReadAll(req.Body)
		if rerr != nil {
			return nil, rerr
		}
		if strings.Contains(string(body), `"over_cap":true`) {
			errJSON := `{"error":{"message":"context full: 62000 prompt tokens and 5536 to generate, 65536 per lane"}}`
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(strings.NewReader(errJSON)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}
		mu.Lock()
		seen[string(body)] = req.URL.String() + "|" + req.Header.Get("Authorization") + "|" + req.Header.Get("X-Api-Key")
		inFlight++
		if inFlight == lanes {
			close(allIn)
		}
		mu.Unlock()
		select {
		case <-allIn:
		case <-time.After(5 * time.Second): // unitlint: fail-fast guard so a broken lane fails instead of hanging the test
			return nil, errors.New("timed out waiting for all 4 lanes to enter RoundTrip concurrently")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","model":"inkling-2-mini"}`)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	})

	consumers := []string{"c1.w1", "c2.w1", "c3.w1", "c4.w1"}
	var wg sync.WaitGroup
	codes := make([]int, lanes)
	for i, c := range consumers {
		wg.Go(func() {
			base := ProxyRouteURL("127.0.0.1:7000", "inky")
			req := httptest.NewRequest(http.MethodPost, base+"/v1/messages", strings.NewReader(`{"lane":"`+c+`"}`))
			req.Header.Set("Authorization", "Bearer "+ProxyKey(c))
			req.Header.Set("X-Api-Key", ProxyKey(c))
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)
			codes[i] = rec.Code
		})
	}
	wg.Wait()

	for i, c := range consumers {
		if codes[i] != http.StatusOK {
			t.Fatalf("lane %s status = %d", c, codes[i])
		}
		want := "http://127.0.0.1:8096/v1/messages|Bearer inky-bearer-fixture|"
		if got := seen[`{"lane":"`+c+`"}`]; got != want {
			t.Fatalf("lane %s forwarded %q, want %q", c, got, want)
		}
	}

	// Over-cap refusal returns clean HTTP 400 JSON with zero secret exposure.
	overReq := httptest.NewRequest(http.MethodPost, ProxyRouteURL("127.0.0.1:7000", "inky")+"/v1/messages", strings.NewReader(`{"over_cap":true}`))
	overReq.Header.Set("Authorization", "Bearer "+ProxyKey("c1.w1"))
	overRec := httptest.NewRecorder()
	p.ServeHTTP(overRec, overReq)
	if overRec.Code != http.StatusBadRequest || !strings.Contains(overRec.Body.String(), `"context full: 62000 prompt tokens and 5536 to generate, 65536 per lane"`) {
		t.Fatalf("over-cap response: code=%d body=%q", overRec.Code, overRec.Body.String())
	}
	if strings.Contains(overRec.Body.String(), "inky-bearer-fixture") {
		t.Fatalf("over-cap response leaked bearer key: %q", overRec.Body.String())
	}
}

// A MacBook worker configured with --harness claude and --providers inky=...
// proves inky/inkling-2-mini on flash and pro, advertises only flash,pro (never
// heavy/frontier), faults deals for unserved providers, and refuses probe when
// the inky route is absent.
func TestMacBookInkyWorkerServesOnlyInkyFlashAndPro(t *testing.T) {
	t.Parallel()
	ps, err := ParseProviders("inky=http://127.0.0.1:8096@INKY_SERVE_KEY")
	if err != nil {
		t.Fatal(err)
	}
	proxy := NewKeyProxy(ps, envOf_internal(map[string]string{"INKY_SERVE_KEY": "inky-secret-fixture"}))
	sp := &probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{Row: "macbook", TierModels: map[string][]string{
		"flash":    {"inky/inkling-2-mini"},
		"pro":      {"inky/inkling-2-mini"},
		"heavy":    {"deepseek/deepseek-flash"},
		"frontier": {"deepseek/deepseek-v4-pro"},
	}}}}
	var probed []string
	wall := &fakeWall{runFn: func(_ int, j Job, argv, env []string) (WallRunResult, error) {
		probed = append(probed, strings.Join(argv[:7], " ")+" | "+strings.Join(env, " "))
		if werr := os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600); werr != nil {
			return WallRunResult{}, werr
		}
		return WallRunResult{Elapsed: 2 * time.Second}, nil
	}}
	r := probeRunner_internal(t, sp, wall, nil, proxy)
	r.DefaultHarness = &ClaudeHarness{Proxied: true}
	if err := r.ProbeRow(context.Background(), "macbook", "claude"); err != nil {
		t.Fatalf("ProbeRow(macbook, claude): %v", err)
	}
	if len(probed) != 1 || !strings.Contains(probed[0], "claude --print --dangerously-skip-permissions --disallowedTools Task --model inkling-2-mini") || !strings.Contains(probed[0], "ANTHROPIC_BASE_URL=http://127.0.0.1:7000/inky") {
		t.Fatalf("probed = %v", probed)
	}
	if r.tiers["macbook"] != "flash,pro" {
		t.Fatalf("advertised tiers = %q, want flash,pro", r.tiers["macbook"])
	}
	if m, err := r.chooseModel_internal("claude", TakeInfo{Tier: "pro", Model: "inky/inkling-2-mini"}); err != nil || m != "inky/inkling-2-mini" {
		t.Fatalf("chooseModel(inky/inkling-2-mini) = %q, %v", m, err)
	}
	if _, err := r.chooseModel_internal("claude", TakeInfo{Tier: "heavy", Model: "deepseek/deepseek-flash"}); err == nil {
		t.Fatal("chooseModel accepted unproved deepseek/deepseek-flash on inky-only macbook worker")
	}

	// Without an inky route on KeyProxy, ProbeRow refuses naming the nova-config provider record that adds it.
	noInky := probeRunner_internal(t, sp, &fakeWall{}, nil, NewKeyProxy(nil, nil))
	noInky.DefaultHarness = &ClaudeHarness{Proxied: true}
	err = noInky.ProbeRow(context.Background(), "macbook", "claude")
	if err == nil || !strings.Contains(err.Error(), "no key-proxy route for inky (nova-config provider add inky --url <url> --api openai|anthropic --key <SECRET_NAME>)") {
		t.Fatalf("want missing inky route refusal, got %v", err)
	}
}

// A proxied claude job gets CLAUDE_CODE_MAX_CONTEXT_TOKENS and
// CLAUDE_CODE_MAX_OUTPUT_TOKENS from the worker's environment when each is a
// positive integer, and neither otherwise; an unproxied claude job never does.
func TestProxiedClaudeContextWindowEnv(t *testing.T) { // unitlint: serial, it sets the process environment (t.Setenv): Command reads os.Getenv
	keys := []string{"CLAUDE_CODE_MAX_CONTEXT_TOKENS", "CLAUDE_CODE_MAX_OUTPUT_TOKENS"}
	dir := t.TempDir()
	proxied := Job{Model: "inky/inkling-2-mini", Consumer: "p1.w1", Row: "macbook", JobDir: dir, ProxyAddr: "127.0.0.1:7000", Routes: []string{"inky"}}
	has := func(env []string, k string) (string, bool) {
		for _, kv := range env {
			if name, v, _ := strings.Cut(kv, "="); name == k {
				return v, true
			}
		}
		return "", false
	}
	h := &ClaudeHarness{Proxied: true}

	for _, k := range keys {
		t.Setenv(k, "")
	}
	_, env := h.Command(proxied)
	for _, k := range keys {
		if v, ok := has(env, k); ok {
			t.Errorf("unset %s reached the job as %q", k, v)
		}
	}

	t.Setenv(keys[0], "131072")
	t.Setenv(keys[1], "8192")
	_, env = h.Command(proxied)
	for k, want := range map[string]string{keys[0]: "131072", keys[1]: "8192"} {
		if v, ok := has(env, k); !ok || v != want {
			t.Errorf("%s = %q (present %v), want %q: %v", k, v, ok, want, env)
		}
	}
	_, env = (&ClaudeHarness{}).Command(Job{Model: "claude-opus", Consumer: "p1.w1", Row: "macbook", JobDir: dir})
	for _, k := range keys {
		if v, ok := has(env, k); ok {
			t.Errorf("unproxied claude got %s=%q", k, v)
		}
	}

	for _, bad := range []string{"lots", "0", "-1", "1.5"} {
		for _, k := range keys {
			t.Setenv(k, bad)
		}
		_, env = h.Command(proxied)
		for _, k := range keys {
			if v, ok := has(env, k); ok {
				t.Errorf("%s=%q reached the job as %q", k, bad, v)
			}
		}
	}
}

// The wall admits the two window variables only as positive integers.
func TestSanitizeWallEnvAdmitsOnlyPositiveContextWindow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, good := range []string{"CLAUDE_CODE_MAX_CONTEXT_TOKENS=131072", "CLAUDE_CODE_MAX_OUTPUT_TOKENS=8192"} {
		if got := SanitizeJobWallEnv([]string{good}, dir, "127.0.0.1:7000"); !slices.Contains(got, good) {
			t.Errorf("SanitizeJobWallEnv refused %q: %v", good, got)
		}
	}
	for _, bad := range []string{
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS=lots",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS=0",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS=-131072",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS=",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS=0",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS=-1",
		"CLAUDE_CODE_SOMETHING_ELSE=8192",
	} {
		k, _, _ := strings.Cut(bad, "=")
		for _, kv := range SanitizeJobWallEnv([]string{bad}, dir, "127.0.0.1:7000") {
			if strings.HasPrefix(kv, k+"=") {
				t.Errorf("SanitizeJobWallEnv admitted %q as %q", bad, kv)
			}
		}
	}
}
