package verbs

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

type releaseRowInput struct {
	row, actor, reason string
	all                bool
}

func parseReleaseRow(args []string, actor string) (releaseRowInput, bool) {
	in := releaseRowInput{actor: actor}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		k := args[i]
		if seen[k] {
			return in, false
		}
		seen[k] = true
		if k == "--all" {
			in.all = true
			continue
		}
		if i+1 >= len(args) || args[i+1] == "" {
			return in, false
		}
		i++
		switch k {
		case "--row":
			in.row = args[i]
		case "--as":
			in.actor = args[i]
		case "--reason":
			in.reason = args[i]
		default:
			return in, false
		}
	}
	return in, in.row != "" && in.actor != "" && in.reason != "" && in.all
}
func (e *Engine) releaseRow(ctx context.Context, args []string) Result {
	in, ok := parseReleaseRow(args, e.Actor)
	if !ok {
		return refused("release", "invalid arguments", "nova-sprint help release")
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Env == nil || e.Now == nil || e.Preview == nil {
		return unavailable("release")
	}
	coordinator, err := e.Env.Coordinator(ctx)
	if err != nil {
		return operationalFailure("release", err)
	}
	if in.actor != coordinator {
		return refused("release", "only the coordinator can release a row", "ask "+coordinator+" over the bus")
	}
	hooked, recipe, err := e.Env.Hooked(ctx, in.actor)
	if err != nil {
		return operationalFailure("release", err)
	}
	if !hooked {
		return refused("release", in.actor+" is not hooked in", recipe)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure("release", err)
	}
	if _, ok := snap.Rows[in.row]; !ok {
		return refused("release", "unknown row "+in.row, "nova-sprint show "+in.row)
	}
	cards := rowLive(snap, in.row)
	var preview strings.Builder
	fmt.Fprintf(&preview, "RELEASE ROW %s epoch=%d consumers=%d\n", in.row, snap.Epoch, len(cards))
	for _, c := range cards {
		fmt.Fprintf(&preview, "withdraw %s@%d: nova-sprint release %s@%d --as %s --reason <why> --epoch %d\n", c.ID, c.Gen, c.ID, c.Gen, c.Holder, snap.Epoch)
	}
	if err = e.Preview(ctx, preview.String()); err != nil {
		return operationalFailure("release", err)
	}
	return e.releaseRowApply(ctx, st, snap, in, cards)
}
func rowLive(s store.Snapshot, row string) []store.Card {
	var out []store.Card
	for _, c := range s.Cards {
		if c.Holder == row && (c.Kind == "work" || c.Kind == "read") && (c.State == "ready" || c.State == "working") {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b store.Card) int { return strings.Compare(a.ID, b.ID) })
	return out
}
func (e *Engine) releaseRowApply(ctx context.Context, st ApplyStore, initial store.Snapshot, in releaseRowInput, cards []store.Card) Result {
	done := 0
	for _, c := range cards {
		err := e.releaseRowOne(ctx, st, initial.Epoch, in, c)
		if err != nil {
			return operationalFailure("release", &OperationError{Problem: fmt.Sprintf("released %d of %d; operation failed", done, len(cards)), Fix: "nova-sprint show " + in.row, Exit: 2})
		}
		done++
	}
	return Result{fmt.Sprintf("RELEASED row=%s consumers=%d\nnext: nothing; wait for the bus\n", in.row, done), 0}
}
func (e *Engine) releaseRowOne(ctx context.Context, st ApplyStore, epoch int, in releaseRowInput, original store.Card) error {
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return err
	}
	if snap.Epoch != epoch {
		return fmt.Errorf("epoch changed")
	}
	c, ok := snap.Cards[original.ID]
	if !ok || c.Gen != original.Gen || c.Holder != in.row || (c.State != "ready" && c.State != "working") {
		return fmt.Errorf("consumer %s changed", original.ID)
	}
	p, ok := snap.Cards[c.Of]
	if !ok {
		return fmt.Errorf("producer %s is missing", c.Of)
	}
	stream, ok := snap.Streams[p.Stream]
	if !ok {
		return fmt.Errorf("stream %s is missing", p.Stream)
	}
	plan, why := card.MoveConsumer(card.FromStore(c), card.FromStore(p), card.Row{}, card.Withdraw{Reason: in.reason, Stream: card.StreamFromStore(stream)})
	if why != nil {
		return fmt.Errorf("%s", why.Reason)
	}
	op := store.Op{ID: consumerOpID("release-row:"+in.actor, epoch, c.ID, c.Gen), Epoch: epoch, Actor: in.actor, Kind: "release row", At: e.Now(), Expect: plan.Expect, Writes: plan.Writes}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return err
	}
	if !rec.Accepted {
		return fmt.Errorf("%s", rec.Reason)
	}
	return nil
}
