package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// The coordinator hooks in with nova-hook (docs/SPEC-SPRINT.md, "The hook";
// internal/sprint/coordinator_hook.go; the owner, 2026-10-10: "Every command fails with
// 'You must hook in first'."; 2026-10-14, v1.3.6: the sprint's own seat hook and seat push
// are retired, and the coordinator proves only through nova-hook).
//
// Every command the seat's holder runs, but the exempt, reads the holder's nova-hook record
// (the Redis beside the bus, NOVA_BUS_REDIS) and runs only while it is proven as the
// coordinator: hooked in with `nova-hook hook --as <name> --role coordinator`, the session's
// answer to nova-hook serve's latest challenge in, and the sprint push, which this sprint's
// server holds (coordinator_push.go), live. A record that cannot be read refuses (fail
// closed). The sprint writes nothing of the coordinator's liveness: only nova-hook does.

// The gate's test seam. hookArmedDefault is false only in this package's test binary
// (TestMain): there a test arms the gate for the names it registers in hookTests. The
// binary, and every name in it, is armed: hookArmed is true outside a test binary
// whatever hookArmedDefault says (testing.Testing), so a machine is never unarmed and
// a machine never hooked is refused (the owner, 2026-10-11: "Fail closed").
var (
	hookArmedDefault = true
	hookTests        sync.Map // name -> any
)

// hookArmed says name's verbs want its nova-hook record proven.
func hookArmed(name string) bool {
	if hookArmedDefault || !testing.Testing() {
		return true
	}
	_, ok := hookTests.Load(name)
	return ok
}

// hookExempt are the verbs the gate never refuses, whoever runs them (docs/SPEC-SPRINT.md,
// "The hook"): the server's own life (run, the server, which holds the coordinator's sprint
// push; tick, its loop; server switch, which puts a server in place; seat install, which
// records the server this machine's seat uses; selftest land, the server's self test). A
// verb that starts or replaces the server cannot wait on a push the server holds. help, -h
// and version run before the gate (main.go, run), and a verb's own -h is its help. Every
// other verb, the machine's adopt, seat uninstall and friend clean among them, is gated for
// the holder.
var hookExempt = map[string]bool{"run": true, "tick": true, "server switch": true, "seat install": true, "selftest land": true}

// hookExemptCall says the call of verb with these words is exempt: tick is, but not
// tick --answer-rules, which answers judgments as the coordinator (the word read as the
// command line has it, so no flag parse can lose it).
func hookExemptCall(verb string, args []string) bool {
	if verb == "tick" {
		for _, w := range args {
			name, val, _ := strings.Cut(strings.TrimLeft(w, "-"), "=")
			if strings.HasPrefix(w, "-") && name == "answer-rules" && val != "false" {
				return false
			}
		}
	}
	return hookExempt[verb]
}

// hookReadBound bounds one read or write of the nova-hook record.
const hookReadBound = 5 * time.Second

// hookGateBound bounds the whole gate a command passes before its verb (hookFirst): the
// store's open, the seat's holder and the nova-hook record, each read in it.
const hookGateBound = 30 * time.Second

// hookConn is a process's one connection to the nova-hook record's store, opened at the
// first read and kept (the server's lane apps share their server's: servelanes.go), so a
// gate on the server's line never dials. A read that fails drops it, and the next dials
// again.
type hookConn struct {
	mu    sync.Mutex
	st    hook.Store
	open  bool
	close func()
}

// hookStore is the nova-hook record's store: a test's (hookStoreFn), else this process's
// kept connection to the bus Redis NOVA_BUS_REDIS names, where nova-hook keeps the record.
func (a *app) hookStore(ctx context.Context) (hook.Store, error) {
	if a.hookStoreFn != nil {
		return a.hookStoreFn(ctx)
	}
	a.hk.mu.Lock()
	defer a.hk.mu.Unlock()
	if a.hk.open {
		return a.hk.st, nil
	}
	addr := a.getenv(busRedisEnv)
	if addr == "" {
		return hook.Store{}, errors.New(busRedisEnv + " is not set: the bus Redis, where nova-hook keeps the hook record, is not named")
	}
	b, closeBus, err := a.busOpen(ctx, addr, a.getenv(busUserEnv))
	if err != nil {
		return hook.Store{}, err
	}
	r, ok := b.Store.(bus.Redis)
	if !ok || r.C == nil {
		closeBus()
		return hook.Store{}, errors.New("the bus store is no Redis, so it keeps no nova-hook record (fail closed)")
	}
	a.hk.st, a.hk.open, a.hk.close = hook.Store{C: r.C, Timeout: hookReadBound}, true, closeBus
	return a.hk.st, nil
}

// hookDrop drops the kept connection after a failed read: the next read dials again.
func (a *app) hookDrop() {
	if a.hookStoreFn != nil {
		return
	}
	a.hk.mu.Lock()
	defer a.hk.mu.Unlock()
	if a.hk.open && a.hk.close != nil {
		a.hk.close()
	}
	a.hk.st, a.hk.open, a.hk.close = hook.Store{}, false, nil
}

// hookStatus is name's nova-hook record as nova-hook reads it (hook.Store.Read: the
// record, each push's lease and proof, and the heartbeat's evidence).
func (a *app) hookStatus(ctx context.Context, name string) (hook.Status, error) {
	hs, err := a.hookStore(ctx)
	if err != nil {
		return hook.Status{}, err
	}
	sts, _, err := hs.Read(ctx, name)
	if err != nil {
		a.hookDrop()
		return hook.Status{}, err
	}
	if len(sts) != 1 {
		return hook.Status{}, fmt.Errorf("the hook record read answered %d statuses for one name", len(sts))
	}
	return sts[0], nil
}

// coordinatorHookWhy is why name may not run a coordinator command now, "" when it may
// (sprint.CoordinatorGate over its nova-hook record); a record that cannot be read is a
// refusal naming why (fail closed).
func (a *app) coordinatorHookWhy(ctx context.Context, name string) string {
	st, err := a.hookStatus(ctx, name)
	if err != nil {
		return sprint.HookUnread(name, oneline.Escape(err.Error()))
	}
	return sprint.CoordinatorGate(name, st)
}

// hookedIn is why the call may not run for want of the coordinator's hook, "" when it
// may (docs/SPEC-SPRINT.md, "The hook"): the seat's holder, on every verb but the
// exempt, wants its nova-hook record proven as the coordinator. Nothing on a sprint with
// no holder is judged (the first init names the seat). c.actor is the caller's own name
// (--actor, NOVA_SPRINT_ACTOR), never a worker's --as. exempt is the caller's word on the
// call (hookExemptCall reads its words; the store's open has the verb alone).
func (a *app) hookedIn(ctx context.Context, st *store.Store, c common, exempt bool) (string, error) {
	if c.actor == "" || exempt || !hookArmed(c.actor) || (st != nil && st.ByHand) {
		return "", nil
	}
	holder, err := st.B.Coordinator(ctx)
	if err != nil || holder == "" || c.actor != holder {
		return "", err
	}
	return a.coordinatorHookWhy(ctx, holder), nil
}

// defaultHookActor fills in an omitted actor only when the store's coordinator's
// nova-hook record is proven as the coordinator. The identity comes from the sprint
// store, never from a local seat/configuration file. Explicit --actor and
// NOVA_SPRINT_ACTOR retain their ordinary precedence. When a candidate cannot be proven,
// notice names the withheld default without exposing backend details; resolved says
// whether this call inserted the proven identity. The discovery reads the backend
// directly so it does not tick or otherwise mutate a twin before the command's normal
// store path.
func (a *app) defaultHookActor(args []string, redisOverride string, ignoreProcessActor bool) ([]string, string, bool) {
	v := readVerb(args)
	if v.words == 0 || v.help || v.err != nil || v.given("actor") || hookExemptCall(v.name, args[v.words:]) {
		return args, "", false
	}
	if verbClasses[v.name] != classCoordinator && verbClasses[v.name] != classSeat {
		return args, "", false
	}
	if !ignoreProcessActor && a.getenv("NOVA_SPRINT_ACTOR") != "" {
		return args, "", false
	}
	redis := redisOverride
	if redis == "" {
		redis = firstEnv(a.getenv, "NOVA_SPRINT_REDIS", "NOVA_REDIS_ADDR", seatLoginAddr)
	}
	if f := v.fs.Lookup("redis"); f != nil && v.given("redis") {
		redis = f.Value.String()
	}
	if strings.TrimSpace(redis) == "" {
		return args, "ACTOR default withheld: no sprint store address is configured", false
	}
	ctx := context.Background()
	b, err := a.backend(ctx, redis, sprint.Names{})
	if err != nil {
		return args, "ACTOR default withheld: the sprint backend could not be opened", false
	}
	holder, err := b.Coordinator(ctx)
	if err != nil {
		return args, "ACTOR default withheld: the sprint backend could not read the coordinator", false
	}
	if holder == "" {
		return args, "ACTOR default withheld: no coordinator is recorded in the sprint store", false
	}
	if !hookArmed(holder) {
		return args, "ACTOR default withheld: the coordinator hook gate is not armed", false
	}
	st, err := a.hookStatus(ctx, holder)
	if err != nil {
		return args, "ACTOR default withheld: the coordinator's nova-hook record could not be read", false
	}
	if sprint.CoordinatorGate(holder, st) != "" {
		return args, "ACTOR default withheld: the coordinator's nova-hook record is not proven as the coordinator", false
	}
	out := slices.Clone(args)
	out = slices.Insert(out, v.words, "--actor", holder)
	return out, "ACTOR " + oneline.Field(holder) + " (coordinator hook proven)", true
}

// hookFirst runs the gate before the verb, on the verb's own words, so its refusal is
// the one line and nothing else, exit 1: "You must hook in first: ...", the breadcrumb
// with the nova-hook command that hooks the coordinator in. The caller is the actor
// (--actor, else NOVA_SPRINT_ACTOR), judged against the seat's holder before anything
// else: a worker verb's --as names its worker, and never takes the holder past the gate.
// It fails closed: a caller whose store is not named or does not open is refused with
// why. It lets the verb run only when the gate does, when no one is named to judge (no
// actor: a read that changes nothing, or a verb that refuses for want of an actor), and
// for a verb's help or flags it refuses (the verb refuses them itself, exit 2, writing
// nothing).
func (a *app) hookFirst(args []string, stderr io.Writer) (int, bool) {
	v := readVerb(args)
	if v.words == 0 || v.help || v.err != nil || hookExemptCall(v.name, args[v.words:]) {
		return 0, false
	}
	c := common{verb: v.name, redis: firstEnv(a.getenv, "NOVA_SPRINT_REDIS", "NOVA_REDIS_ADDR", seatLoginAddr), actor: a.getenv("NOVA_SPRINT_ACTOR")}
	if f := v.fs.Lookup("redis"); f != nil && v.given("redis") {
		c.redis = f.Value.String()
	}
	if f := v.fs.Lookup("actor"); f != nil && v.given("actor") {
		c.actor = f.Value.String()
	}
	if c.actor == "" || !hookArmed(c.actor) {
		return 0, false
	}
	refuse := func(why string) (int, bool) {
		fmt.Fprintln(stderr, why)
		return 1, true
	}
	if strings.TrimSpace(c.redis) == "" {
		return refuse(sprint.HookUnread(c.actor, "no sprint store is named (NOVA_SPRINT_REDIS or --redis), so the seat's holder cannot be read"))
	}
	// the gate never waits forever: its store open and its reads end by hookGateBound,
	// and a gate that has not answered by then refuses, failing closed
	ctx, cancel := context.WithTimeout(context.Background(), hookGateBound)
	defer cancel()
	st, err := a.storeCtx(ctx, common{redis: c.redis})
	if err != nil {
		return refuse(sprint.HookUnread(c.actor, "the sprint store "+oneline.Field(c.redis)+" did not open: "+oneline.Escape(err.Error())))
	}
	why, err := a.hookedIn(ctx, st, c, false) // the exempt call left above
	if err != nil {
		return refuse(sprint.HookUnread(c.actor, "the seat's holder could not be read: "+oneline.Escape(err.Error())))
	}
	if why == "" {
		return 0, false
	}
	fmt.Fprintln(stderr, why)
	// the sprint push is the server's: a refusal names where the server is set when
	// neither source names one (namedServer); a process that is the server, or a verb with
	// its own store, names one itself. (docs/SPEC-SPRINT.md, "The server".)
	if a.serveAddr == "" {
		if addr, _ := a.namedServer(); addr == "" {
			fmt.Fprintln(stderr, a.noServerHint())
		}
	}
	return 1, true
}

// onLine runs f holding the server's line of control (a.serial), where every verb the
// server runs and the tick run: a read here is never of a verb half-way.
func (a *app) onLine(ctx context.Context, f func(st *store.Store) error) error {
	if err := a.serial.LockCtx(ctx); err != nil {
		return err
	}
	defer a.serial.Unlock()
	st, err := a.store(common{redis: a.serveAddr})
	if err != nil {
		return err
	}
	return f(st)
}

// serveSay says a line on the server's log (run's stdout once it listens); nothing in a
// process that is not the server.
func (a *app) serveSay(format string, args ...any) {
	if a.serveLog == nil {
		return
	}
	a.lanesMu.Lock()
	defer a.lanesMu.Unlock()
	fmt.Fprintf(a.serveLog, format+"\n", args...)
}
