package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// takeReceipt is the take seam (SEAMS 6): everything nova-worker needs to
// run a consumer. Model is the roster's, Deadline (seconds) the active
// policy's timeouts().consumer[tier], Forge the stream's; the worker holds
// none of its own.
type takeReceipt struct {
	Consumer  string   `json:"consumer"`
	Producer  string   `json:"producer"`
	Kind      string   `json:"kind"`
	Stream    string   `json:"stream"`
	Repo      string   `json:"repo"`
	Base      string   `json:"base"`
	Branch    string   `json:"branch"`
	Head      string   `json:"head"`
	Tier      string   `json:"tier"`
	Brief     string   `json:"brief"`
	Prose     string   `json:"prose"`
	Finding   string   `json:"finding"`
	StreamRef string   `json:"stream_ref"`
	BaseSHA   string   `json:"base_sha"`
	PriorHead string   `json:"prior_head"`
	Paths     []string `json:"paths"`
	DoneWhen  []string `json:"done_when"`
	Epoch     int      `json:"epoch"`
	Gen       int      `json:"gen"`
	Attempt   int      `json:"attempt"`
	Review    int      `json:"review"`
	Model     string   `json:"model"`
	Deadline  int      `json:"deadline"`
	Forge     string   `json:"forge"`
}

// runTake is `take`: a consumer dealt to this row at this gen goes to
// working. The width check is soft, on the snapshot (no seq fence); the op
// id is canonical, so a repeat prints the same receipt.
func runTake(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	id, gen, f := cardRef("take", in.arg(0))
	if f != nil {
		return "", f
	}
	opID, err := machineID(in, snap, "take", id, gen)
	if err != nil {
		return "", err
	}
	lim, tmo, err := e.rules(ctx, snap)
	if err != nil {
		return "", err
	}
	ro, err := e.roster(ctx, lim)
	if err != nil {
		return "", err
	}
	r, found, err := e.Store.Replay(ctx, opID)
	if err != nil {
		return "", err
	}
	if !found {
		if r, err = e.take(ctx, in.actor, snap, ro, id, gen, opID); err != nil {
			return "", err
		}
	}
	return takenOutput(in, snap, r, ro, tmo, id)
}

// machineID is a row's op id on a consumer, checked against --epoch and
// --id when given.
func machineID(in *input, snap store.Snapshot, kind, id string, gen int) (string, error) {
	epoch, f := in.num("epoch")
	if f != nil {
		return "", f
	}
	if err := epochNow(epoch, snap); err != nil {
		return "", err
	}
	opID := consumerOpID(kind, snap.Epoch, id, gen)
	if given := in.str("id"); given != "" && given != opID {
		return "", refuse("--id "+given+" is not this "+kind+"'s id", "pass --id "+opID+", or leave --id out")
	}
	return opID, nil
}

func (e *Engine) take(ctx context.Context, row string, snap store.Snapshot, ro roster.Roster, id string, gen int, opID string) (store.Receipt, error) {
	c, ok := snap.Cards[id]
	if !ok || c.Gen != gen || c.Holder != row || (c.Kind != "work" && c.Kind != "read") {
		return store.Receipt{}, refuse(id+" is not dealt to this row at this generation", "nova-sprint show "+id)
	}
	_, width, tiers, ok := ro.Row(row)
	if !ok {
		return store.Receipt{}, refuse("row "+row+" is not in nova-config", "add it to nova-config (nova-config friend add, or a bench machine), then take again")
	}
	if !slices.Contains(tiers, c.Tier) {
		return store.Receipt{}, refuse(fmt.Sprintf("row %s does not serve tier %s", row, c.Tier), "nova-sprint release "+fmtCard(id, gen)+" --as "+row+" --reason \"tier "+c.Tier+" not served\"")
	}
	if busy := working(snap, row); len(busy) >= width {
		return store.Receipt{}, refuse(fmt.Sprintf("row %s is at width %d", row, width), "finish "+firstOr(busy, "a card")+" first")
	}
	plan, why := card.MoveConsumer(card.FromStore(c), card.FromStore(snap.Cards[c.Of]), card.RowFromStore(snap.Rows[row], width, tiers), card.Take{Row: row})
	if why != nil {
		return store.Receipt{}, moved(why, id)
	}
	op := store.Op{ID: opID, Epoch: snap.Epoch, Actor: row, Kind: "take", At: e.Now(), Expect: plan.Expect, Writes: plan.Writes}
	return e.commit(ctx, op, "nova-sprint show "+id)
}

// working are the consumers row works on now, sorted.
func working(snap store.Snapshot, row string) []string {
	var out []string
	for _, c := range snap.Cards {
		if c.Holder == row && c.State == card.Working && (c.Kind == "work" || c.Kind == "read") {
			out = append(out, c.ID)
		}
	}
	slices.Sort(out)
	return out
}

func firstOr(s []string, or string) string {
	if len(s) == 0 {
		return or
	}
	return s[0]
}

// takenOutput is the receipt, as JSON for a machine or as the brief for a
// mind; a stored receipt of another row's take, or a refused one, is a
// refusal.
func takenOutput(in *input, snap store.Snapshot, r store.Receipt, ro roster.Roster, tmo policy.Timeouts, id string) (string, error) {
	if r.Actor != in.actor || r.Kind != "take" {
		return "", refuse("this take was "+r.Kind+" by "+r.Actor, "nova-sprint next --as "+in.actor)
	}
	if !r.Accepted {
		return "", refuse(r.Reason, "nova-sprint show "+id)
	}
	c, ok := r.After.Cards[id]
	if !ok {
		return "", broken("the take's receipt does not hold "+id, "nova-sprint show "+id)
	}
	s := snap.Streams[c.Stream]
	t := takeReceipt{Consumer: c.ID, Producer: c.Of, Kind: c.Kind, Stream: c.Stream, Repo: s.Repo, Base: s.Base, Branch: c.Branch, Head: c.Head,
		Tier: c.Tier, Brief: c.Brief, Prose: c.Prose, Finding: c.Finding, StreamRef: git.StreamBranch(c.Stream), BaseSHA: c.BaseSHA,
		PriorHead: c.Prior, Paths: nonNil(c.Paths), DoneWhen: nonNil(c.DoneWhen), Epoch: r.Epoch, Gen: c.Gen, Attempt: c.Attempt,
		Review: c.Review, Model: ro.Kin[in.actor].Model, Deadline: tmo.Consumer[c.Tier], Forge: s.Forge}
	if in.has("json") {
		b, err := json.Marshal(t)
		return string(b) + "\n", err
	}
	return takenText(t, in.actor), nil
}

func takenText(t takeReceipt, row string) string {
	ref := fmtCard(t.Consumer, t.Gen)
	terms := fmt.Sprintf("deadline=%ds forge=%s", t.Deadline, forgeWord(t.Forge))
	if t.Kind == "read" {
		return done(fmt.Sprintf("nova-sprint read %s yes|fix --as %s --epoch %d", ref, row, t.Epoch),
			fmt.Sprintf("TAKEN %s repo=%s head=%s %s", t.Consumer, t.Repo, t.Head, terms), "Read detached at this head; do not edit or push.", t.Brief)
	}
	lines := []string{fmt.Sprintf("TAKEN %s repo=%s base=%s branch=%s %s", t.Consumer, t.Repo, t.BaseSHA, t.Branch, terms)}
	if t.PriorHead != "" {
		lines = append(lines, "continue from "+t.PriorHead+" (git fetch origin "+t.Branch+"); the finding: "+t.Finding)
	}
	if t.Forge != "" {
		lines = append(lines, "the stream wants a PR: gh pr create --base "+t.StreamRef+" --head "+t.Branch)
	}
	lines = append(lines, t.Brief)
	return done(fmt.Sprintf("nova-sprint finish %s --head <sha> --as %s --epoch %d", ref, row, t.Epoch), lines...)
}

func forgeWord(f string) string {
	if f == "" {
		return "none"
	}
	return f
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
