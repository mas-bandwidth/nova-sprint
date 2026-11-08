package policy

import (
	"context"
	"maps"
	"slices"
	"strings"

	"go.starlark.net/starlark"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Catalog is what a version's catalog() answers. Version is the policy
// version it is from (0 the built-in). Bench is the one model a bench runs
// for each tier, in provider/model form (a harness's model id); the deal
// names it (defect 57) and a tier it lacks deals to no bench.
type Catalog struct {
	Version int
	Bench   map[string]string
}

// CatalogFix is the command that changes the catalog.
const CatalogFix = "nova-sprint policy set <file> --reason <why>"

// clone is c with its own map.
func (c Catalog) clone() Catalog { return Catalog{Version: c.Version, Bench: maps.Clone(c.Bench)} }

// BenchModel is the model a bench runs for tier t, "" when the catalog
// names none.
func (c Catalog) BenchModel(t string) string { return c.Bench[t] }

// checkCatalog: {"bench": {tier: "provider/model"}}, each tier from TIERS
// once, each model in provider/model form. An older version's "models" and
// "harnesses" keys are accepted and not read. Linear in the answer, and its
// loop stops at the tick's deadline.
func checkCatalog(ctx context.Context, v starlark.Value) (Catalog, string) {
	d, msg := dictWith(v, []string{"bench"}, "models", "harnesses")
	if msg != "" {
		return Catalog{}, msg
	}
	b, ok := d["bench"].(*starlark.Dict)
	if !ok {
		return Catalog{}, refuse("bench is %s, not a dict of tier to model", kind(d["bench"]))
	}
	c := Catalog{Bench: map[string]string{}}
	for i, kv := range b.Items() {
		tier, okTier := kv[0].(starlark.String)
		model, okModel := kv[1].(starlark.String)
		provider, name, _ := strings.Cut(string(model), "/")
		switch {
		case late(ctx, i):
			return Catalog{}, overTime()
		case !okTier || !slices.Contains(store.Tiers, string(tier)):
			return Catalog{}, refuse("bench: tier %s is not one of %s", brief(kv[0]), strings.Join(store.Tiers, " "))
		case !okModel || provider == "" || name == "":
			return Catalog{}, refuse("bench: %s's model %s is not provider/model", string(tier), cut(kv[1].String(), 64))
		}
		c.Bench[string(tier)] = string(model)
	}
	return c, ""
}
