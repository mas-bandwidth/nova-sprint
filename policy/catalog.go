package policy

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

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
// compiled and validated like every other answer; a version that fails is a
// Failure. A stored version with no catalog() has the built-in's. An unknown
// or negative version, or one of kind data, is a Refusal naming the list.
func ReadCatalog(ctx context.Context, st store.Store, v int) (Catalog, error) {
	if v == 0 {
		return builtinCatalog(), nil
	}
	if v < 0 {
		return Catalog{}, &Refusal{"catalog", fmt.Sprintf("policy version %d does not exist", v), "nova-sprint policy show --list"}
	}
	pv, err := version(ctx, st, "catalog", v)
	switch {
	case err != nil:
		return Catalog{}, err
	case pv.Kind != "script":
		return Catalog{}, &Refusal{"catalog", fmt.Sprintf("v%d is of kind %s, which policy does not run", v, pv.Kind), "nova-sprint policy show --list"}
	}
	c, f := catalogOf(pv)
	switch {
	case f != nil:
		return Catalog{}, f
	case c.Models == nil: // a version from before catalog: the built-in's
		c = builtinCatalog()
		c.Version = v
	}
	return c, nil
}

// builtinCatalog is the built-in's catalog; the embedded default is checked
// by the tests, so a failure here is a bug in the binary and panics.
var builtinCatalog = sync.OnceValue(func() Catalog {
	c, f := catalogOf(store.PolicyVersion{Version: 0, Kind: "script", Body: defaultBody})
	if f != nil {
		panic(f.Error())
	}
	return c
})

// catalogOf compiles a version and answers its catalog() under a tick's
// budget; the zero Catalog when it defines none.
func catalogOf(pv store.PolicyVersion) (Catalog, *Failure) {
	p, f := compile(pv.Version, Label(pv.Version), pv.Body)
	if f != nil {
		return Catalog{}, f
	}
	if p.fns["catalog"] == nil {
		return Catalog{}, nil // the zero catalog: the version has none
	}
	b := newBudget(p.label)
	defer b.stop()
	v, f := (&Engine{answers: map[string]starlark.Value{}}).call(p, ask{q: "catalog", key: "()"}, b)
	if f != nil {
		return Catalog{}, f
	}
	c, msg := checkCatalog(v)
	if msg != "" {
		return Catalog{}, &Failure{Version: p.version, Label: p.label, Question: "catalog", Line: int(p.fns["catalog"].Position().Line), Msg: msg}
	}
	c.Version = pv.Version
	return c, nil
}

// Unknown is "" when model and harness are both in the catalog, else what
// is not, naming the policy version; the fix is CatalogFix.
func (c Catalog) Unknown(model, harness string) string {
	var names []string
	for _, p := range sortedKeys(c.Models) {
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
// [h]}; names non-empty, tiers from TIERS, nothing listed twice.
func checkCatalog(v starlark.Value) (Catalog, string) {
	d, msg := dictOf(v, "models", "harnesses")
	if msg != "" {
		return Catalog{}, msg
	}
	c := Catalog{Models: map[string][]Model{}}
	if c.Harnesses, msg = names("harnesses", d["harnesses"]); msg != "" {
		return Catalog{}, msg
	}
	providers, ok := d["models"].(*starlark.Dict)
	if !ok {
		return Catalog{}, refuse("models is %s, not a dict of provider to models", kind(d["models"]))
	}
	for _, kv := range providers.Items() {
		p, ok := kv[0].(starlark.String)
		if !ok || p == "" {
			return Catalog{}, refuse("a provider is %s, not a name", show(kv[0]))
		}
		if c.Models[string(p)], msg = models(string(p), kv[1]); msg != "" {
			return Catalog{}, msg
		}
	}
	return c, twice(c.Models)
}

// twice refuses a model listed under two providers: nothing is listed twice.
func twice(ms map[string][]Model) string {
	seen := map[string]string{}
	for _, p := range sortedKeys(ms) {
		for _, m := range ms[p] {
			if q, ok := seen[m.Name]; ok && q != p {
				return refuse("model %s is listed under both %s and %s", m.Name, q, p)
			}
			seen[m.Name] = p
		}
	}
	return ""
}

func models(provider string, v starlark.Value) ([]Model, string) {
	l, ok := v.(*starlark.List)
	if !ok {
		return nil, refuse("%s is %s, not a list of models", provider, kind(v))
	}
	out := make([]Model, l.Len())
	for i := range out {
		at := fmt.Sprintf("%s model %d", provider, i+1)
		d, msg := dictOf(l.Index(i), "name", "tier")
		if msg != "" {
			return nil, at + ": " + msg
		}
		name, okName := d["name"].(starlark.String)
		tier, okTier := d["tier"].(starlark.String)
		switch {
		case !okName || name == "":
			return nil, refuse("%s: name is %s, not a model name", at, show(d["name"]))
		case !okTier || !slices.Contains(store.Tiers, string(tier)):
			return nil, refuse("%s: tier is %s, not one of %s", at, show(d["tier"]), strings.Join(store.Tiers, " "))
		}
		out[i] = Model{Name: string(name), Tier: string(tier)}
		if slices.Contains(out[:i], out[i]) {
			return nil, refuse("%s: %s at %s is listed twice", at, name, tier)
		}
	}
	return out, ""
}

// names is a list of distinct non-empty strings.
func names(what string, v starlark.Value) ([]string, string) {
	l, ok := v.(*starlark.List)
	if !ok {
		return nil, refuse("%s is %s, not a list of names", what, kind(v))
	}
	out := make([]string, l.Len())
	for i := range out {
		s, ok := l.Index(i).(starlark.String)
		switch {
		case !ok || s == "":
			return nil, refuse("%s element %d is %s, not a name", what, i+1, show(l.Index(i)))
		case slices.Contains(out[:i], string(s)):
			return nil, refuse("%s element %d, %s, is listed twice", what, i+1, s)
		}
		out[i] = string(s)
	}
	return out, ""
}
