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

// SanitizeWallEnv refuses non-loopback ANTHROPIC_BASE_URL, non-placeholder
// ANTHROPIC_AUTH_TOKEN, any ANTHROPIC_API_KEY, unsafe model names, and /tmp.
func TestSanitizeWallEnvRefusesUntrustedAnthropicVars(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, bad := range []string{
		"ANTHROPIC_BASE_URL=https://api.anthropic.com",
		"ANTHROPIC_BASE_URL=http://10.0.0.1:7000/inky",
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
		if got := SanitizeWallEnv([]string{bad}, dir); slices.Contains(got, bad) {
			t.Errorf("SanitizeWallEnv admitted untrusted %q: %v", bad, got)
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
		<-allIn
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

	// Without an inky route on KeyProxy, ProbeRow refuses naming --providers inky=<url>@<KEY>.
	noInky := probeRunner_internal(t, sp, &fakeWall{}, nil, NewKeyProxy(nil, nil))
	noInky.DefaultHarness = &ClaudeHarness{Proxied: true}
	err = noInky.ProbeRow(context.Background(), "macbook", "claude")
	if err == nil || !strings.Contains(err.Error(), "no key-proxy route for inky (--providers inky=<url>@<KEY>)") {
		t.Fatalf("want missing inky route refusal, got %v", err)
	}
}
