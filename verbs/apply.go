package verbs

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// commit is the one way a verb writes: Apply, and on an unknown outcome the
// receipt the op id already got, else the same op again (CORRECT-USE,
// "Unknown outcomes never leak"). A refused receipt is a refusal naming fix.
func (e *Engine) commit(ctx context.Context, op store.Op, fix string) (store.Receipt, error) {
	r, err := applyOnce(ctx, e.Store, op, e.Store.Apply)
	if err == nil && !r.Accepted {
		return r, refuse(r.Reason, fix)
	}
	return r, err
}

// applyOnce is op through write (st.Apply, or st.Clear) exactly once: on an
// unknown outcome, the receipt its id already got, else the same op again.
func applyOnce(ctx context.Context, st store.Store, op store.Op, write func(context.Context, store.Op) (store.Receipt, error)) (store.Receipt, error) {
	r, err := write(ctx, op)
	if errors.Is(err, store.ErrUnknown) {
		var found bool
		if r, found, err = st.Replay(ctx, op.ID); err == nil && !found {
			r, err = write(ctx, op)
		}
	}
	return r, err
}

// newOp is an op of kind by actor on snap, its id fixed by what it asks (a
// repeat at the same seq replays rather than applying twice).
func (e *Engine) newOp(kind, actor string, snap store.Snapshot, what ...any) store.Op {
	body, _ := json.Marshal(append([]any{actor, snap.Epoch, snap.Seq}, what...))
	return store.Op{ID: fmt.Sprintf("%s.%x", kind, sha256.Sum256(body)), Epoch: snap.Epoch, Actor: actor, Kind: kind, At: e.Now()}
}

// consumerOpID is a row's op on a consumer, canonical from (epoch, verb,
// consumer, gen): nova-worker derives the same id (edge/worker), so a
// retried take or report replays.
func consumerOpID(kind string, epoch int, consumer string, gen int) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%d:%s:%d:%s:%d", len(kind), kind, epoch, consumer, gen))
	return fmt.Sprintf("consumer:%x", sum)
}

// rules are the active policy's numbers the verbs read: the roster's width
// cap and the consumer deadlines (SEAMS 4: policy owns run's numbers).
func (e *Engine) rules(ctx context.Context, snap store.Snapshot) (policy.Limits, policy.Timeouts, error) {
	a, err := e.answers(ctx, snap)
	return a.Limits, a.Timeouts, err
}

// answers are the active policy's argument-free answers (limits, timeouts,
// prices, catalog) at snap.
func (e *Engine) answers(ctx context.Context, snap store.Snapshot) (policy.Answers, error) {
	if e.Answers == nil {
		return policy.Answers{}, broken("the policy engine is not configured", "nova-sprint check")
	}
	vs := policy.Versions{Seq: snap.Seq}
	if snap.Policy > 0 {
		pv, err := e.Store.Policy(ctx, snap.Policy)
		if err != nil {
			return policy.Answers{}, err
		}
		vs.Active = &pv
	}
	a, failures := e.Answers.AnswerContext(ctx, vs, snap, roster.Roster{}, policy.Questions{})
	if a.Version < 0 {
		problem := "no policy version answered, not even the built-in"
		if len(failures) > 0 {
			problem += fmt.Sprintf(" (%s; %d failures)", clip(Redact(failures[0].Error())), len(failures))
		}
		return policy.Answers{}, broken(problem, "nova-sprint policy check v"+fmt.Sprint(snap.Policy))
	}
	return a, nil
}

// tierModels are the models row may run, by tier it serves: a friend's own
// model at each of its tiers; a bench's, the one the deal names for the
// tier (the catalog's bench model, policy.Catalog.BenchModel), which the
// probe proves its harness serves. A tier with none is an empty list (no
// bench deal is made for it).
func tierModels(ro roster.Roster, row string, cat policy.Catalog) map[string][]string {
	out := map[string][]string{}
	if k, ok := ro.Kin[row]; ok {
		for _, t := range k.Tiers {
			out[t] = nonNil(slices.DeleteFunc([]string{k.Model}, func(m string) bool { return m == "" }))
		}
		return out
	}
	for _, t := range ro.Workers[row].Tiers {
		out[t] = nonNil(slices.DeleteFunc([]string{cat.BenchModel(t)}, func(m string) bool { return m == "" }))
	}
	return out
}

// roster is nova-config's roster at the active policy's width cap, with
// each worker's width and tiers as its row advertised (roster.Advertised).
func (e *Engine) roster(ctx context.Context, lim policy.Limits, snap store.Snapshot) (roster.Roster, error) {
	if e.Roster == nil {
		return roster.Roster{}, broken("nova-config is not configured", "nova-sprint check")
	}
	ro, err := e.Roster.ReadRosterAt(ctx, max(1, lim.FleetWidthCap))
	if err != nil {
		return ro, RefusalOr(err, broken("nova-config's roster cannot be read ("+errText(err)+")", "nova-config check, then nova-sprint check"))
	}
	return ro.Advertised(snap.Rows), nil
}

// epochNow refuses a row's command copied from an epoch a clear ended.
func epochNow(given int, snap store.Snapshot) error {
	if given != 0 && given != snap.Epoch {
		return refuse(fmt.Sprintf("epoch %d is over: this card was recreated in epoch %d", given, snap.Epoch), "run nova-sprint next")
	}
	return nil
}
