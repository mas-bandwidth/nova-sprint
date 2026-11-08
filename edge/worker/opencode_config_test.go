package worker

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Unit tier: no process, no socket, no clock.

type opencodeConfig_test struct {
	Provider map[string]struct {
		NPM     string `json:"npm"`
		Options struct {
			BaseURL string `json:"baseURL"`
			APIKey  string `json:"apiKey"`
		} `json:"options"`
		Models map[string]any `json:"models"`
	} `json:"provider"`
}

func opencodeConfigOf_internal(t *testing.T, env []string) opencodeConfig_test {
	t.Helper()
	var cfg opencodeConfig_test
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "OPENCODE_CONFIG_CONTENT="); ok {
			if err := json.Unmarshal([]byte(v), &cfg); err != nil {
				t.Fatalf("OPENCODE_CONFIG_CONTENT is not JSON: %v: %s", err, v)
			}
			return cfg
		}
	}
	t.Fatalf("no OPENCODE_CONFIG_CONTENT in the opencode env: %v", env)
	return cfg
}

// v2.0.11: opencode is given a config declaring the job's model under an
// OpenAI-compatible provider whose base URL is the key proxy's route for
// that provider and whose key is the placeholder, so `opencode run --model
// deepseek/deepseek-flash` goes through the proxy (it knew only its own
// free models before, and every bench's probe was refused).
func TestOpenCodeConfigDeclaresTheJobsModelThroughTheProxy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, env := (&OpenCodeHarness{}).Command(Job{Consumer: "c1", JobDir: dir, ProxyAddr: "127.0.0.1:7000", Model: "deepseek/deepseek-flash"})
	cfg := opencodeConfigOf_internal(t, env)
	p, ok := cfg.Provider["deepseek"]
	if !ok || len(cfg.Provider) != 1 {
		t.Fatalf("want one provider, deepseek: %+v", cfg)
	}
	if p.NPM != "@ai-sdk/openai-compatible" || p.Options.BaseURL != "http://127.0.0.1:7000/deepseek/v1" || p.Options.APIKey != ProxyPlaceholderKey {
		t.Fatalf("deepseek is not the proxy's route with the placeholder key: %+v", p)
	}
	if _, ok := p.Models["deepseek-flash"]; !ok || len(p.Models) != 1 {
		t.Fatalf("want the job's model declared: %+v", p.Models)
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "OPENAI_") {
			t.Fatalf("the OPENAI_* wiring opencode ignores is gone: %q", kv)
		}
	}
	if (&OpenCodeHarness{}).Needs().Limits == "" || strings.Contains((&OpenCodeHarness{}).Needs().Limits, "not built") {
		t.Fatalf("Limits still says the provider config is not built: %q", (&OpenCodeHarness{}).Needs().Limits)
	}
	if _, env := (&OpenCodeHarness{}).Command(Job{Consumer: "c1", JobDir: dir, Model: "deepseek/deepseek-flash"}); slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "OPENCODE_CONFIG_CONTENT=") }) {
		t.Fatalf("no proxy, no config: %v", env)
	}
}

// The wall keeps the config only when every provider's key in it is the
// placeholder: a real key never crosses into the wall this way.
func TestSanitizeWallEnvKeepsOnlyAPlaceholderOpenCodeConfig(t *testing.T) {
	t.Parallel()
	good := OpenCodeConfig("127.0.0.1:7000", []string{"deepseek/deepseek-flash", "inception/mercury-3"})
	bad := strings.ReplaceAll(good, ProxyPlaceholderKey, "sk-real")
	for _, c := range []struct {
		v    string
		keep bool
	}{{good, true}, {bad, false}, {"not json", false}, {`{"provider":{"x":{"options":{}}}}`, false},
		{strings.Replace(good, `"apiKey":"`, `"headers":{"Authorization":"Bearer sk-real"},"apiKey":"`, 1), false}} {
		env := SanitizeWallEnv([]string{"OPENCODE_CONFIG_CONTENT=" + c.v}, t.TempDir())
		if kept := slices.Contains(env, "OPENCODE_CONFIG_CONTENT="+c.v); kept != c.keep {
			t.Errorf("config %q: kept %v, want %v", c.v, kept, c.keep)
		}
	}
}

// The probe lists models under the same config, declaring every tier
// candidate whose provider has a key-proxy route (and only those), so
// `opencode models` lists the policy's models.
func TestProbeListsModelsUnderTheProxyConfig(t *testing.T) {
	t.Parallel()
	sp := &probeSprint{cr2Sprint: cr2Sprint{next: NextResponse{Row: "b1", TierModels: map[string][]string{
		"flash": {"deepseek/deepseek-flash"}, "pro": {"inception/mercury-3", "anthropic/fable"},
	}}}}
	fx := listingExec_internal("deepseek/deepseek-flash", "inception/mercury-3")
	r := probeRunner_internal(t, sp, &fakeWall{}, fx, threeProviders_internal(t, allKeys_internal))
	_ = r.ProbeRow(context.Background(), "b1", "opencode")
	var listing []string
	for _, c := range fx.Calls() {
		if slices.Equal(c.Cmd.Argv, []string{"opencode", "models"}) {
			listing = c.Cmd.Env
		}
	}
	if listing == nil {
		t.Fatal("opencode models was not run")
	}
	cfg := opencodeConfigOf_internal(t, listing)
	if _, ok := cfg.Provider["deepseek"].Models["deepseek-flash"]; !ok {
		t.Fatalf("deepseek-flash not declared: %+v", cfg)
	}
	if _, ok := cfg.Provider["inception"].Models["mercury-3"]; !ok {
		t.Fatalf("mercury-3 not declared: %+v", cfg)
	}
	if _, ok := cfg.Provider["anthropic"]; ok {
		t.Fatalf("a provider with no route is not declared: %+v", cfg)
	}
}

// Functional: the real `opencode models`, given the env OpenCodeHarness.Command
// builds (the probe's listing), lists the declared provider/models. No
// provider is called: listing reads the config. Skipped without opencode on
// PATH, and for opencode 2.x, which needs --standalone (Needs.Limits).
func TestFunctionalOpenCodeListsTheConfiguredModels(t *testing.T) {
	t.Parallel()
	bin, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("opencode is not on PATH")
	}
	ver, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatalf("opencode --version: %v", err)
	}
	if v := strings.TrimPrefix(strings.TrimSpace(string(ver)), "opencode v"); !strings.HasPrefix(v, "1.") {
		t.Skipf("%s: the adapter runs opencode 1.x (2.x needs --standalone)", strings.TrimSpace(string(ver)))
	}
	dir := t.TempDir()
	for _, sub := range []string{"home", "tmp", "data"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"deepseek/nova-test-flash", "inception/nova-test-3"}
	_, env := (&OpenCodeHarness{}).Command(Job{JobDir: dir, Consumer: "b1.models", ProxyAddr: "127.0.0.1:9", Models: want})
	cmd := exec.Command(bin, "models")
	cmd.Env, cmd.Dir = env, dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("opencode models: %v: %s", err, out)
	}
	listed := strings.Fields(string(out))
	for _, m := range want {
		if !slices.Contains(listed, m) {
			t.Errorf("opencode models does not list %s: %s", m, out)
		}
	}
}
