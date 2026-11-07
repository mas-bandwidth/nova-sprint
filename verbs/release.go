package verbs

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// runRelease is `release` (Stella's ruling): a row gives back its own held
// card with a reason; another holder's card, or a whole row's (--row), is
// the coordinator's. The consumer is withdrawn and its branch kept.
func runRelease(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	reason := in.str("reason")
	if reason == "" {
		return "", refuse("a release needs its reason", "add --reason \"<why>\"")
	}
	if row := in.str("row"); row != "" {
		return e.releaseRow(ctx, in, snap, row, reason)
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
	opID, err := machineID(in, snap, "release", id, gen)
	if err != nil {
		return "", err
	}
	plan, err := withdraw(snap, []store.Card{c}, reason)
	if err != nil {
		return "", err
	}
	op := store.Op{ID: opID, Epoch: snap.Epoch, Actor: in.actor, Kind: "release", At: e.Now(), Expect: plan.Expect, Writes: plan.Writes, Note: reason}
	r, err := e.commit(ctx, op, "nova-sprint show "+id)
	if err != nil {
		return "", err
	}
	return done(waitNext, fmt.Sprintf("RELEASED %s applied at seq %d; branch %s kept", id, r.Seq, c.Branch)), nil
}

// releaseRow withdraws every live consumer of row in one op, the
// coordinator's, after printing what it withdraws.
func (e *Engine) releaseRow(ctx context.Context, in *input, snap store.Snapshot, row, reason string) (string, error) {
	if snap.Rows[in.actor].Role != "coordinator" {
		return "", notCoordinator("release --row", in.actor, snap)
	}
	if _, ok := snap.Rows[row]; !ok {
		return "", refuse("no row "+row, "nova-sprint show")
	}
	if !in.has("all") {
		return "", misuse("release", "--row releases every card the row holds: confirm with --all")
	}
	cards := liveOf(snap, row)
	what := fmt.Sprintf("RELEASE ROW %s withdraws %s", row, idList(cards))
	if len(cards) == 0 {
		return done(waitNext, "RELEASE ROW "+row+" holds nothing"), nil
	}
	if in.has("dry-run") {
		return done("nova-sprint release --row "+row+" --all --reason \"<why>\"", what+" (dry run)"), nil
	}
	plan, err := withdraw(snap, cards, reason)
	if err != nil {
		return "", err
	}
	op := e.newOp("release-row", in.actor, snap, row, reason)
	op.Expect, op.Writes, op.Note = plan.Expect, plan.Writes, reason
	if _, err := e.commit(ctx, op, "nova-sprint show "+row); err != nil {
		return "", err
	}
	return done(waitNext, what, fmt.Sprintf("RELEASED row=%s consumers=%d", row, len(cards))), nil
}

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
		ev := card.Withdraw{Reason: reason, Head: head, Stream: card.StreamFromStore(snap.Streams[p.Stream])}
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

// liveOf are row's live consumers, by id.
func liveOf(snap store.Snapshot, row string) []store.Card {
	var out []store.Card
	for _, c := range snap.Cards {
		if c.Holder == row && live(c) {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b store.Card) int { return strings.Compare(a.ID, b.ID) })
	return out
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
