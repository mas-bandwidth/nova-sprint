package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// RowCatalog checks labels against the configured models and harnesses.
type RowCatalog interface {
	Catalog(context.Context, store.Snapshot) (version int, models, harnesses []string, err error)
}

type rowInput struct {
	name, kind, actor, model, harness string
	width                             int
	widthSet                          bool
	tiers                             []string
}

func parseRow(args []string, actor string) (rowInput, bool) {
	in := rowInput{actor: actor}
	if len(args) == 0 || args[0] == "" {
		return in, false
	}
	in.name = args[0]
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		if i+1 >= len(args) || args[i+1] == "" || seen[args[i]] {
			return in, false
		}
		seen[args[i]] = true
		v := args[i+1]
		switch args[i] {
		case "--kind":
			in.kind = v
		case "--width":
			n, err := strconv.ParseInt(v, 10, 32)
			if err != nil || n < 0 {
				return in, false
			}
			in.width = int(n)
			in.widthSet = true
		case "--tiers":
			in.tiers = strings.Split(v, ",")
		case "--model":
			in.model = v
		case "--harness":
			in.harness = v
		case "--as":
			in.actor = v
		default:
			return in, false
		}
		i++
	}
	return in, in.actor != "" && in.kind != "" && in.widthSet && len(in.tiers) > 0
}
func validRowText(s string) bool {
	return s != "" && len(s) <= 128 && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func validateRow(in rowInput) string {
	if !validRowText(in.actor) {
		return "invalid actor"
	}
	if !validRowText(in.name) {
		return "invalid row name"
	}
	if in.kind != "friend" && in.kind != "worker" {
		return "kind " + in.kind + " is not friend or worker"
	}
	if in.model == "" {
		return "model is missing"
	}
	if !validRowText(in.model) {
		return "invalid model " + in.model
	}
	if in.harness == "" {
		return "harness is missing"
	}
	if !validRowText(in.harness) {
		return "invalid harness " + in.harness
	}
	seen := map[string]bool{}
	for _, tier := range in.tiers {
		if !slices.Contains(store.Tiers, tier) {
			return "unknown tier " + tier
		}
		if seen[tier] {
			return "tier " + tier + " is named twice"
		}
		seen[tier] = true
	}
	return ""
}
func (e *Engine) row(ctx context.Context, args []string) Result {
	if len(args) == 0 || args[0] != "add" {
		return refused("row", "invalid arguments", "nova-sprint help row")
	}
	in, ok := parseRow(args[1:], e.Actor)
	if !ok {
		return refused("row add", "invalid arguments", "nova-sprint help row add")
	}
	if why := validateRow(in); why != "" {
		return refused("row add", why, "nova-sprint help row add")
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Env == nil || e.Now == nil || e.RowCatalog == nil {
		return unavailable("row add")
	}
	coordinator, err := e.Env.Coordinator(ctx)
	if err != nil {
		return operationalFailure("row add", err)
	}
	if in.actor != coordinator {
		return refused("row add", "only the coordinator can add a row", "ask "+coordinator+" over the bus")
	}
	hooked, recipe, err := e.Env.Hooked(ctx, in.actor)
	if err != nil {
		return operationalFailure("row add", err)
	}
	if !hooked {
		return refused("row add", in.actor+" is not hooked in", recipe)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure("row add", err)
	}
	version, models, harnesses, err := e.RowCatalog.Catalog(ctx, snap)
	if err != nil {
		return operationalFailure("row add", err)
	}
	if version != snap.Policy {
		return unavailable("row add")
	}
	if !slices.Contains(models, in.model) {
		return refused("row add", fmt.Sprintf("model %s is not in policy version %d", in.model, version), "policy set to add it")
	}
	if !slices.Contains(harnesses, in.harness) {
		return refused("row add", fmt.Sprintf("harness %s is not in policy version %d", in.harness, version), "policy set to add it")
	}
	if _, exists := snap.Rows[in.name]; exists {
		return refused("row add", "row "+in.name+" exists", "nova-sprint show "+in.name)
	}
	return e.applyRow(ctx, st, snap, in)
}
func (e *Engine) applyRow(ctx context.Context, st ApplyStore, snap store.Snapshot, in rowInput) Result {
	kind := in.kind
	if kind == "friend" {
		kind = "kin"
	}
	key, _ := json.Marshal([]any{in.name, kind, in.width, in.tiers, in.model, in.harness, in.actor})
	op := store.Op{ID: consumerOpID("row add:"+in.actor, snap.Epoch, string(key), int(snap.Seq)), Epoch: snap.Epoch, Actor: in.actor, Kind: "row add", At: e.Now(), Expect: []store.Expect{store.AtSeq{Seq: snap.Seq}}, Writes: []store.Write{store.PutRow{Name: in.name, Kind: kind, Width: in.width, Tiers: in.tiers, Model: in.model, Harness: in.harness}}}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return operationalFailure("row add", err)
	}
	if !rec.Accepted {
		return refused("row add", rec.Reason, "nova-sprint show "+in.name)
	}
	next := "nova-sprint worker --as " + in.name
	if in.kind == "friend" {
		next = "nova-bus hook --as " + in.name
	}
	return Result{fmt.Sprintf("ROW ADDED %s kind=%s width=%d tiers=%s model=%s harness=%s\nnext: %s\n", in.name, in.kind, in.width, strings.Join(in.tiers, ","), in.model, in.harness, next), 0}
}
