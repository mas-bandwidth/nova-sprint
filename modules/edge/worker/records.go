package worker

import (
	"context"
	"maps"
	"os"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// Records are what nova-config holds for a worker: its enabled provider
// records as key-proxy routes (ProviderRecord) and each enabled model
// record's harness, by model name (provider/model). Skipped is one line per
// record (or read) left out, with the fix: one bad record never stops a
// worker whose routes are elsewhere.
type Records struct {
	Providers []Provider
	Harnesses map[string]string
	Skipped   []string
}

// warnSkipped_internal says each record left out, as a worker warning.
func warnSkipped_internal(warn func(string), skipped []string) {
	for _, line := range skipped {
		warn("WORKER WARNING " + edge.RedactN(line, edge.CommandMax))
	}
}

// modelHarness_internal is model's record's harness, "" for none. A model
// not among the records last read has them read again once (a model record
// added since the worker started), its providers then the proxy's routes;
// a read that fails is said and the last read stands. A read that names no
// record for model is kept as "" (and so is each such model before it), so
// a model with no record opens nova-config once, not on every take, until
// the worker restarts.
func (r *Runner) modelHarness_internal(ctx context.Context, model string) string {
	r.mu.Lock()
	name, seen := r.ModelHarnesses[model]
	r.mu.Unlock()
	if seen || model == "" || r.Records == nil {
		return name
	}
	rec, err := r.Records(ctx)
	if err != nil {
		r.report("re-reading nova-config's provider and model records for "+model, err)
		return ""
	}
	if r.Warn != nil {
		warnSkipped_internal(r.Warn, rec.Skipped)
	}
	if lookup := r.LookupEnv; r.RecordRoutes {
		if lookup == nil {
			lookup = os.LookupEnv
		}
		r.Proxy.SetRoutes(rec.Providers, lookup)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	harnesses := maps.Clone(rec.Harnesses)
	if harnesses == nil {
		harnesses = map[string]string{}
	}
	for m, h := range r.ModelHarnesses {
		if _, ok := harnesses[m]; !ok && h == "" {
			harnesses[m] = ""
		}
	}
	if _, ok := harnesses[model]; !ok {
		harnesses[model] = ""
	}
	r.ModelHarnesses = harnesses
	return harnesses[model]
}

// harnessOf_internal is the harness a run of model under harness name runs:
// claude proxied when the worker reads nova-config and the proxy has a
// route for the model's provider (a provider record names it), claude on
// its own login when none does, else harnessFor_internal.
func (r *Runner) harnessOf_internal(name, model string) (Harness, error) {
	if provider, _, _ := strings.Cut(model, "/"); r.Records != nil && name == "claude" && r.Proxy.HasRoute(provider) {
		return &ClaudeHarness{Proxied: true}, nil
	}
	return r.harnessFor_internal(name)
}

// harnessKey_internal names h apart from every other harness the probe
// may run: claude proxied is not claude on its own login.
func harnessKey_internal(h Harness) string {
	if c, ok := h.(*ClaudeHarness); ok && c.Proxied {
		return "claude (proxied)"
	}
	return h.Name()
}

// probeHarness_internal is the harness the probe runs model with: its
// record's (as last read), else the row's harness h.
func (r *Runner) probeHarness_internal(h Harness, model string) (Harness, error) {
	r.mu.Lock()
	name := r.ModelHarnesses[model]
	r.mu.Unlock()
	if name == "" {
		return h, nil
	}
	return r.harnessOf_internal(name, model)
}
