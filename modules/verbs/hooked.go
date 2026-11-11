package verbs

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// hooked is the one hooked check (CORRECT-USE, "Hooked before anything that
// acts as you"; DO NOT REMOVE without Glenn, LESSONS W.17): a name, on the
// bus, hooked in to the sprint, in the role the verb needs. It returns the
// snapshot it read, with the cards scope names, which the verb acts on.
// The sprint hook is bound to the bus hook it was made on: the row's
// session is the id nova-bus who showed for the name at the hook, and a
// name whose bus hook was started since (who shows another id: a new
// session, or a restart) is not hooked in, whatever its row says. So a
// session that forgets to hook in is told on its first verb, in the role
// its row has, and the hook hands it its playbook (tla/Hook.tla,
// NoStaleVerb; its witness HookAnySession is the gate without this).
func (e *Engine) hooked(ctx context.Context, in *input, need role, scope store.Scope) (store.Snapshot, error) {
	live, err := e.busSession(ctx, in.actor)
	if err != nil {
		return store.Snapshot{}, err
	}
	snap, err := e.read(ctx, scope)
	if err != nil {
		return snap, err
	}
	row := snap.Rows[in.actor]
	if row.Session != live {
		return snap, refuse("hook in first", "nova-sprint hook --as "+in.actor+" --role "+cmp.Or(row.Role, roleWord(need)))
	}
	if need == roleCoordinator && row.Role != "coordinator" {
		return snap, notCoordinator(in.verb, in.actor, snap)
	}
	return snap, nil
}

// onBus is a name given and present in nova-bus who: the bus first, then the
// sprint (Glenn, 2026-11-06).
func (e *Engine) onBus(ctx context.Context, name string) error {
	_, err := e.busHook(ctx, name)
	return err
}

// busSession is onBus, and the session id nova-bus who shows for name's bus
// hook now: what the sprint hook records and the hooked check compares. It
// is who's word for the name, nothing about the process that asks: the bus
// hook may be a service beside the session (nova-bus help hook) or a
// worker's child. A hook that shows no id cannot be bound to.
func (e *Engine) busSession(ctx context.Context, name string) (string, error) {
	p, err := e.busHook(ctx, name)
	if err == nil && p.Session == "" {
		err = refuse(name+"'s bus hook shows no session id in nova-bus who", "start it again with nova-tools "+cmp.Or(e.ToolPin, "at this build's pin")+" or later: "+busRecipe(name))
	}
	return p.Session, err
}

// busHook is name's line in nova-bus who, refused for no name, a name the
// sprint does not take, or one not on the bus.
func (e *Engine) busHook(ctx context.Context, name string) (bus.Presence, error) {
	if name == "" {
		return bus.Presence{}, refuse("no name given", "pass --as <you>, or set NOVA_SPRINT_AS=<you>")
	}
	if !validName(name) {
		return bus.Presence{}, refuse(clip(name)+" is not a name the sprint takes", "use 1 to 128 of a-z A-Z 0-9 . _ -")
	}
	p, present, err := e.present(ctx, name)
	if err != nil {
		return p, err
	}
	if !present {
		return p, refuse(name+" is not on the bus", busRecipe(name))
	}
	return p, nil
}

// present is name in nova-bus who, the one presence test (the run loop reads
// the same who for its deals and silences); a who that fails is the failure,
// named with how name hooks in.
func (e *Engine) present(ctx context.Context, name string) (bus.Presence, bool, error) {
	if e.Bus == nil {
		return bus.Presence{}, false, broken("nova-bus is not configured", "set NOVA_BUS_REDIS, then nova-sprint check")
	}
	who, err := e.Bus.Who(ctx)
	var run *edge.ExecError
	switch {
	case err != nil && errors.As(err, &run) && AsRefusal(err) == nil:
		// nova-bus never ran: that is the cause, named first
		return bus.Presence{}, false, broken("cannot run nova-bus: "+edge.RedactN(run.Reason, edge.CommandMax), run.Fix+", then "+busRecipe(name))
	case err != nil:
		return bus.Presence{}, false, RefusalOr(err, broken("nova-bus who failed ("+errText(err)+")", busRecipe(name)))
	}
	p, ok := who[name]
	return p, ok, nil
}

// busRecipe is how a name hooks in to the bus.
func busRecipe(name string) string {
	return "hook in to the bus first: NOVA_BUS_REDIS=<host:port> nova-bus hook --as " + name + " under your session's Monitor tool"
}

func notCoordinator(verb, actor string, snap store.Snapshot) *Fail {
	c := coordinatorOf(snap)
	if c == "" {
		return refuse(verb+" is the coordinator's and the sprint has none", "nova-sprint hook --as <coordinator> --role coordinator")
	}
	return refuse(verb+" is the coordinator's, and "+actor+" is a worker", "ask "+c+": nova-bus send --as "+actor+" --to "+c+" \"<subject>\" \"<what you need>\"")
}

// coordinatorOf is the row hooked in as coordinator, or "".
func coordinatorOf(s store.Snapshot) string {
	for _, n := range slices.Sorted(maps.Keys(s.Rows)) {
		if r := s.Rows[n]; r.Session != "" && r.Role == "coordinator" {
			return n
		}
	}
	return ""
}

func roleWord(r role) string {
	if r == roleCoordinator {
		return "coordinator"
	}
	return "worker"
}

// validName is a row's name as the hook takes it.
func validName(s string) bool {
	if len(s) == 0 || len(s) > store.MaxName {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// snapshot is one read of the store, every card.
func (e *Engine) snapshot(ctx context.Context) (store.Snapshot, error) {
	return e.read(ctx, store.Scope{All: true})
}

// read is one read of the store with the cards scope names.
func (e *Engine) read(ctx context.Context, scope store.Scope) (store.Snapshot, error) {
	if e.Store == nil {
		return store.Snapshot{}, broken("the store is not configured", "set NOVA_PG_DSN and NOVA_SPRINT_PG_PASSWORD, then nova-sprint check")
	}
	snap, err := e.Store.Read(ctx, scope)
	if err != nil {
		if f := e.storeRefusal(ctx, err); f != nil {
			return snap, f
		}
		return snap, RefusalOr(err, broken("the store did not answer ("+errText(err)+")", "nova-sprint check"))
	}
	return snap, nil
}

// storeRefusal is what a store error means when the store is not this
// build's to read: no store named (ErrNoStore), not set up (ErrNotSetUp,
// or a store the doctor reads as without the schema), or set up by another
// build (the doctor's refusal, schemaSkew, naming both hashes: a live
// sprint is never "not set up"). nil is a store error as it is.
func (e *Engine) storeRefusal(ctx context.Context, err error) *Fail {
	switch {
	case errors.Is(err, ErrNoStore):
		return NoStore("")
	case errors.Is(err, ErrNotSetUp):
		return notSetUp("")
	case e.Doctor == nil || ctx.Err() != nil:
		return nil
	}
	current, serr := e.Doctor.Schema(ctx)
	if serr == nil && !current {
		return notSetUp("")
	}
	if r := AsRefusal(serr); r != nil {
		if f := classify(r); f.Exit == exitRefused {
			return f
		}
	}
	return nil
}
