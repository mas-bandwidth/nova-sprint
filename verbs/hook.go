package verbs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/docs"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// runHook is `hook` (Stella's rulings, 2026-11-06): the name on the bus
// first; the sprint mints the session; one hook per name and one
// coordinator per sprint; a worker only once the sprint has a commit
// identity; the identity's two git config lines and the role's playbook
// printed. A worker's restart
// is the bus hook, its own unhook, this hook, then next.
func runHook(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	r := in.str("role")
	if r != "worker" && r != "coordinator" {
		return "", misuse("hook", "--role is worker or coordinator")
	}
	if err := e.onBus(ctx, in.actor); err != nil {
		return "", err
	}
	snap, err := e.snapshot(ctx) // the store credential finish and read need: without it, CredentialFix
	if err != nil {
		return "", err
	}
	if r == "worker" && !snap.Identity.Set() { // nothing commits until the sprint knows as whom
		return "", noIdentity()
	}
	if err := e.originsReadable(ctx, snap); err != nil {
		return "", err
	}
	return e.hookAs(ctx, snap, in.actor, r)
}

// originsReadable is hook's admission (intent rule 34): the hooking
// process has what finish and read will need. The store credential is
// proved by the read before this; finish also reads the card's head and
// the group branch at the stream's origin, so every stream's repository
// must be readable from this process, all read at once. No forge token is
// asked of a row: the run does the forge work.
func (e *Engine) originsReadable(ctx context.Context, snap store.Snapshot) error {
	ss := streamsInOrder(snap)
	errs := make([]error, len(ss))
	var wg sync.WaitGroup
	for i, s := range ss {
		wg.Go(func() { _, errs[i] = e.baseTip(ctx, s, "hook again") })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			f := *classify(err)
			f.What = "finish and read could not reach stream " + ss[i].Name + " from this process: " + f.What
			return &f
		}
	}
	return nil
}

// hookAs records name's session and role on snap and prints the sprint's
// commit identity (the two git config lines) and its playbook:
// the hook verb's, and up's and restore's for their coordinator.
func (e *Engine) hookAs(ctx context.Context, snap store.Snapshot, name, r string) (string, error) {
	if row := snap.Rows[name]; row.Session != "" {
		return "", refuse(name+" is already hooked in as "+row.Role, "nova-sprint unhook --as "+name+", then hook again")
	}
	if c := coordinatorOf(snap); r == "coordinator" && c != "" {
		return "", refuse("the sprint's coordinator is "+c, "ask "+c+" to run nova-sprint unhook --as "+c+" first")
	}
	session, err := e.newSession()
	if err != nil {
		return "", err
	}
	op := e.newOp("hook", name, snap, session, r)
	op.Writes = []store.Write{store.PutRow{Name: name, Session: session, Role: r}}
	if r == "coordinator" { // one coordinator: no other hook may land between the read and this op
		op.Expect = []store.Expect{store.AtSeq{Seq: snap.Seq}}
	}
	if _, err := e.commit(ctx, op, "nova-sprint unhook --as "+name+", then hook again"); err != nil {
		return "", err
	}
	text, _ := docs.Playbook(r)
	return done("nova-sprint next --as "+name, fmt.Sprintf("HOOKED %s role=%s", name, r), identityLines(snap), text), nil
}

func (e *Engine) newSession() (string, error) {
	if e.Session != nil {
		return e.Session()
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", RefusalOr(err, broken("no randomness for a session ("+errText(err)+")", "run nova-sprint hook again"))
	}
	return hex.EncodeToString(b[:]), nil
}

// runUnhook is `unhook`: one's own hook needs nothing (a worker's restart
// starts with it); another row's is the coordinator's.
func runUnhook(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	target := in.arg(0)
	if target == "" || target == in.actor {
		if in.actor == "" {
			return "", refuse("no name given", "pass --as <you>, or set NOVA_SPRINT_AS=<you>")
		}
		snap, err := e.snapshot(ctx)
		if err != nil {
			return "", err
		}
		return e.unhook(ctx, snap, in.actor, in.actor)
	}
	snap, err := e.hooked(ctx, in, roleCoordinator)
	if err != nil {
		return "", err
	}
	return e.unhook(ctx, snap, target, in.actor)
}

func (e *Engine) unhook(ctx context.Context, snap store.Snapshot, target, actor string) (string, error) {
	if snap.Rows[target].Session == "" && target == actor { // a restart's unhook: already true
		return done("nova-sprint hook --as "+target+" --role worker", target+" is not hooked in; nothing to unhook"), nil
	}
	if snap.Rows[target].Session == "" {
		return "", refuse(target+" is not hooked in", "nova-sprint show "+target)
	}
	op := e.newOp("unhook", actor, snap, target)
	op.Expect = []store.Expect{store.RowGen{Row: target, Gen: snap.Rows[target].Gen}}
	op.Writes = []store.Write{store.PutRow{Name: target}}
	if _, err := e.commit(ctx, op, "nova-sprint show "+target); err != nil {
		return "", err
	}
	return done("nova-sprint hook --as "+target+" --role worker", "UNHOOKED "+target), nil
}
