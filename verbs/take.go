package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/edge/worker"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// takeReceipt is the take seam (SEAMS 6): everything nova-worker needs to
// run a consumer. Model is the one the deal named and the consumer carries
// (a friend's own; a bench's the active policy's bench model for the card's
// tier, in provider/model form); take reads it from the consumer and never
// invents one (defect 57). Deadline (seconds) is the active policy's
// timeouts().consumer[tier], Forge the stream's, CommitName and CommitEmail
// the sprint's; the worker holds none of its own.
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
	// CommitName and CommitEmail are the sprint's commit identity
	// (store.Identity): the worker writes them into the job's clone.
	CommitName  string `json:"commit_name"`
	CommitEmail string `json:"commit_email"`
}

// runTake is `take`: the row's acknowledgement of a card dealt to it. The
// deal is the take (defect 52): the consumer is in the row's hand, working,
// from the deal, and the deadline runs from there. take writes the
// acknowledgement once (steps.AckCursor), which stops the nudges, and
// prints the receipt; a repeat, or a take after the acknowledgement, prints
// the same receipt and writes nothing.
func runTake(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	id, gen, f := cardRef("take", in.arg(0))
	if f != nil {
		return "", f
	}
	opID, err := machineID(in, snap, "take", id, gen)
	if err != nil {
		return "", err
	}
	a, err := e.answers(ctx, snap)
	if err != nil {
		return "", err
	}
	ro, err := e.roster(ctx, a.Limits, snap)
	if err != nil {
		return "", err
	}
	c, err := heldBy(snap, ro, in.actor, id, gen)
	if err != nil {
		return "", err
	}
	if !snap.Identity.Set() {
		return "", noIdentity()
	}
	if _, acked := snap.Cursors[steps.AckCursor(snap.Epoch, id)]; !acked {
		if err := e.acknowledge(ctx, in.actor, snap, c, opID); err != nil {
			return "", err
		}
	}
	return takenOutput(in, snap, c, a)
}

// heldBy is consumer id at gen as row holds it: dealt to row, working, on
// a row nova-config names and serving the card's tier, with the model its
// deal named.
func heldBy(snap store.Snapshot, ro roster.Roster, row, id string, gen int) (store.Card, error) {
	c, ok := snap.Cards[id]
	switch {
	case !ok || c.Gen != gen || c.Holder != row || (c.Kind != "work" && c.Kind != "read"):
		return c, refuse(id+" is not dealt to this row at this generation", "nova-sprint next --as "+row)
	case c.State == card.Ready:
		return c, refuse(id+" was dealt before the deal became the take; the next tick withdraws it and deals it again, in hand", "nova-sprint next --as "+row)
	case c.State != card.Working:
		return c, refuse(fmt.Sprintf("%s is %s: %s", id, c.State, orWord(c.Reason, "no reason given")), "nova-sprint next --as "+row)
	}
	_, _, tiers, ok := ro.Row(row)
	if !ok {
		return c, refuse("row "+row+" is not in nova-config", "add it to nova-config (nova-config friend add, or a bench machine), then take again")
	}
	if !slices.Contains(tiers, c.Tier) {
		return c, refuse(fmt.Sprintf("row %s does not serve tier %s", row, c.Tier), "nova-sprint release "+fmtCard(id, gen)+" --as "+row+" --reason \"tier "+c.Tier+" not served\"")
	}
	return c, nil
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

// acknowledge writes row's acknowledgement of c, fenced on c's gen: one op,
// its id canonical, so a repeat replays.
func (e *Engine) acknowledge(ctx context.Context, row string, snap store.Snapshot, c store.Card, opID string) error {
	op := store.Op{ID: opID, Epoch: snap.Epoch, Actor: row, Kind: "take", At: e.Now(),
		Expect: []store.Expect{store.CardGen{Card: c.ID, Gen: c.Gen}},
		Writes: []store.Write{store.SetCursor{Name: steps.AckCursor(snap.Epoch, c.ID), Seq: max(snap.Seq, 1)}}}
	r, err := applyOnce(ctx, e.Store, op, e.Store.Apply)
	switch {
	case err != nil:
		return err
	case !r.Accepted && !strings.Contains(r.Reason, "already at"):
		return refuse(r.Reason, "nova-sprint show "+c.ID)
	}
	return nil
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

// takenOutput is the receipt, as JSON for a machine or as the brief for a
// mind, of consumer c as row holds it: the model is the one its deal named.
func takenOutput(in *input, snap store.Snapshot, c store.Card, a policy.Answers) (string, error) {
	s := snap.Streams[c.Stream]
	t := takeReceipt{Consumer: c.ID, Producer: c.Of, Kind: c.Kind, Stream: c.Stream, Repo: s.Repo, Base: s.Base, Branch: c.Branch, Head: c.Head,
		Tier: c.Tier, Brief: c.Brief, Prose: c.Prose, Finding: c.Finding, StreamRef: s.Base, BaseSHA: c.BaseSHA,
		PriorHead: c.Prior, Paths: nonNil(c.Paths), DoneWhen: nonNil(c.DoneWhen), Epoch: snap.Epoch, Gen: c.Gen, Attempt: c.Attempt,
		Review: c.Review, Model: c.Model, Deadline: a.Timeouts.Consumer[c.Tier], Forge: s.Forge,
		CommitName: snap.Identity.Name, CommitEmail: snap.Identity.Email}
	if in.has("json") {
		b, err := json.Marshal(t)
		return string(b) + "\n", err
	}
	return takenText(t, in.actor), nil
}

func takenText(t takeReceipt, row string) string {
	ref := fmtCard(t.Consumer, t.Gen)
	terms := fmt.Sprintf("deadline=%ds forge=%s", t.Deadline, orWord(t.Forge, "none"))
	if t.Kind == "read" {
		return done(fmt.Sprintf("nova-sprint read %s yes|fix --as %s --epoch %d", ref, row, t.Epoch),
			fmt.Sprintf("TAKEN %s repo=%s head=%s %s", t.Consumer, t.Repo, t.Head, terms), "Read detached at this head; do not edit or push.", t.Brief)
	}
	lines := []string{fmt.Sprintf("TAKEN %s repo=%s base=%s branch=%s %s", t.Consumer, t.Repo, t.BaseSHA, t.Branch, terms)}
	if t.PriorHead != "" {
		lines = append(lines, "continue from "+t.PriorHead+" (git fetch origin "+t.Branch+"); the finding: "+t.Finding)
	}
	lines = append(lines, strings.TrimRight(worker.CommitRules(t.CommitName, t.CommitEmail), "\n"), t.Brief)
	return done(fmt.Sprintf("nova-sprint finish %s --head <sha> --as %s --epoch %d", ref, row, t.Epoch), lines...)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
