package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// runRelease is `release`: a row gives back its own held card with a
// reason; another holder's card is the coordinator's. The consumer is withdrawn and its branch kept. A release is
// recorded as released, neither ok nor fail: the consumer keeps no result
// (so no row's OK% counts it) and its reason starts "released: "; the
// producer goes back with its bounces and score unchanged, and the redeal is
// a new attempt with no blame.
func runRelease(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	reason := in.str("reason")
	if reason == "" {
		return "", refuse("a release needs its reason", "add --reason \"<why>\"")
	}
	id, gen, f := cardRef("release", in.arg(0))
	if f != nil {
		return "", f
	}
	c, ok := snap.Cards[id]
	if !ok || c.Gen != gen || !live(c) {
		return "", refuse(id+" is not held at generation "+fmt.Sprint(gen), "nova-sprint show "+id)
	}
	if c.Holder != in.actor && snap.Rows[in.actor].Role != "coordinator" {
		return "", refuse(id+" is held by "+c.Holder+", not "+in.actor, "ask "+c.Holder+", or the coordinator, to release it")
	}
	c, herr := e.releasedHead(ctx, snap, c, strings.ToLower(in.str("head")))
	if herr != nil {
		return "", herr
	}
	opID, err := machineID(in, snap, "release", id, gen)
	if err != nil {
		return "", err
	}
	plan, err := withdraw(snap, []store.Card{c}, reason)
	if err != nil {
		return "", err
	}
	if in.has("dry-run") {
		return done("nova-sprint release "+fmtCard(id, gen)+" --reason \"<why>\" (without --dry-run)", "RELEASE "+fmtCard(id, gen)+" (dry run)"), nil
	}
	op := store.Op{ID: opID, Epoch: snap.Epoch, Actor: in.actor, Kind: "release", At: e.Now(), Expect: plan.Expect, Writes: plan.Writes, Note: reason}
	r, err := e.commit(ctx, op, "nova-sprint show "+id)
	if err != nil {
		return "", err
	}
	return done(waitNext, fmt.Sprintf("RELEASED %s applied at seq %d; branch %s kept", id, r.Seq, c.Branch)), nil
}

// releasedHead is c with head as its last pushed head: a worker releasing a
// working work card it held when it restarted names the partial head it
// pushed, so the redeal continues from it (the head is checked as a
// finish's is: a full sha at the tip of the card's branch at origin). No
// head keeps the card's own.
func (e *Engine) releasedHead(ctx context.Context, snap store.Snapshot, c store.Card, head string) (store.Card, error) {
	if head == "" || head == c.Head {
		return c, nil
	}
	if c.Kind != "work" || c.State != card.Working {
		return c, refuse("only a working work card carries a head back", "release "+fmtCard(c.ID, c.Gen)+" without --head")
	}
	if err := e.headReady(ctx, snap, c, head); err != nil {
		return c, err
	}
	c.Head = head
	return c, nil
}

// ReleasedPrefix starts a released consumer's reason: the record that it was
// given back, not failed.
const ReleasedPrefix = card.ReleasedPrefix

// withdraw is one plan withdrawing cards, each with its producer's stream
// (a release into a held stream parks) and its last pushed head.
func withdraw(snap store.Snapshot, cards []store.Card, reason string) (card.Plan, error) {
	var plans []card.Plan
	for _, c := range cards {
		p := snap.Cards[c.Of]
		head := ""
		if c.Kind == "work" && c.State == card.Working {
			head = c.Head
		}
		ev := card.Withdraw{Reason: ReleasedPrefix + reason, Head: head, Stream: card.StreamFromStore(snap.Streams[p.Stream])}
		plan, why := card.MoveConsumer(card.FromStore(c), card.FromStore(p), card.RowFromStore(snap.Rows[c.Holder], 0, nil), ev)
		if why != nil {
			return card.Plan{}, moved(why, c.ID)
		}
		plans = append(plans, plan)
	}
	plan, why := card.Merge(plans...)
	if why != nil {
		return card.Plan{}, refuse(why.Reason, "release the cards one at a time: nova-sprint release <card>@<gen>")
	}
	return plan, nil
}

func live(c store.Card) bool {
	return (c.Kind == "work" || c.Kind == "read") && (c.State == card.Ready || c.State == card.Working)
}

func idList(cards []store.Card) string {
	if len(cards) == 0 {
		return "nothing"
	}
	ids := make([]string, len(cards))
	for i, c := range cards {
		ids[i] = fmtCard(c.ID, c.Gen)
	}
	return strings.Join(ids, ",")
}
