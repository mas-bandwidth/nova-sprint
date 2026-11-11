package main

import (
	"slices"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/worker"
)

// The worker's key-proxy routes include OpenCode Zen and OpenRouter, each
// upstream given without /v1 and each key opened from the bench's seat.
func TestWorkerProvidersRouteOpenCodeAndOpenRouter(t *testing.T) {
	t.Parallel()
	ps, err := worker.ParseProviders(workerProviders)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]worker.Provider{
		"opencode":   {Name: "opencode", Upstream: "https://opencode.ai/inference/openai", Host: "opencode.ai", Key: "OPENCODE_API_KEY"},
		"openrouter": {Name: "openrouter", Upstream: "https://openrouter.ai/api", Host: "openrouter.ai", Key: "OPENROUTER_API_KEY"},
	}
	for name, w := range want {
		i := slices.IndexFunc(ps, func(p worker.Provider) bool { return p.Name == name })
		if i < 0 || ps[i] != w {
			t.Errorf("route %s: got %+v, want %+v", name, ps, w)
		}
	}
	for _, p := range ps {
		if !slices.Contains(workerProviderKeys(), p.Key) {
			t.Errorf("route %s: key %s is not in workerProviderKeys", p.Name, p.Key)
		}
	}
}
