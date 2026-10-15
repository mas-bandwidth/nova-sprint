package main

import (
	"context"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// Who may run a verb. Every verb has one class (a class test holds it):
//
//   - coordinator: the sprint's coordinator alone (init --coordinator, set
//     once by the first init); another actor is refused and nothing written.
//   - worker: a fleet member or a reader, named by --as (fleet beat: the
//     member; friend beat: the friend); its actor is that name, whatever
//     --actor says.
//   - report: an outside actor's report (merge, ci), anyone's who names it.
//   - machine: the run loop's (tick, run), recorded as the machine.
//   - read: changes nothing and needs no actor; seat --repair alone writes,
//     the coordinator key from the seat's record, and wants an actor, the
//     record's holder or the owner (sprint.NotSeatRepair).
//   - seat: coordinator <name>, the seat moved: given by its holder or the
//     sprint's owner, or taken by the one taking it with the owner's name
//     (sprint.NotSeat); the verb judges who may, and the step again.
//
// Every class but read, machine and worker wants an actor. A coordinator or
// seat verb without --actor or NOVA_SPRINT_ACTOR may use the coordinator's name
// only while that name's nova-hook record is proven as the coordinator.
const (
	classCoordinator = "coordinator"
	classWorker      = "worker"
	classReport      = "report"
	classMachine     = "machine"
	classRead        = "read"
	classSeat        = "seat"
)

var verbClasses = map[string]string{
	"grant add": classCoordinator, "grant extend": classCoordinator, "grant end": classCoordinator, "grant list": classRead,
	"init": classCoordinator, "add": classCoordinator, "quack": classCoordinator, "release": classCoordinator, "resolve": classCoordinator,
	"start": classCoordinator, "stop": classCoordinator, "ask": classCoordinator, "accept": classCoordinator,
	"rework": classCoordinator, "return": classCoordinator, "redo": classCoordinator, "drop": classCoordinator, "defer": classCoordinator, "unpin": classCoordinator, "priority": classCoordinator, "rank": classCoordinator, "relink": classCoordinator, "recut": classCoordinator, "brief": classCoordinator, "move": classCoordinator,
	"resume": classCoordinator, "land": classCoordinator, "fleet up": classCoordinator, "fleet down": classCoordinator, "hold": classCoordinator, "unhold": classCoordinator, "fleet hold": classCoordinator, "fleet unhold": classCoordinator, "friend hold": classCoordinator, "friend unhold": classCoordinator,
	"fleet level": classCoordinator, "fleet quiet": classCoordinator, "fleet sync": classCoordinator, "friend sync": classCoordinator, "friend down": classCoordinator, "friend up": classCoordinator, "friend take": classCoordinator, "friend give": classCoordinator, "friend level": classCoordinator, "reader add": classCoordinator, "reader set": classCoordinator, "reader away": classCoordinator, "reader up": classCoordinator, "reader remove": classCoordinator, "reader retire": classCoordinator, "stream remove": classCoordinator, "stream set": classCoordinator, "set": classCoordinator, "promoted": classCoordinator, "funded": classCoordinator, "cost reconcile": classCoordinator, "cost reprice": classCoordinator, "wait": classCoordinator,
	"merge-window open": classCoordinator,
	"ack":               classCoordinator, "answer": classCoordinator, "clear": classCoordinator, "teardown": classCoordinator, "repair": classCoordinator,
	"goal set": classCoordinator, "goal drop": classCoordinator, "play": classCoordinator,

	"take": classWorker, "finish": classWorker, "progress": classWorker, "read": classWorker, "queue": classWorker, "stop-return": classWorker, "fleet beat": classWorker, "friend beat": classWorker, "friend cards": classWorker, "friend stage": classWorker, "lane take": classWorker, "lane give": classWorker,
	// remind is any actor's: it sets a timer for itself or for another (--for).
	"remind": classWorker,

	"merge": classReport, "ci": classReport,

	"tick": classMachine, "run": classMachine, "friend clean": classMachine, "seat install": classMachine, "seat uninstall": classMachine, "selftest land": classMachine, "server switch": classMachine,

	"inbox": classRead, "card": classRead, "log": classRead, "check": classRead, "where": classRead, "watch": classRead, "dashboard": classRead, "routes": classRead, "rules": classRead, "stats": classRead, "bases": classRead,
	"goal show": classRead, "handover": classRead, "seat": classRead, "lane list": classRead, "fsck seat": classRead, "doctor": classRead,

	"coordinator": classSeat,
}

// orActor is a worker's actor: the member or reader it names, whatever
// --actor or NOVA_SPRINT_ACTOR say, so the record names the worker the verb
// was run as, as the server records a worker's verb (serve.go).
func (c *common) orActor(name string) {
	c.actor = name
}

// needsActor is why the verb may not run with no actor: "" is may.
func needsActor(c common) string {
	switch verbClasses[c.verb] {
	case "", classRead, classMachine:
		return ""
	}
	if c.actor != "" || c.read {
		return ""
	}
	return "--actor <name> is required (or NOVA_SPRINT_ACTOR): who acts is recorded with every change; nothing was changed"
}

// hookGateError is the hook gate's refusal as an error: the command was refused for want
// of the coordinator's nova-hook record proven before it read or wrote anything. It is a type so a caller that must tell it from a failure finds it with
// errors.As, never by its words (the land loop: a land the gate refused is a wait for the
// seat, not a land fault, and never rolls a switch back; docs/SPEC-SPRINT.md section 14,
// tla/ServerSwitch.tla LandHookRefused).
type hookGateError struct{ why string }

func (e *hookGateError) Error() string { return e.why }

// coordinatorGate is why the actor may not run the verb ("" is may), saying also
// whether the refusal is the hook gate's. The first init names the coordinator
// (--coordinator, else the actor); every later coordinator verb, init included,
// is that actor's alone. A store with no coordinator takes init and teardown
// only. Every command run as the coordinator waits for its nova-hook record
// proven as the coordinator (hookedIn; coordinator_hook.go): the first init, on
// a store with no coordinator, only names the seat.
func (a *app) coordinatorGate(ctx context.Context, st *store.Store, c common) (why string, byHook bool, err error) {
	if verbClasses[c.verb] == classCoordinator {
		why, err := coordinatorsAlone(ctx, st, c)
		if err != nil || why != "" {
			return why, false, err
		}
	}
	why, err = a.hookedIn(ctx, st, c, hookExempt[c.verb])
	return why, err == nil && why != "", err
}

// coordinatorsAlone is why the actor may not do what is the coordinator's
// alone (a coordinator verb, or inbox --read, which moves the coordinator's
// cursor): "" is may.
func coordinatorsAlone(ctx context.Context, st *store.Store, c common) (string, error) {
	coord, err := st.B.Coordinator(ctx)
	if err != nil {
		return "", err
	}
	switch {
	case coord == "" && (c.verb == "init" || c.verb == "teardown"):
		return "", nil
	case coord == "":
		return c.verb + " is the coordinator's, and the sprint has no coordinator; run: nova-sprint init --coordinator <name>", nil
	case c.actor != coord:
		return c.verb + " is the coordinator's alone: " + coord + ", not " + c.actor + "; nothing was changed", nil
	case c.verb == "init" && c.coordinator != "" && c.coordinator != coord:
		return "the sprint's coordinator is " + coord + ", and init does not change the coordinator; nothing was changed", nil
	}
	return "", nil
}
