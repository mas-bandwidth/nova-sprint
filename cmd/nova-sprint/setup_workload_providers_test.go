package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

// A worker service opens each enabled provider record's key, sorted and
// once; no enabled provider record refuses, naming nova-config provider add.
func TestWorkerProviderKeysFromTheProviderRecords(t *testing.T) {
	t.Parallel()
	keys, err := workerProviderKeys([]verbs.FleetProvider{
		{Name: "openrouter", Key: "OPENROUTER_API_KEY", Enabled: true}, {Name: "deepseek", Key: "DEEPSEEK_API_KEY", Enabled: true},
		{Name: "deepseek-beta", Key: "DEEPSEEK_API_KEY", Enabled: true}, {Name: "ollama", Enabled: true}, {Name: "off", Key: "OFF_API_KEY"},
	})
	if err != nil || !slices.Equal(keys, []string{"DEEPSEEK_API_KEY", "OPENROUTER_API_KEY"}) {
		t.Fatalf("got %v %v", keys, err)
	}
	for _, ps := range [][]verbs.FleetProvider{nil, {{Name: "off", Key: "OFF_API_KEY"}}} {
		if _, err := workerProviderKeys(ps); err == nil || !strings.Contains(err.Error(), "no enabled provider record") || !strings.Contains(err.Error(), "nova-config provider add <name> --url") {
			t.Fatalf("%v: got %v", ps, err)
		}
	}
}
