package verbs

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
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

// fenceTries is how many times a verb applies its op on a store that others
// write between its read and its commit: the first, and four plans again on
// a fresh read. A verb's read-to-commit window is one plan (milliseconds);
// live, one bench report or deal landed in it, never two (2026-11-10: replace
// refused on a card the run dealt in it, hold on a bench report). Five
// refusals in a row each by a change the verb did not read mean the store is
// moving faster than the verb can plan; it stops and says so rather than
// spin, and no sleep is taken between tries (each is a fresh read).
const fenceTries = 5

// fenced is the op plan makes from snap, committed. An op refused with a
// write by another between its read and its commit (the refused op's own
// event is not the next after the read) is planned again on a fresh read
// and committed again, up to fenceTries in all, while own(was, now) is "":
// what the verb itself read (its target, the epoch, the caller's hook) is
// as it was on was, the verb's first read, so the refusal was a write to a
// record the verb neither read nor wrote. own naming a change is the
// refusal, with the store's reason, the last try's included; so is plan
// refusing on the fresh read (its precondition no longer holds, named),
// and so is an op refused with nothing written between: no fence was
// tripped, the op itself was refused. It returns the receipt and the op it
// committed.
func (e *Engine) fenced(ctx context.Context, snap store.Snapshot, fix string, own func(was, now store.Snapshot) string, plan func(store.Snapshot) (store.Op, error)) (store.Receipt, store.Op, error) {
	was := snap
	for try := 1; ; try++ {
		op, err := plan(snap)
		if err != nil {
			return store.Receipt{}, op, err
		}
		r, err := applyOnce(ctx, e.Store, op, e.Store.Apply)
		if err != nil || r.Accepted {
			return r, op, err
		}
		if r.Seq <= snap.Seq+1 {
			return r, op, refuse(r.Reason, fix)
		}
		if snap, err = e.snapshot(ctx); err != nil {
			return r, op, err
		}
		if what := cmp.Or(epochChange(was, snap), own(was, snap)); what != "" {
			return r, op, refuse(r.Reason+"; "+what, fix)
		}
		if try == fenceTries {
			return r, op, refuse(fmt.Sprintf("%s (refused %d times, each by a write to records this verb did not read: the store moves faster than it can plan)", r.Reason, try), fix)
		}
	}
}

// epochChange is the epoch moving under a verb (a clear): every verb's own.
func epochChange(was, now store.Snapshot) string {
	if was.Epoch == now.Epoch {
		return ""
	}
	return fmt.Sprintf("the sprint moved from epoch %d to %d since this verb read it", was.Epoch, now.Epoch)
}

// owns is the first change any of checks names, or "".
func owns(checks ...func(was, now store.Snapshot) string) func(was, now store.Snapshot) string {
	return func(was, now store.Snapshot) string {
		for _, c := range checks {
			if what := c(was, now); what != "" {
				return what
			}
		}
		return ""
	}
}

// hookChange is the caller's own hook changed: a verb fenced on the hooks
// (UnchangedSince Hooks) so that its caller is still the coordinator it was
// read as is refused by any row's hook; only the caller's is its own.
func hookChange(actor string) func(was, now store.Snapshot) string {
	return func(was, now store.Snapshot) string {
		a, b := was.Rows[actor], now.Rows[actor]
		if a.Session == b.Session && a.Role == b.Role {
			return ""
		}
		return fmt.Sprintf("row %s's hook changed since this verb read it (session %s role %s, now session %s role %s)", actor, orWord(a.Session, "none"), orWord(a.Role, "none"), orWord(b.Session, "none"), orWord(b.Role, "none"))
	}
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

// consumerOpID is store.ConsumerOpID: a row's op on a consumer.
var consumerOpID = store.ConsumerOpID

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
	return ro.Advertised(snap.Rows, ""), nil // the probe asks every tier its row serves; check names a stale binary
}

// epochNow refuses a row's command copied from an epoch a clear ended.
func epochNow(given int, snap store.Snapshot) error {
	if given != 0 && given != snap.Epoch {
		return refuse(fmt.Sprintf("epoch %d is over: the sprint is in epoch %d", given, snap.Epoch), "run nova-sprint next")
	}
	return nil
}
