package policy

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/roster"
)

// BenchProviders are the providers the bench workers (harness nova-worker)
// reach on API keys, billed per token: the take receipt's tier map and the
// worker's opencode model ids (verbs/take.go takeModel_local,
// edge/worker/harness.go; LOCAL DOGFOOD: the policy's tier rows are to
// answer this, defect 57).
var BenchProviders = []string{"deepseek", "inception"}

// apiProviders are the providers whose runs are billed at API rate: the
// benches', and the provider of any roster row on an API harness (api, the
// catalog's API-rate adapter, or nova-worker) whose model names one.
func apiProviders(ro roster.Roster) map[string]bool {
	out := map[string]bool{}
	for _, p := range BenchProviders {
		out[p] = true
	}
	for _, k := range ro.Kin {
		if k.Harness != "api" && k.Harness != roster.WorkerHarness {
			continue
		}
		if p, _, ok := strings.Cut(k.Model, "/"); ok && p != "" {
			out[p] = true
		}
	}
	return out
}

// PriceWarnings are the catalog models billed at API rate that prices()
// leaves out (the owner, 2026-11-07: find defects at creation): each run of
// one is recorded unpriced=no-price, never $0, until the policy prices it.
// A price is found as the engine finds it: under provider/model or the
// model alone.
func PriceWarnings(a Answers, ro roster.Roster) []string {
	api := apiProviders(ro)
	var out []string
	for _, provider := range slices.Sorted(maps.Keys(a.Catalog.Models)) {
		if !api[provider] {
			continue
		}
		seen := map[string]bool{}
		for _, m := range a.Catalog.Models[provider] {
			if seen[m.Name] {
				continue
			}
			seen[m.Name] = true
			_, whole := a.Prices[provider+"/"+m.Name]
			_, bare := a.Prices[m.Name]
			if !whole && !bare {
				out = append(out, fmt.Sprintf("POLICY WARNING prices(): %s/%s (tier %s) is billed at API rate and has no price: its runs will be recorded unpriced=no-price; add %q: {\"in\": <micro-USD per million>, \"out\": <...>}", provider, m.Name, m.Tier, m.Name))
			}
		}
	}
	return out
}
