package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/worker"
	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// takeReceipt is the take seam (SEAMS 6): everything nova-worker needs to
// run a consumer. Model is the one the deal named and the consumer carries
// (a friend's own; a bench's the active policy's bench model for the card's
// tier, in provider/model form); take reads it from the consumer and never
// invents one (defect 57). Deadline (seconds) is the active policy's
// timeouts().consumer[tier], Forge the stream's, CommitName and CommitEmail
// the sprint's; the worker holds none of its own.
type takeReceipt struct {
	Consumer  string `json:"consumer"`
	Producer  string `json:"producer"`
	Kind      string `json:"kind"`
	Stream    string `json:"stream"`
	Repo      string `json:"repo"`
	Base      string `json:"base"`
	Branch    string `json:"branch"`
	Head      string `json:"head"`
	Tier      string `json:"tier"`
	Brief     string `json:"brief"`
	Prose     string `json:"prose"`
	Finding   string `json:"finding"`
	StreamRef string `json:"stream_ref"`
	BaseSHA   string `json:"base_sha"`
	PriorHead string `json:"prior_head"`
	// PriorFinished: a work attempt of the card reported ok at PriorHead
	// (card.FinishedAt), so the card machine refuses it as this attempt's
	// finish; false for a head an attempt pushed and then faulted, failed
	// or was withdrawn at. A bench's worker decides from it what an ok that
	// adds nothing to the prior head is.
	PriorFinished bool     `json:"prior_finished"`
	Paths         []string `json:"paths"`
	DoneWhen      []string `json:"done_when"`
	// StreamGates are the stream's gates (store.Stream.Gates): every card
	// of the stream passes them at its finished head, after its DONE-WHENs.
	StreamGates []string `json:"stream_gates"`
	Epoch       int      `json:"epoch"`
	Gen         int      `json:"gen"`
	Attempt     int      `json:"attempt"`
	Review      int      `json:"review"`
	Model       string   `json:"model"`
	Deadline    int      `json:"deadline"`
	Forge       string   `json:"forge"`
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
	opID := consumerOpID("take", snap.Epoch, id, gen)
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
	return e.takenOutput(ctx, in, snap, c, a)
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

// acknowledge writes row's acknowledgement of c, fenced on c's gen: one op,
// its id canonical, so a repeat replays.
func (e *Engine) acknowledge(ctx context.Context, row string, snap store.Snapshot, c store.Card, opID string) error {
	op := store.Op{ID: opID, Epoch: snap.Epoch, Actor: row, Kind: "take", At: e.Now(),
		Expect: []store.Expect{store.CardGen{Card: c.ID, Gen: c.Gen}},
		Writes: []store.Write{store.SetCursor{Name: steps.AckCursor(snap.Epoch, c.ID), Seq: max(snap.Seq, 1)}}}
	r, err := applyOnce(ctx, e.Store, op, e.Store.Apply)
	if err != nil || r.Accepted {
		return err
	}
	// refused: acknowledged all the same only if the cursor is written
	// (a take that raced this one), read from the store, never the words
	now, err := e.read(ctx, store.Scope{}) // the cursors
	if err != nil {
		return err
	}
	if _, acked := now.Cursors[steps.AckCursor(snap.Epoch, c.ID)]; acked {
		return nil
	}
	return refuse(r.Reason, "nova-sprint show "+c.ID)
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
// A work card's prior head is where it starts (worker.StartHead): the dealt
// branch's tip at origin, read now, when it holds work past the base, so
// work an earlier attempt pushed is never lost to a prior head the store
// did not learn. An origin that cannot be read refuses, after the
// acknowledgement; take again.
func (e *Engine) takenOutput(ctx context.Context, in *input, snap store.Snapshot, c store.Card, a policy.Answers) (string, error) {
	s := snap.Streams[c.Stream]
	prior := c.Prior
	if c.Kind == "work" {
		_, _, tip, ok, err := e.branchTip(ctx, snap, c, "then take again")
		if err != nil {
			return "", err
		}
		if ok {
			prior = worker.StartHead(c.BaseSHA, c.Prior, tip)
		}
	}
	t := takeReceipt{Consumer: c.ID, Producer: c.Of, Kind: c.Kind, Stream: c.Stream, Repo: s.Repo, Base: s.Base, Branch: c.Branch, Head: c.Head,
		Tier: c.Tier, Brief: c.Brief, Prose: c.Prose, Finding: c.Finding, StreamRef: s.Base, BaseSHA: c.BaseSHA,
		PriorHead: prior, PriorFinished: c.Kind == "work" && prior != "" && card.FinishedAt(consumersOf(snap, c.Of), prior),
		Paths: nonNil(c.Paths), DoneWhen: nonNil(c.DoneWhen), StreamGates: nonNil(s.Gates), Epoch: snap.Epoch, Gen: c.Gen, Attempt: c.Attempt,
		Review: c.Review, Model: c.Model, Deadline: a.Timeouts.Consumer[c.Tier], Forge: s.Forge,
		CommitName: snap.Identity.Name, CommitEmail: snap.Identity.Email}
	if in.has("json") {
		b, err := json.Marshal(t)
		return string(b) + "\n", err
	}
	return takenText(t, in.actor), nil
}

// takenText is take for a mind: the receipt line, the brief, and how the
// card finishes, from the rules a bench's JOB.md carries (KinFinishRules).
func takenText(t takeReceipt, row string) string {
	ref := fmtCard(t.Consumer, t.Gen)
	terms := fmt.Sprintf("deadline=%ds forge=%s", t.Deadline, orWord(t.Forge, "none"))
	rules := "How this card finishes:\n" + strings.TrimRight(worker.KinFinishRules(worker.Job{Kind: t.Kind, Branch: t.Branch, Head: t.Head, Stream: t.Stream, BaseSHA: t.BaseSHA,
		StreamGates: t.StreamGates, CommitName: t.CommitName, CommitEmail: t.CommitEmail}, worker.KinFinish{Ref: ref, Row: row, Epoch: t.Epoch}), "\n")
	brief := t.Brief
	for _, g := range t.StreamGates { // the stream's, after the card's own DONE-WHEN lines
		brief = strings.TrimRight(brief, "\n") + "\nSTREAM GATE: " + g
	}
	if t.Kind == "read" {
		return done(fmt.Sprintf("nova-sprint read %s yes|fix --as %s --epoch %d", ref, row, t.Epoch),
			fmt.Sprintf("TAKEN %s repo=%s head=%s %s", t.Consumer, t.Repo, t.Head, terms), brief, rules)
	}
	lines := []string{fmt.Sprintf("TAKEN %s repo=%s base=%s branch=%s %s", t.Consumer, t.Repo, t.BaseSHA, t.Branch, terms)}
	if t.PriorHead != "" {
		start := "continue from " + t.PriorHead + ", never from the base: git fetch origin " + t.PriorHead + ", then git checkout -b " + t.Branch + " " + t.PriorHead +
			"; merge a newer base into it, never rebase."
		if t.Finding != "" {
			start += " The finding: " + t.Finding
		}
		lines = append(lines, start)
	}
	lines = append(lines, brief, rules)
	return done(fmt.Sprintf("nova-sprint finish %s --head <sha> --as %s --epoch %d", ref, row, t.Epoch), lines...)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
