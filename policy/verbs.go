package policy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// The verbs' logic (docs/LAYER-5-POLICY.md, "The verbs"), called by layer
// 6's verbs: checked before stored, stored before active, every change an
// event through the store's policy writes. A refusal is a *Refusal in the
// CORRECT-USE form; any other error is the store's.

// Caller is who runs a verb and the op it may write. Coordinator is layer
// 6's answer for Actor (the rows nova-config names).
type Caller struct {
	Actor       string
	Coordinator bool
	OpID        string
	At          time.Time
}

// Refusal is a verb's refusal: SPRINT REFUSED policy <verb>: <what>: <fix>.
type Refusal struct{ Verb, What, Fix string }

func (r *Refusal) Error() string {
	return "SPRINT REFUSED policy " + r.Verb + ": " + r.What + ": " + r.Fix
}

// Result is what a verb that writes did, and the next command.
type Result struct {
	Version int
	Changes int // activate: the answers that change on the current snapshot
	Next    string
}

// Up stores the built-in default as the next version and activates it, in
// one op fenced at the snapshot's seq, when no version is active; otherwise
// it changes nothing. A data row stored first (stream data) is only counted
// (Emma's probe: up once left no active version after one).
func Up(ctx context.Context, st store.Store, c Caller) (Result, error) {
	snap, err := st.Snapshot(ctx)
	if err != nil || snap.Policy > 0 {
		return Result{Version: snap.Policy, Next: "nova-sprint policy show"}, err
	}
	all, err := versions(ctx, st)
	if err != nil {
		return Result{}, err
	}
	v := len(all) + 1
	op := store.Op{ID: c.OpID, Epoch: snap.Epoch, Actor: c.Actor, Kind: "policy-set", At: c.At, Note: "up: the built-in default",
		Expect: []store.Expect{store.AtSeq{Seq: snap.Seq}},
		Writes: []store.Write{store.PutPolicy{Kind: "script", Name: "default.star", Body: defaultBody}, store.SetActivePolicy{Version: v}}}
	_, err = apply(ctx, st, op, "up")
	return Result{Version: v, Next: "nova-sprint policy show"}, err
}

// Set compiles the file, checks its names and arity, runs it on the current
// snapshot, and stores it as the next version, not active (tla/Policy.tla Set).
func Set(ctx context.Context, st store.Store, e *Engine, c Caller, path, reason string) (Result, error) {
	no := func(what, fix string) (Result, error) { return Result{}, &Refusal{"set", what, fix} }
	if r := coordinator("set", c, reason); r != nil {
		return Result{}, r
	}
	name, body, r := readFile("set", path)
	if r != nil {
		return Result{}, r
	}
	snap, err := st.Snapshot(ctx)
	if err != nil || snap.Policy == 0 {
		return Result{}, upFirst("set", err)
	}
	all, err := versions(ctx, st)
	if err != nil {
		return Result{}, err
	}
	if i := slices.IndexFunc(all, func(v store.PolicyVersion) bool { return v.Body == body }); i >= 0 {
		return no(fmt.Sprintf("v%d has this body", all[i].Version), fmt.Sprintf("nova-sprint policy activate %d", all[i].Version))
	}
	if _, fs := e.runFile(name, body, snap); len(fs) > 0 {
		return no(fs[0].Error(), "nova-sprint policy check "+path)
	}
	op := store.Op{ID: c.OpID, Epoch: snap.Epoch, Actor: c.Actor, Kind: "policy-set", At: c.At, Note: reason,
		Writes: []store.Write{store.PutPolicy{Kind: "script", Name: name, Body: body}}}
	after, err := apply(ctx, st, op, "set")
	for v := range after.Policies { // the one version the op stored
		return Result{Version: v, Next: fmt.Sprintf("nova-sprint policy check v%d", v)}, nil
	}
	return Result{}, err
}

// Activate re-runs check on the current snapshot, then in one op moves the
// active flag to v and answers the open policy judgements of the old
// version and of v (v now runs clean). It writes no card (tla/Policy.tla
// Activate, ActivateMovesNoCard).
func Activate(ctx context.Context, st store.Store, e *Engine, c Caller, v int, reason string) (Result, error) {
	no := func(what, fix string) (Result, error) { return Result{}, &Refusal{"activate", what, fix} }
	if r := coordinator("activate", c, reason); r != nil {
		return Result{}, r
	}
	pv, err := script(ctx, st, "activate", v)
	if err != nil {
		return Result{}, err
	}
	if pv.Active {
		return no(fmt.Sprintf("policy version %d is already active", v), "nova-sprint policy show")
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return Result{}, err
	}
	rep, err := check(ctx, st, e, snap, pv)
	if err != nil {
		return Result{}, err
	}
	if len(rep.Refusals) > 0 {
		return no(strings.Join(rep.Refusals, "; "), "nova-sprint policy check "+strconv.Itoa(v))
	}
	writes := []store.Write{store.SetActivePolicy{Version: v}}
	for _, id := range append(OpenJudgements(snap, snap.Policy), OpenJudgements(snap, v)...) {
		writes = append(writes, store.AnswerJudgement{ID: id, Answer: fmt.Sprintf("v%d activated: %s", v, reason)})
	}
	op := store.Op{ID: c.OpID, Epoch: snap.Epoch, Actor: c.Actor, Kind: "policy-activate", At: c.At, Note: reason, Writes: writes}
	_, err = apply(ctx, st, op, "activate")
	return Result{Version: v, Changes: len(rep.Changes), Next: "nova-sprint next"}, err
}

func coordinator(verb string, c Caller, reason string) *Refusal {
	switch {
	case !c.Coordinator:
		return &Refusal{verb, "policy is the coordinator's", "ask over the bus: nova-bus send --as " + c.Actor + " --to <coordinator>"}
	case strings.TrimSpace(reason) == "":
		return &Refusal{verb, "no reason given", "nova-sprint policy " + verb + " ... --reason <why>"}
	}
	return nil
}

func upFirst(verb string, err error) error {
	if err != nil {
		return err
	}
	return &Refusal{verb, "no policy is active", "nova-sprint up"}
}

// readFile reads a policy file of at most MaxBody bytes, reading no more
// than one byte past it; its name is the base name.
func readFile(verb, path string) (string, string, *Refusal) {
	f, err := os.Open(path)
	var b []byte
	if err == nil {
		b, err = io.ReadAll(io.LimitReader(f, MaxBody+1))
		_ = f.Close() // ignored: read-only, and everything needed is read
	}
	switch {
	case err != nil:
		return "", "", &Refusal{verb, "cannot read " + path + ": " + err.Error(), "name a readable file"}
	case len(b) > MaxBody:
		return "", "", &Refusal{verb, fmt.Sprintf("%s is over %d bytes", path, MaxBody), "make the file smaller"}
	}
	return filepath.Base(path), string(b), nil
}

// apply applies a verb's op: the touched rows after it, or the store's
// refusal as the verb's.
func apply(ctx context.Context, st store.Store, op store.Op, verb string) (*store.State, error) {
	r, err := st.Apply(ctx, op)
	switch {
	case err != nil:
		return &store.State{}, err
	case !r.Accepted:
		return &store.State{}, &Refusal{verb, r.Reason, "nova-sprint policy show --list"}
	}
	return r.After, nil
}

// version is stored version v, or the refusal naming the list.
func version(ctx context.Context, st store.Store, verb string, v int) (store.PolicyVersion, error) {
	pv, err := st.Policy(ctx, v)
	if errors.Is(err, store.ErrNotFound) || v < 0 {
		return pv, &Refusal{verb, fmt.Sprintf("policy version %d does not exist", v), "nova-sprint policy show --list"}
	}
	return pv, err
}

// script is stored version v of kind script: a data row (stream data) is
// refused by every verb that runs a version (activate, check, catalog).
func script(ctx context.Context, st store.Store, verb string, v int) (store.PolicyVersion, error) {
	pv, err := version(ctx, st, verb, v)
	if err == nil && pv.Kind != "script" {
		err = &Refusal{verb, fmt.Sprintf("v%d is of kind %s, which policy does not run", v, pv.Kind), "name a script version: nova-sprint policy show --list"}
	}
	return pv, err
}

// versions are every stored version, oldest first: versions are numbered
// from 1 without gaps and never deleted.
func versions(ctx context.Context, st store.Store) ([]store.PolicyVersion, error) {
	var out []store.PolicyVersion
	for v := 1; ; v++ {
		pv, err := st.Policy(ctx, v)
		if errors.Is(err, store.ErrNotFound) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, pv)
	}
}

// LoadVersions are the active version and the one active before it, read
// from the store (the previous from the log's last activation; PrevOf).
func LoadVersions(ctx context.Context, st store.Store) (Versions, error) {
	snap, err := st.Snapshot(ctx)
	if err != nil || snap.Policy == 0 {
		return Versions{}, err
	}
	var vs Versions
	active, err := st.Policy(ctx, snap.Policy)
	if err != nil {
		return vs, err
	}
	vs.Active = &active
	prev := 0
	err = walk(ctx, st, snap.Seq, func(e store.Event) {
		if p, ok := PrevOf(e); ok {
			prev = p
		}
	})
	if err != nil || prev == 0 {
		return vs, err
	}
	p, err := st.Policy(ctx, prev)
	vs.Prev = &p
	return vs, err
}

// PrevOf is, for an accepted event that changed the active version, the
// version active before it; layer 6 calls it as it folds events.
func PrevOf(e store.Event) (int, bool) {
	if !e.Accepted || e.Before == nil || e.After == nil {
		return 0, false
	}
	for v, p := range e.Before.Policies {
		if p.Active && !e.After.Policies[v].Active {
			return v, true
		}
	}
	return 0, false
}

// walk calls fn on every event in seq order up to seq to (a snapshot's,
// so what it folds is what the snapshot holds: Emma's probe), or every
// event when to is 0.
func walk(ctx context.Context, st store.Store, to int64, fn func(store.Event)) error {
	for after := int64(0); ; {
		es, err := st.Events(ctx, after, store.MaxEvents)
		if err != nil || len(es) == 0 {
			return err
		}
		for _, e := range es {
			if to > 0 && e.Seq > to {
				return nil
			}
			fn(e)
		}
		after = es[len(es)-1].Seq
	}
}
