package main

import (
	"strings"
	"testing"

	config "github.com/mas-bandwidth/nova-tools/v2/modules/config"
)

// The worker's records: enabled providers as routes, enabled models' harness
// (opencode when the record names none); a provider record that cannot be a
// route refuses the start, naming the nova-config verb.
func TestWorkerRecordsOf(t *testing.T) {
	t.Parallel()
	rec, err := workerRecordsOf([]config.Provider{
		{Name: "deepseek", URL: "https://api.deepseek.com/v1", API: "openai", Key: "DEEPSEEK_API_KEY", Enabled: true},
		{Name: "off", URL: "https://off.example", API: "openai", Enabled: false},
	}, []config.Model{{Name: "deepseek/v4", Enabled: true}, {Name: "ollama/q", Harness: "claude", Enabled: true}, {Name: "deepseek/old", Harness: "codex"}})
	if err != nil || len(rec.Providers) != 1 || rec.Providers[0].Upstream != "https://api.deepseek.com" || rec.Providers[0].Key != "DEEPSEEK_API_KEY" {
		t.Fatalf("got %+v %v", rec, err)
	}
	if len(rec.Harnesses) != 2 || rec.Harnesses["deepseek/v4"] != "opencode" || rec.Harnesses["ollama/q"] != "claude" {
		t.Fatalf("harnesses %v", rec.Harnesses)
	}
	_, err = workerRecordsOf([]config.Provider{{Name: "p", URL: "https://p.example", API: "grpc", Enabled: true}}, nil)
	if err == nil || !strings.Contains(err.Error(), "nova-config provider set p --api openai|anthropic") {
		t.Fatalf("got %v", err)
	}
}
