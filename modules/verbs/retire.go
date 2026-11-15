package verbs

import (
	"context"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// runRetire is `retire`, the coordinator's worst case (Glenn, 2026-11-15:
// "you can have this for worst case"): the run retires a row nova-config no
// longer names on its next tick by itself; this does the same by hand, one
// op, its live hand returned to the queue (branches and heads kept) and the
// row retired (store.RetireRow), so it shows nowhere until it hooks in
// again. It reads nova-config first and judges the row as the tick does
// (steps.Retirable): it refuses a row nova-config still names, a hooked
// coordinator, and anything at all when nova-config cannot be read or names
// nothing (a fault must never retire the fleet).
func runRetire(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	name, reason := in.arg(0), in.str("reason")
	if reason == "" {
		return "", refuse("a retire needs its reason", "add --reason \"<why>\"")
	}
	row, ok := snap.Rows[name]
	if !ok {
		return "", refuse("no row "+name+" in the store (a retired row is in no read)", "nova-sprint show")
	}
	if e.Roster == nil {
		return "", broken("nova-config is not configured, so retire cannot tell whether it still names "+name, "nova-sprint check")
	}
	ro, err := e.Roster.ReadRosterAt(ctx, 1) // the width cap moves no name in or out of the roster
	switch {
	case err != nil:
		return "", broken("nova-config's roster cannot be read ("+errText(err)+"); nothing is retired on a failed read", "nova-sprint check, then retire again")
	case ro.Empty():
		return "", refuse("nova-config names no machine and no friend; nothing is retired on an empty roster", "nova-config machine list; nova-config friend list")
	case ro.Names(name):
		return "", refuse("row "+name+" is still in nova-config; a row it names is never retired", "nova-config machine remove "+name+" (a machine) or nova-config friend remove "+name+" (a friend); the run then retires the row on its next tick")
	case !steps.Retirable(ro, row):
		return "", refuse("row "+name+" is hooked as the coordinator; a coordinator is never retired", "nova-sprint unhook "+name+" --as "+in.actor+", then retire again")
	}
	line, err := e.move(ctx, in, snap, fixed(steps.Request{Row: name, Event: card.Retire{Reason: steps.RetiredReason + ": " + reason}}), name)
	if err != nil {
		return "", err
	}
	return done(nextAfter(in), line), nil
}
