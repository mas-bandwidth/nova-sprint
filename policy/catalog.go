package policy

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"go.starlark.net/starlark"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Catalog is what a version's catalog() answers: the models rows may name,
// by provider, and the harnesses. Version is the policy version it is from
// (0 the built-in). The row verbs refuse an unknown model or harness with
// Unknown, naming the version and CatalogFix.
type Catalog struct {
	Version   int
	Models    map[string][]Model
	Harnesses []string
}

// Model is one model and a tier it serves; a model serving two tiers is two entries.
type Model struct{ Name, Tier string }

// CatalogFix is the command that changes the catalog.
const CatalogFix = "nova-sprint policy set <file> --reason <why>"

// ReadCatalog reads version v's catalog (0 the built-in) from the store,
// compiled, run and validated under a tick's deadline like every other
// answer; a version that fails is a *Failure. A version with no catalog()
// has the built-in's, and its Version is 0, the catalog that answered, as
// in a tick's Answers (ruling 7). An unknown or negative version, or one of
// kind data, is a Refusal naming the list. Each call returns its own copy.
// e runs it (round 5: on e's clock, and in e's evaluator).
func ReadCatalog(ctx context.Context, st store.Store, e *Engine, v int) (Catalog, error) {
	pv := store.PolicyVersion{Body: defaultBody}
	if v != 0 {
		var err error
		if pv, err = script(ctx, st, "catalog", v); err != nil {
			return Catalog{}, err
		}
	}
	a, fs := e.catalog(ctx, storedSource(v, pv.Body))
	if len(fs) > 0 {
		return Catalog{}, &fs[0]
	}
	return a.Catalog, nil
}

// catalog is src's catalog, run in this process under a tick's deadline.
func (e *Engine) catalog(ctx context.Context, src source) (Answers, []Failure) {
	start := e.clock.Now()
	if e.proc != nil {
		return e.remote(ctx, start, request{Kind: "catalog", Src: src.wire()}, []source{src})
	}
	ctx, cancel := e.deadline(ctx, start)
	defer cancel()
	return e.race(ctx, start, []source{src}, []ask{catalogAsk(ctx)})
}

// clone is c with its own map and slices.
func (c Catalog) clone() Catalog {
	out := Catalog{Version: c.Version, Models: make(map[string][]Model, len(c.Models)), Harnesses: slices.Clone(c.Harnesses)}
	for p, ms := range c.Models {
		out.Models[p] = slices.Clone(ms)
	}
	return out
}

// Unknown is "" when model and harness are both in the catalog, else what
// is not, naming the policy version; the fix is CatalogFix.
func (c Catalog) Unknown(model, harness string) string {
	var names []string
	for _, p := range slices.Sorted(maps.Keys(c.Models)) {
		for _, m := range c.Models[p] {
			if m.Name == model {
				return c.unknownHarness(harness)
			}
			names = append(names, m.Name)
		}
	}
	return fmt.Sprintf("model %q is not in the catalog of policy %s (models: %s)", model, Label(c.Version), strings.Join(slices.Compact(names), " "))
}

func (c Catalog) unknownHarness(harness string) string {
	if slices.Contains(c.Harnesses, harness) {
		return ""
	}
	return fmt.Sprintf("harness %q is not in the catalog of policy %s (harnesses: %s)", harness, Label(c.Version), strings.Join(c.Harnesses, " "))
}

// checkCatalog: {"models": {provider: [{"name": n, "tier": t}]}, "harnesses":
// [h]}; names non-empty, tiers from TIERS, nothing listed twice. Linear in
// the answer, and its loops stop at the tick's deadline.
func checkCatalog(ctx context.Context, v starlark.Value) (Catalog, string) {
	d, msg := dictOf(v, "models", "harnesses")
	if msg != "" {
		return Catalog{}, msg
	}
	c := Catalog{Models: map[string][]Model{}}
	if c.Harnesses, msg = names(ctx, "harnesses", d["harnesses"]); msg != "" {
		return Catalog{}, msg
	}
	providers, ok := d["models"].(*starlark.Dict)
	if !ok {
		return Catalog{}, refuse("models is %s, not a dict of provider to models", kind(d["models"]))
	}
	for i, kv := range providers.Items() {
		p, ok := kv[0].(starlark.String)
		switch {
		case late(ctx, i):
			return Catalog{}, overTime()
		case !ok || p == "":
			return Catalog{}, refuse("a provider is %s, not a name", brief(kv[0]))
		}
		if c.Models[string(p)], msg = models(ctx, string(p), kv[1]); msg != "" {
			return Catalog{}, msg
		}
	}
	return c, twice(c.Models)
}

// twice refuses a model listed under two providers: nothing is listed twice.
func twice(ms map[string][]Model) string {
	seen := map[string]string{}
	for _, p := range slices.Sorted(maps.Keys(ms)) {
		for _, m := range ms[p] {
			if q, ok := seen[m.Name]; ok && q != p {
				return refuse("model %s is listed under both %s and %s", cut(m.Name, 64), cut(q, 64), cut(p, 64))
			}
			seen[m.Name] = p
		}
	}
	return ""
}

func models(ctx context.Context, provider string, v starlark.Value) ([]Model, string) {
	provider = cut(provider, 64)
	l, ok := v.(*starlark.List)
	if !ok {
		return nil, refuse("%s is %s, not a list of models", provider, kind(v))
	}
	out, seen := make([]Model, l.Len()), map[Model]bool{}
	for i := range out {
		if late(ctx, i) {
			return nil, overTime()
		}
		at := fmt.Sprintf("%s model %d", provider, i+1)
		d, msg := dictOf(l.Index(i), "name", "tier")
		if msg != "" {
			return nil, at + ": " + msg
		}
		name, okName := d["name"].(starlark.String)
		tier, okTier := d["tier"].(starlark.String)
		switch {
		case !okName || name == "":
			return nil, refuse("%s: name is %s, not a model name", at, brief(d["name"]))
		case !okTier || !slices.Contains(store.Tiers, string(tier)):
			return nil, refuse("%s: tier is %s, not one of %s", at, brief(d["tier"]), strings.Join(store.Tiers, " "))
		}
		out[i] = Model{Name: string(name), Tier: string(tier)}
		if seen[out[i]] {
			return nil, refuse("%s: %s at %s is listed twice", at, brief(name), tier)
		}
		seen[out[i]] = true
	}
	return out, ""
}

// names is a list of distinct non-empty strings.
func names(ctx context.Context, what string, v starlark.Value) ([]string, string) {
	l, ok := v.(*starlark.List)
	if !ok {
		return nil, refuse("%s is %s, not a list of names", what, kind(v))
	}
	out, seen := make([]string, l.Len()), map[string]bool{}
	for i := range out {
		s, ok := l.Index(i).(starlark.String)
		switch {
		case late(ctx, i):
			return nil, overTime()
		case !ok || s == "":
			return nil, refuse("%s element %d is %s, not a name", what, i+1, brief(l.Index(i)))
		case seen[string(s)]:
			return nil, refuse("%s element %d, %s, is listed twice", what, i+1, brief(s))
		}
		out[i], seen[string(s)] = string(s), true
	}
	return out, ""
}
