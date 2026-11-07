package verbs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/docs"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func hookInput(args []string) (string, string, bool) {
	var name, role string
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) || args[i+1] == "" || seen[args[i]] {
			return "", "", false
		}
		seen[args[i]] = true
		switch args[i] {
		case "--as":
			name = args[i+1]
		case "--role":
			role = args[i+1]
		default:
			return "", "", false
		}
		i++
	}
	return name, role, validSetupActor(name) && role != ""
}
func newHookSession() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

// Hook records an existing live bus name, then prints its role playbook.
// Bootstrap and the CLI both call this same operation.
func (e *Engine) Hook(ctx context.Context, name, role string) Result {
	if !validSetupActor(name) {
		return refused("hook", "invalid arguments", "nova-sprint help hook")
	}
	if role != "worker" && role != "coordinator" {
		return refused("hook", "unknown role "+role, "choose coordinator or worker")
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Env == nil || e.Now == nil {
		return unavailable("hook")
	}
	present, recipe, err := e.Env.Hooked(ctx, name)
	if err != nil {
		return operationalFailure("hook", err)
	}
	if !present {
		return busHookRefusal("hook", name, recipe)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure("hook", err)
	}
	if row, found := snap.Rows[name]; found && row.Session != "" {
		return refused("hook", name+" is already hooked", "nova-sprint unhook --as "+name+" then nova-sprint hook --as "+name+" --role "+role)
	}
	session, err := newHookSession()
	if err != nil {
		return unavailable("hook")
	}
	op := store.Op{ID: consumerOpID("hook:"+name, snap.Epoch, session, int(snap.Seq)), Epoch: snap.Epoch, Actor: name, Kind: "hook", At: e.Now(), Expect: []store.Expect{store.AtSeq{Seq: snap.Seq}}, Writes: []store.Write{store.PutRow{Name: name, Session: session, Role: role}}}
	receipt, err := applyExact(ctx, st, op)
	if err != nil {
		return operationalFailure("hook", err)
	}
	if !receipt.Accepted {
		return refused("hook", receipt.Reason, "nova-sprint hook --as "+name+" --role "+role)
	}
	text, _ := docs.Playbook(role)
	return Result{fmt.Sprintf("%s\nHOOKED %s as %s\nnext: nova-sprint next --as %s\n", text, name, role, name), 0}
}

func unhookInput(args []string) (target, actor string, ok bool) {
	for i := 0; i < len(args); i++ {
		if args[i] == "--as" {
			if actor != "" || i+1 >= len(args) {
				return "", "", false
			}
			i++
			actor = args[i]
			continue
		}
		if target != "" || args[i] == "" || args[i][0] == '-' {
			return "", "", false
		}
		target = args[i]
	}
	if target == "" {
		target = actor
	}
	return target, actor, validSetupActor(actor) && validSetupActor(target)
}
func (e *Engine) unhook(ctx context.Context, args []string) Result {
	target, actor, ok := unhookInput(args)
	if !ok {
		return refused("unhook", "invalid arguments", "nova-sprint help unhook")
	}
	if target != actor {
		ctx = context.WithValue(ctx, roleRequiredKey{}, true)
		var result Result
		ctx, result, ok = e.authorizeRole(ctx, actor, "coordinator", "unhook")
		if !ok {
			return result
		}
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Now == nil {
		return unavailable("unhook")
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure("unhook", err)
	}
	row, ok := snap.Rows[target]
	if !ok || row.Session == "" {
		return refused("unhook", target+" is not hooked", "nova-sprint hook --as "+target+" --role worker")
	}
	op := store.Op{ID: consumerOpID("unhook:"+actor, snap.Epoch, target, int(snap.Seq)), Epoch: snap.Epoch, Actor: actor, Kind: "unhook", At: e.Now(), Expect: []store.Expect{store.AtSeq{Seq: snap.Seq}}, Writes: []store.Write{store.PutRow{Name: target}}}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return operationalFailure("unhook", err)
	}
	if !rec.Accepted {
		return refused("unhook", rec.Reason, unhookCommand(target, actor))
	}
	return Result{fmt.Sprintf("UNHOOKED %s\nnext: nova-sprint hook --as %s --role worker\n", target, target), 0}
}

// EnsureCoordinatorHook is the idempotent setup boundary after step 2.
func (e *Engine) EnsureCoordinatorHook(ctx context.Context, in SetupInput) Result {
	if e.Store == nil || e.Env == nil {
		return unavailable("up")
	}
	snap, err := e.Store.Snapshot(ctx)
	if err != nil {
		return operationalFailure("up", err)
	}
	if row, ok := snap.Rows[in.Actor]; ok && row.Session != "" {
		present, recipe, err := e.Env.Hooked(ctx, in.Actor)
		if err != nil {
			return operationalFailure("up", err)
		}
		if !present {
			return busHookRefusal("up", in.Actor, recipe)
		}
		if row.Role != "coordinator" {
			return refused("up", in.Actor+" is hooked as worker", "nova-sprint unhook --as "+in.Actor+" then nova-sprint hook --as "+in.Actor+" --role coordinator")
		}
		return Result{}
	}
	result := e.Hook(ctx, in.Actor, "coordinator")
	if result.Exit != 0 {
		return result
	}
	result.Output = "hooked you in as coordinator; the playbooks follow\n" + result.Output
	return result
}

func busHookRefusal(verb, name, recipe string) Result {
	if recipe != "" && recipe != "nova-bus hook --as "+name {
		return refused(verb, "hook in to the bus first", recipe)
	}
	return refused(verb, "hook in to the bus first", "run NOVA_BUS_REDIS=<addr> nova-bus hook --as "+name+" as the Monitor tool command, then hook in to the sprint again")
}
func unhookCommand(target, actor string) string {
	if target == actor {
		return "nova-sprint unhook --as " + actor
	}
	return "nova-sprint unhook " + target + " --as " + actor
}
