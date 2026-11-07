package verbs

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// ApplyStore is the transactional store interface used by modifying verbs.
type ApplyStore interface {
	Reader
	Replay(context.Context, string) (store.Receipt, bool, error)
	Apply(context.Context, store.Op) (store.Receipt, error)
}

// ReportPlanner is the card machine's atomic consumer and producer report plan.
// It must fence the original generations and return one plan for one Apply.
type ReportPlanner interface {
	PlanReport(store.Snapshot, store.Card, card.Report) (card.Plan, *card.Refusal)
}

func cardRef(ref string) (string, int, bool) {
	i := strings.LastIndexByte(ref, '@')
	if i < 1 {
		return "", 0, false
	}
	gen, err := strconv.Atoi(ref[i+1:])
	return ref[:i], gen, err == nil && gen >= 0
}

func (e *Engine) take(ctx context.Context, args []string) Result {
	if len(args) < 1 {
		return refused("take", "missing card and generation", "nova-sprint help take")
	}
	id, gen, ok := cardRef(args[0])
	if !ok {
		return refused("take", "invalid card generation", "nova-sprint help take")
	}
	actor, epoch, valid := parseActor(args[1:], e.Actor)
	if !valid {
		return refused("take", "invalid arguments", "nova-sprint help take")
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Env == nil || e.Now == nil {
		return unavailable("take")
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return unavailable("take")
	}
	if epoch != 0 && epoch != snap.Epoch {
		return epochRefusal("take", epoch, snap.Epoch)
	}
	opID := consumerOpID("take", snap.Epoch, id, gen)
	if result, found := replay(ctx, st, opID, actor, "take", id); found {
		return result
	}
	return e.takeNew(ctx, st, snap, id, gen, actor, opID)
}
func (e *Engine) takeNew(ctx context.Context, st ApplyStore, snap store.Snapshot, id string, gen int, actor, opID string) Result {
	hooked, recipe, err := e.Env.Hooked(ctx, actor)
	if err != nil {
		return unavailable("take")
	}
	if !hooked {
		return refused("take", actor+" is not hooked in", recipe)
	}
	c, exists := snap.Cards[id]
	if !exists || c.Gen != gen || c.Holder != actor {
		return refused("take", id+" is not dealt to this row at this generation", "nova-sprint show "+id)
	}
	p, exists := snap.Cards[c.Of]
	if !exists {
		return unavailable("take")
	}
	r, exists := snap.Rows[actor]
	if !exists {
		return refused("take", "unknown row "+actor, "nova-sprint show "+actor)
	}
	width, tiers, result, ok := e.takeRoster(ctx, snap, c, actor)
	if !ok {
		return result
	}
	plan, why := card.MoveConsumer(card.FromStore(c), card.FromStore(p), card.RowFromStore(r, width, tiers), card.Take{Row: actor})
	if why != nil {
		return refused("take", why.Reason, "nova-sprint show "+id)
	}
	plan.Expect = append(plan.Expect, store.AtSeq{Seq: snap.Seq})
	op := store.Op{ID: opID, Epoch: snap.Epoch, Actor: actor, Kind: "take", At: e.Now(), Expect: plan.Expect, Writes: plan.Writes, Note: snap.Streams[c.Stream].Repo}
	receipt, err := applyExact(ctx, st, op)
	if err != nil {
		return unavailable("take")
	}
	return receiptResult(receipt, actor, "take", id)
}

func parseActor(args []string, actor string) (string, int, bool) {
	epoch := 0
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) || args[i+1] == "" {
			return "", 0, false
		}
		switch args[i] {
		case "--as":
			actor = args[i+1]
		case "--epoch":
			var err error
			epoch, err = strconv.Atoi(args[i+1])
			if err != nil || epoch < 1 {
				return "", 0, false
			}
		default:
			return "", 0, false
		}
		i++
	}
	return actor, epoch, actor != ""
}

func epochRefusal(verb string, old, current int) Result {
	return refused(verb, fmt.Sprintf("epoch %d is over: this card was recreated in epoch %d", old, current), "run nova-sprint next")
}

func replay(ctx context.Context, st ApplyStore, id, actor, kind, cardID string) (Result, bool) {
	r, found, err := st.Replay(ctx, id)
	if err != nil {
		return unavailable(kind), true
	}
	if !found {
		return Result{}, false
	}
	return receiptResult(r, actor, kind, cardID), true
}

func receiptResult(r store.Receipt, actor, kind, id string) Result {
	if r.Actor != actor || r.Kind != kind {
		return refused(kind, "operation cannot be performed", "nova-sprint next --as "+actor)
	}
	if !r.Accepted {
		return refused(kind, r.Reason, "nova-sprint show "+id)
	}
	if kind == "take" && r.After != nil {
		if c, ok := r.After.Cards[id]; ok {
			return takenResult(c, r.Note, actor, r.Epoch)
		}
	}
	next := "nova-sprint next --as " + actor
	if (kind == "release" || kind == "report") && r.Note != "" {
		next = r.Note
	}
	return Result{fmt.Sprintf("%s applied at seq %d\nnext: %s\n", strings.ToUpper(kind), r.Seq, next), 0}
}

// applyExact retries only the identical operation when the result is unknown.
func applyExact(ctx context.Context, st ApplyStore, op store.Op) (store.Receipt, error) {
	if err := fenceRole(ctx, &op); err != nil {
		return store.Receipt{}, err
	}
	r, err := st.Apply(ctx, op)
	if !errors.Is(err, store.ErrUnknown) {
		return r, err
	}
	r, found, err := st.Replay(ctx, op.ID)
	if err != nil || found {
		return r, err
	}
	return st.Apply(ctx, op)
}

// Clock returns a caller-injected time; unit tests use a fake clock.
type Clock func() time.Time

func consumerOpID(kind string, epoch int, id string, gen int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%d:%s:%d", len(kind), kind, epoch, id, gen)))
	return fmt.Sprintf("consumer:%x", sum)
}

func takenResult(c store.Card, repo, actor string, epoch int) Result {
	command := fmt.Sprintf("nova-sprint finish %s@%d --head <sha> --as %s", c.ID, c.Gen, actor) + fmt.Sprintf(" --epoch %d", epoch)
	if c.Kind == "read" {
		command = fmt.Sprintf("nova-sprint read %s@%d yes|fix --as %s", c.ID, c.Gen, actor) + fmt.Sprintf(" --epoch %d", epoch)
		return Result{fmt.Sprintf("TAKEN %s repo=%s head=%s\nRead detached at this head; do not edit or push.\n%s\nnext: %s\n", c.ID, repo, c.Head, c.Brief, command), 0}
	}
	return Result{fmt.Sprintf("TAKEN %s repo=%s base=%s branch=%s\n%s\nnext: %s\n", c.ID, repo, c.BaseSHA, c.Branch, c.Brief, command), 0}
}
