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

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// A provider record is a key-proxy route: its url less a trailing /v1 (the
// harness's path carries it), its api, and the name of its key; a record
// that cannot be a route is refused naming the nova-config verb that fixes it.
func TestProviderRecordIsARoute(t *testing.T) {
	t.Parallel()
	p, err := ProviderRecord("ollama", "http://127.0.0.1:11434/v1", "anthropic", "")
	if err != nil || p.Upstream != "http://127.0.0.1:11434" || p.Host != "127.0.0.1" || p.API != "anthropic" || p.Key != "" {
		t.Fatalf("got %+v %v", p, err)
	}
	if p, err = ProviderRecord("deepseek", "https://api.deepseek.com", "", "DEEPSEEK_API_KEY"); err != nil || p.API != "openai" || p.Upstream != "https://api.deepseek.com" {
		t.Fatalf("an unset api is openai: got %+v %v", p, err)
	}
	for _, tc := range []struct{ name, url, api, key, want string }{
		{"Bad", "https://x.example", "openai", "K", "nova-config provider add <name>"},
		{"p", "ftp://x.example", "openai", "K", "nova-config provider set p --url"},
		{"p", "https://x.example", "grpc", "K", "nova-config provider set p --api openai|anthropic"},
		{"p", "https://x.example", "openai", "lower", "nova-config provider set p --key"},
	} {
		if _, err := ProviderRecord(tc.name, tc.url, tc.api, tc.key); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: got %v, want %q", tc, err, tc.want)
		}
	}
}

// The proxy injects a route's key as its api wants it: Authorization: Bearer
// for openai, x-api-key for anthropic; a keyless route forwards with none.
// Whatever key header the harness sent is dropped, and the job is read from
// the placeholder in either header.
func TestKeyProxyInjectsTheKeyByAPI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		api, key     string
		send         [2]string
		auth, apiKey string
	}{
		{"openai", "sk-o", [2]string{"Authorization", "Bearer " + ProxyKey("p1.w1")}, "Bearer sk-o", ""},
		{"anthropic", "sk-a", [2]string{"Authorization", "Bearer " + ProxyKey("p1.w1")}, "", "sk-a"},
		{"anthropic", "sk-a", [2]string{"X-Api-Key", ProxyKey("p1.w1")}, "", "sk-a"},
		{"anthropic", "", [2]string{"Authorization", "Bearer " + ProxyKey("p1.w1")}, "", ""},
	} {
		var auth, apiKey string
		p := NewKeyProxy([]Provider{{Name: "up", Upstream: "https://up.example", Host: "up.example", Key: keyName_internalFor(tc.key), API: tc.api}}, func(n string) (string, bool) { return tc.key, n == "UP_KEY" })
		p.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			auth, apiKey = r.Header.Get("Authorization"), r.Header.Get("X-Api-Key")
			return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader(`{}`)), Header: http.Header{}}, nil
		})
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:1/up/v1/messages", strings.NewReader(`{}`))
		req.Header.Set(tc.send[0], tc.send[1])
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusTooManyRequests || auth != tc.auth || apiKey != tc.apiKey {
			t.Errorf("%+v: code %d auth %q x-api-key %q", tc, rec.Code, auth, apiKey)
		}
		if got := p.ConsumeStatus("p1.w1"); got != http.StatusTooManyRequests {
			t.Errorf("%+v: the job's status is %d, want 429", tc, got)
		}
	}
}

// keyName_internalFor is the route's key name: none for a keyless route.
func keyName_internalFor(key string) string {
	if key == "" {
		return ""
	}
	return "UP_KEY"
}

// ProxyRouteURL is the route's base without /v1: an Anthropic client
// appends /v1/messages itself.
func TestProxyRouteURL(t *testing.T) {
	t.Parallel()
	if got := ProxyRouteURL("127.0.0.1:4000", "ollama"); got != "http://127.0.0.1:4000/ollama" {
		t.Fatalf("got %q", got)
	}
}

// A proxied claude runs behind one loopback --net-allow with the proxy's
// route, the placeholder key, the model id without its provider and its
// directories inside the job; SanitizeWallEnv admits exactly those values.
func TestProxiedClaudeCommand(t *testing.T) {
	t.Parallel()
	h := &ClaudeHarness{Proxied: true}
	j := Job{Model: "ollama/qwen3:8b", Consumer: "p1.w1", Row: "amd-1", JobDir: "/w/j", ProxyAddr: "127.0.0.1:4000", Routes: []string{"ollama"}}
	argv, env := h.Command(j)
	if got := strings.Join(argv[:7], " "); got != "claude --print --dangerously-skip-permissions --disallowedTools Task --model qwen3:8b" {
		t.Fatalf("argv %q", got)
	}
	for _, kv := range []string{"ANTHROPIC_BASE_URL=http://127.0.0.1:4000/ollama", "ANTHROPIC_AUTH_TOKEN=" + ProxyKey("p1.w1"), "CLAUDE_CONFIG_DIR=/w/j/home/.claude",
		"CLAUDE_CODE_TMPDIR=/w/j/tmp", "DISABLE_AUTOUPDATER=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1"} {
		if !slices.Contains(env, kv) {
			t.Errorf("env lacks %s: %v", kv, env)
		}
	}
	j.Needs = h.Needs()
	flags := strings.Join(sandboxFlags_internal(WallConfig{NetDeny: true}, j, env), " ")
	if !strings.Contains(flags, "--net-deny --net-allow 127.0.0.1:4000") || strings.Count(flags, "--net-allow") != 1 {
		t.Fatalf("wall flags %q", flags)
	}
	for _, bad := range []string{"ANTHROPIC_BASE_URL=https://api.anthropic.com", "ANTHROPIC_AUTH_TOKEN=sk-ant-real", "CLAUDE_CODE_TMPDIR=/tmp", "DISABLE_AUTOUPDATER=0", "ANTHROPIC_API_KEY=" + ProxyKey("p1.w1")} {
		if got := SanitizeWallEnv([]string{bad}, "/w/j"); slices.Contains(got, bad) {
			t.Errorf("%s was admitted", bad)
		}
	}
	if _, env := (&ClaudeHarness{}).Command(j); slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "ANTHROPIC_") }) {
		t.Fatalf("claude on its own login is given the proxy: %v", env)
	}
}

// The probe runs each tier's model with its model record's harness, the
// row's harness for a model without one, and the row advertises the tiers
// whose model was proved so: here opencode for deepseek, claude through the
// proxy's keyless anthropic route for a local model.
func TestProbeRunsEachModelWithItsRecordsHarness(t *testing.T) {
	t.Parallel()
	sp := &probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{Row: "b1", TierModels: map[string][]string{
		"flash": {"deepseek/deepseek-flash"}, "frontier": {"ollama/qwen3:8b"},
	}}}}
	var mu sync.Mutex
	ran := map[string]string{}
	wall := &fakeWall{runFn: func(_ int, j Job, argv, env []string) (WallRunResult, error) {
		mu.Lock()
		ran[argv[0]+" "+argv[slices.Index(argv, "--model")+1]] = strings.Join(env, " ")
		mu.Unlock()
		_ = os.WriteFile(filepath.Join(j.JobDir, "result.json"), []byte(`{"result":"ok"}`), 0o600)
		return WallRunResult{Elapsed: 2 * time.Second}, nil
	}}
	proxy := NewKeyProxy([]Provider{{Name: "deepseek", Upstream: "https://api.deepseek.example", Host: "api.deepseek.example", Key: "DEEPSEEK_API_KEY", API: "openai"},
		{Name: "ollama", Upstream: "http://127.0.0.1:11434", Host: "127.0.0.1", API: "anthropic"}}, envOf_internal(allKeys_internal))
	r := probeRunner_internal(t, sp, wall, listingExec_internal("deepseek/deepseek-flash"), proxy)
	r.Records = func(context.Context) (Records, error) {
		t.Fatal("the probe reads the records the worker started on")
		return Records{}, nil
	}
	r.ModelHarnesses = map[string]string{"ollama/qwen3:8b": "claude"}
	if err := r.ProbeRow(context.Background(), "b1", "opencode"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ran["opencode deepseek/deepseek-flash"]; !ok || len(ran) != 2 {
		t.Fatalf("ran %v", ran)
	}
	if env := ran["claude qwen3:8b"]; !strings.Contains(env, "ANTHROPIC_BASE_URL=http://127.0.0.1:7000/ollama") {
		t.Fatalf("claude ran without the proxy: %q", env)
	}
	if r.tiers["b1"] != "flash,frontier" {
		t.Fatalf("advertised %q", r.tiers["b1"])
	}
	if h, _ := r.harnessOf_internal("claude", "anthropic/opus"); h.Needs().NetOpen != true {
		t.Fatalf("claude for a provider no record names runs on its own login: %+v", h.Needs())
	}
}

// A job naming a model the worker has no record of has the records read
// again once, the proxy's routes with them; a model they know reads nothing.
func TestModelHarnessRereadsForAnUnseenModel(t *testing.T) {
	t.Parallel()
	reads := 0
	r := probeRunner_internal(t, &probeSprint{}, &fakeWall{}, edge.NewFakeExec(), NewKeyProxy(nil, nil))
	r.RecordRoutes, r.ModelHarnesses = true, map[string]string{"deepseek/v4": "opencode"}
	r.Records = func(context.Context) (Records, error) {
		reads++
		return Records{Providers: []Provider{{Name: "openai", Upstream: "https://api.openai.example", Host: "api.openai.example", API: "openai"}},
			Harnesses: map[string]string{"deepseek/v4": "opencode", "openai/gpt-6": "codex"}}, nil
	}
	ctx := context.Background()
	if got := r.modelHarness_internal(ctx, "deepseek/v4"); got != "opencode" || reads != 0 {
		t.Fatalf("got %q after %d reads", got, reads)
	}
	if got := r.modelHarness_internal(ctx, "openai/gpt-6"); got != "codex" || reads != 1 || !r.Proxy.HasRoute("openai") {
		t.Fatalf("got %q after %d reads, routes %v", got, reads, r.Proxy.RouteNames())
	}
	if h, err := r.harnessFor_internal("codex"); err != nil || h.Name() != "codex" {
		t.Fatalf("a record's harness is that harness, not the default: %v %v", h, err)
	}
}

// The worker starts on nova-config's records, --providers in place of
// their routes; records it cannot read refuse the start.
func TestWorkerStartsOnTheRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	recs := func(context.Context) (Records, error) {
		return Records{Providers: []Provider{{Name: "a", Upstream: "https://a.example", Host: "a.example"}}, Harnesses: map[string]string{"a/m": "claude"}}, nil
	}
	s, err := (Config{Records: recs}).records_internal(ctx)
	if err != nil || len(s.Providers) != 1 || s.Providers[0].Name != "a" || s.Harnesses["a/m"] != "claude" || s.from != "nova-config provider records" {
		t.Fatalf("got %+v %v", s, err)
	}
	s, err = (Config{Records: recs, Providers: "b=https://b.example@B_KEY"}).records_internal(ctx)
	if err != nil || len(s.Providers) != 1 || s.Providers[0].Name != "b" || s.Harnesses["a/m"] != "claude" || s.from != "--providers" {
		t.Fatalf("--providers names the routes: got %+v %v", s, err)
	}
	unread := func(context.Context) (Records, error) {
		return Records{}, errors.New("permission denied for table providers")
	}
	_, err = (Config{Records: unread}).records_internal(ctx)
	if err == nil || !strings.Contains(err.Error(), "worker refused: nova-config's provider and model records could not be read: permission denied for table providers") {
		t.Fatalf("got %v", err)
	}
	// With --providers the routes it needs are given: an unreadable read is a
	// warning, never a refusal.
	s, err = (Config{Records: unread, Providers: "b=https://b.example@B_KEY"}).records_internal(ctx)
	if err != nil || len(s.Providers) != 1 || s.Providers[0].Name != "b" || len(s.Skipped) != 1 || !strings.Contains(s.Skipped[0], "permission denied for table providers") {
		t.Fatalf("got %+v %v", s, err)
	}
	// A record skipped as bad is said at start, its fix with it.
	var lines []string
	warnSkipped_internal(func(l string) { lines = append(lines, l) }, []string{"provider record p skipped: bad api (nova-config provider set p --api openai|anthropic)"})
	if len(lines) != 1 || lines[0] != "WORKER WARNING provider record p skipped: bad api (nova-config provider set p --api openai|anthropic)" {
		t.Fatalf("lines %q", lines)
	}
}

// A model with no record is read for once: a successful read that does not
// name it is kept, so the next take does not open nova-config again.
func TestModelHarnessCachesAModelWithNoRecord(t *testing.T) {
	t.Parallel()
	reads := 0
	r := probeRunner_internal(t, &probeSprint{}, &fakeWall{}, edge.NewFakeExec(), NewKeyProxy(nil, nil))
	r.ModelHarnesses = map[string]string{"deepseek/v4": "opencode"}
	r.Records = func(context.Context) (Records, error) {
		reads++
		return Records{Harnesses: map[string]string{"deepseek/v4": "opencode"}}, nil
	}
	ctx := context.Background()
	for range 3 {
		if got := r.modelHarness_internal(ctx, "ollama/q"); got != "" {
			t.Fatalf("got %q", got)
		}
	}
	if reads != 1 {
		t.Fatalf("%d reads for one model with no record", reads)
	}
	if got := r.modelHarness_internal(ctx, "deepseek/v4"); got != "opencode" || reads != 1 {
		t.Fatalf("got %q after %d reads", got, reads)
	}
}

// SetRoutes replaces the routes a running proxy forwards on.
func TestKeyProxySetRoutes(t *testing.T) {
	t.Parallel()
	p := NewKeyProxy(nil, nil)
	p.SetRoutes([]Provider{{Name: "a", Upstream: "https://a.example", Host: "a.example", API: "openai"}}, nil)
	if !p.HasRoute("a") || strings.Join(p.RouteNames(), ",") != "a" || len(p.MissingKeys()) != 0 {
		t.Fatalf("routes %v missing %v", p.RouteNames(), p.MissingKeys())
	}
}
