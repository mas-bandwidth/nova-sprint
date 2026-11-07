package verbs

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// hooked is the one hooked check (CORRECT-USE, "Hooked before anything that
// acts as you"; DO NOT REMOVE without Glenn, LESSONS W.17): a name, on the
// bus, hooked in to the sprint, in the role the verb needs. It returns the
// snapshot it read, which the verb acts on.
func (e *Engine) hooked(ctx context.Context, in *input, need role) (store.Snapshot, error) {
	if err := e.onBus(ctx, in.actor); err != nil {
		return store.Snapshot{}, err
	}
	snap, err := e.snapshot(ctx)
	if err != nil {
		return snap, err
	}
	row := snap.Rows[in.actor]
	if row.Session == "" {
		return snap, refuse("hook in first", "nova-sprint hook --as "+in.actor+" --role "+roleWord(need))
	}
	if need == roleCoordinator && row.Role != "coordinator" {
		return snap, notCoordinator(in.verb, in.actor, snap)
	}
	return snap, nil
}

// onBus is a name given and present in nova-bus who: the bus first, then the
// sprint (Glenn, 2026-11-06).
func (e *Engine) onBus(ctx context.Context, name string) error {
	if name == "" {
		return refuse("no name given", "pass --as <you>, or set NOVA_SPRINT_AS=<you>")
	}
	if !validName(name) {
		return refuse(clip(name)+" is not a name the sprint takes", "use 1 to 128 of a-z A-Z 0-9 . _ -")
	}
	if e.Bus == nil {
		return broken("nova-bus is not configured", "set NOVA_BUS_REDIS, then nova-sprint check")
	}
	who, err := e.Bus.Who(ctx)
	if err != nil {
		return RefusalOr(err, broken("nova-bus who failed ("+errText(err)+")", "nova-sprint check"))
	}
	if _, ok := who[name]; !ok {
		return refuse(name+" is not on the bus", busRecipe(name))
	}
	return nil
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

// snapshot is one read of the store.
func (e *Engine) snapshot(ctx context.Context) (store.Snapshot, error) {
	if e.Store == nil {
		return store.Snapshot{}, broken("the store is not configured", "set NOVA_PG_DSN and NOVA_SPRINT_PG_PASSWORD, then nova-sprint check")
	}
	snap, err := e.Store.Snapshot(ctx)
	if err != nil {
		if e.notSetUp(ctx, err) {
			return snap, notSetUp("")
		}
		return snap, RefusalOr(err, broken("the store did not answer ("+errText(err)+")", "nova-sprint check"))
	}
	return snap, nil
}

// notSetUp is a store error that means the sprint is not set up: no sprint
// credential (ErrNotSetUp), or a store the doctor reads as without this
// build's schema.
func (e *Engine) notSetUp(ctx context.Context, err error) bool {
	if errors.Is(err, ErrNotSetUp) {
		return true
	}
	if e.Doctor == nil || ctx.Err() != nil {
		return false
	}
	current, serr := e.Doctor.Schema(ctx)
	return serr == nil && !current
}
