package main

import (
	"bufio"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// The coordinator hooks in (docs/SPEC-SPRINT.md, "The hook"; internal/sprint/hook.go;
// tla/SeatHook.tla; the owner, 2026-10-10: "You MUST be required to 'hook' in as
// coordinator." "This is not optional. Every command fails with 'You must hook in
// first'." "Until you call 'nova-sprint hook'." "and then when you stop, you go
// 'nova-sprint unhook'." "And you must PROVE YOU HAVE HOOKED IN before it will allow
// you to do anything.").
//
// The server owns the subscription (client/server: its record in the store is the one
// truth, written by the server alone). The seat's session runs `nova-sprint hook` under
// a Monitor: one connection to the server's loopback listener, upgraded from HTTP to a
// stream of lines, over which the server sends a challenge and every judgment and push,
// each with an id. The session answers through the same connection: `hook --prove
// <challenge>` and `hook --ack <id> --session <token>` hand their word to the hook process over a socket
// of this machine, and the hook process writes it on the connection; `unhook` ends it
// the same way. The hook process holds nothing the server decides on: it carries lines.

// hookPath is the hook's endpoint on the server's loopback listener (the coordinator's);
// hookUpgrade is the protocol the connection is upgraded to.
const (
	hookPath    = "/api/hook"
	hookUpgrade = "nova-sprint-hook"
)

// hookLook is how often the server looks for what is new for the hooked seat and at the
// challenge's clock; hookWriteBound bounds each line written to the session, so a hook
// that does not read never holds the server.
const (
	hookLook       = time.Second // the waiting-judgment warning is judged every look, pushed at most once a sprint.JudgmentWarnEvery for one set (sprint.JudgmentWarnDue)
	hookWriteBound = 10 * time.Second
	hookAskBound   = 30 * time.Second
)

// hookWarnWhat is what a waiting-judgment warning push is (sprint.JudgmentWarnings).
const hookWarnWhat = "WARNING-judgments-waiting"

// hookMsg is one line on the hook's connection, either way. The server sends hello,
// challenge, push, ok, refused and bye; the session sends prove, ack, ack-all and
// unhook, each with its req, which the server's answer carries back. Acked is the
// ack-all answer's count (how many open pushes it acknowledged).
type hookMsg struct {
	T         string    `json:"t"`
	Req       string    `json:"req,omitempty"`
	Of        string    `json:"of,omitempty"`
	Name      string    `json:"name,omitempty"`
	Role      string    `json:"role,omitempty"`
	Session   string    `json:"session,omitempty"`
	Challenge string    `json:"challenge,omitempty"`
	ID        uint64    `json:"id,omitempty"`
	What      string    `json:"what,omitempty"`
	Text      string    `json:"text,omitempty"`
	Why       string    `json:"why,omitempty"`
	Live      time.Time `json:"live,omitzero"`
	Unacked   int       `json:"unacked,omitempty"`
	Acked     int       `json:"acked,omitempty"`
}

// The hook's test seam. hookArmedDefault is false only in this package's test binary
// (TestMain): there a test arms the gate for the names it registers in hookTests. The
// binary, and every name in it, is armed: hookArmed is true outside a test binary
// whatever hookArmedDefault says (testing.Testing), so a machine is never unarmed and
// a machine never armed is not hooked (the owner, 2026-10-11: "Fail closed").
var (
	hookArmedDefault = true
	hookTests        sync.Map // name -> any
)

// hookArmed says name's verbs want its live, proven hook.
func hookArmed(name string) bool {
	if hookArmedDefault || !testing.Testing() {
		return true
	}
	_, ok := hookTests.Load(name)
	return ok
}

// hookExempt are the verbs the gate never refuses, whoever runs them (docs/SPEC-SPRINT.md,
// "The two pushes"): the hook itself and its end, and the server's own life (run, the
// server; tick, its loop; server switch and seat install, which put a server in place;
// selftest land, the server's self test). The hook is a stream from the server, so a
// verb that starts or replaces the server cannot wait on it. help, -h and version run
// before the gate (main.go, run), and a verb's own -h is its help. Every other verb, the
// machine's adopt, seat uninstall and friend clean among them, is gated for the holder.
var hookExempt = map[string]bool{"hook": true, "unhook": true, "run": true, "tick": true, "server switch": true, "seat install": true, "selftest land": true}

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

// readHook is the seat's hook record; ok false when there is none.
func readHook(ctx context.Context, st *store.Store) (sprint.HookRecord, bool, error) {
	var rec sprint.HookRecord
	kv, ok := st.B.(store.KV)
	if !ok {
		return rec, false, nil
	}
	raw, ok, err := kv.GetKey(ctx, store.KeySeatHook)
	if err != nil || !ok {
		return rec, false, err
	}
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return rec, false, fmt.Errorf("the seat's hook record is not JSON: %w", err)
	}
	return rec, true, nil
}

// writeHook writes the seat's hook record: the server's alone (hookSession).
func writeHook(ctx context.Context, st *store.Store, rec sprint.HookRecord) error {
	kv, ok := st.B.(store.KV)
	if !ok {
		return errors.New("this store keeps no keys, and the hook record is one")
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return kv.SetKey(ctx, store.KeySeatHook, string(b))
}

func keySet(keys []string) map[string]bool {
	out := make(map[string]bool, len(keys))
	for _, key := range keys {
		out[key] = true
	}
	return out
}

func keyList(keys map[string]bool) []string {
	active := make(map[string]struct{}, len(keys))
	for key, yes := range keys {
		if yes {
			active[key] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(active))
}

func cloneKeys(keys map[string]bool) map[string]bool { return maps.Clone(keys) }

// hookedIn is why the call may not run for want of the seat's hook, "" when it may
// (docs/SPEC-SPRINT.md, "The two pushes"; sprint.HookGateCall; SeatHook.tla, Gate): the
// seat's holder, on every verb but the exempt, wants both its pushes live, the sprint
// push as the server recorded it and the bus push from its record in the bus store (read
// from a copy at most a second old: hookerShared). A record that cannot be read refuses,
// naming why (fail closed). Nothing on a sprint with no holder is judged (the first init
// names the seat). c.actor is the caller's own name (--actor, NOVA_SPRINT_ACTOR), never a
// worker's --as. exempt is the caller's word on the call (hookExemptCall reads its
// words; the store's open has the verb alone).
func (a *app) hookedIn(ctx context.Context, st *store.Store, c common, exempt bool) (string, error) {
	if c.actor == "" || exempt || !hookArmed(c.actor) || (st != nil && st.ByHand) {
		return "", nil
	}
	holder, err := st.B.Coordinator(ctx)
	if err != nil || holder == "" || c.actor != holder {
		return "", err
	}
	call := sprint.HookCall{Identity: c.actor}
	rec, ok, err := readHook(ctx, st)
	if err != nil {
		call.Unread = "the sprint push's record could not be read: " + oneline.Escape(err.Error())
	}
	call.Seat, call.SeatOK = rec, ok
	if call.Unread == "" {
		if call.Shared, err = a.hookerShared(ctx, c.actor); err != nil {
			call.Unread = "the bus push's record (" + sprint.HookSharedKey(c.actor) + " in the bus store) could not be read: " + oneline.Escape(err.Error())
		}
	}
	return sprint.HookGateCall(call, st.Now()), nil
}

// defaultHookActor fills in an omitted actor only when the store's coordinator has
// both live, proven pushes. The identity comes from the sprint store, never from a
// local seat/configuration file. Explicit --actor and NOVA_SPRINT_ACTOR retain their
// ordinary precedence. When a candidate cannot be proven, notice names the withheld
// default without exposing backend details; resolved says whether this call inserted
// the proven identity. The discovery reads the backend directly so it does not tick
// or otherwise mutate a twin before the command's normal store path.
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
	st := &store.Store{B: b, Now: a.now}
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
	seat, seatOK, err := readHook(ctx, st)
	if err != nil {
		return args, "ACTOR default withheld: the sprint backend could not read the coordinator's hook record", false
	}
	if !sprint.HookLive(seat, seatOK, holder, st.Now()) {
		return args, "ACTOR default withheld: the coordinator's sprint hook is missing, stale, or unproven", false
	}
	shared, err := a.hookerShared(ctx, holder)
	if err != nil {
		return args, "ACTOR default withheld: the bus backend could not read the coordinator's push proof", false
	}
	if !shared.PushLive(sprint.HookPushBus, st.Now()) {
		return args, "ACTOR default withheld: the coordinator's bus push proof is missing, stale, or invalid", false
	}
	out := slices.Clone(args)
	out = slices.Insert(out, v.words, "--actor", holder)
	return out, "ACTOR " + oneline.Field(holder) + " (coordinator hook proven)", true
}

// hookFirst runs the gate before the verb, on the verb's own words, so its refusal is
// the one line and nothing else, exit 1: "You must hook in first: run nova-sprint hook
// --actor <name> (...)", the breadcrumb, or the push to acknowledge. The caller is the
// actor (--actor, else NOVA_SPRINT_ACTOR), judged against the seat's holder before
// anything else: a worker verb's --as names its worker, and never takes the holder past
// the gate. It fails closed: a caller whose store is not named or does not open is
// refused with why. It lets the verb run only when the gate does, when no one is named to
// judge (no actor: a read that changes nothing, or a verb that refuses for want of an
// actor), and for a verb's help or flags it refuses (the verb refuses them itself, exit
// 2, writing nothing).
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
		return refuse(sprint.HookBreadcrumb(c.actor, "", "no sprint store is named (NOVA_SPRINT_REDIS or --redis), so the hook cannot be checked"))
	}
	st, err := a.storeCtx(context.Background(), common{redis: c.redis})
	if err != nil {
		return refuse(sprint.HookBreadcrumb(c.actor, "", "the sprint store "+oneline.Field(c.redis)+" did not open, so the hook cannot be checked: "+oneline.Escape(err.Error())))
	}
	why, err := a.hookedIn(context.Background(), st, c, false) // the exempt call left above
	if err != nil {
		return refuse(sprint.HookBreadcrumb(c.actor, "", "the hook could not be checked: "+oneline.Escape(err.Error())))
	}
	if why == "" {
		return 0, false
	}
	fmt.Fprintln(stderr, why)
	// the gate's refusal names the server to hook into when neither source names one:
	// the environment first, else the seat's record (namedServer); a process that is
	// the server, or a verb with its own store, names one itself. (docs/SPEC-SPRINT.md,
	// "The server".)
	if strings.HasPrefix(why, sprint.HookFirst) && a.serveAddr == "" {
		if addr, _ := a.namedServer(); addr == "" {
			fmt.Fprintln(stderr, a.noServerHint())
		}
	}
	return 1, true
}

// --- the bus push's record (sprint.Hooker) over the bus store ---

// busHooker is sprint.Hooker over the bus store: the hash sprint-hook:<identity>
// (HookShared's fields), written with the bus store's marks (HSET, HDEL) and read with
// Marks (HGETALL). It is interim: the hook module's record replaces it.
type busHooker struct{ s bus.Store }

func (h busHooker) Shared(ctx context.Context, identity string) (sprint.HookShared, bool, error) {
	ms, err := h.s.Marks(ctx, sprint.HookSharedKey(identity))
	if err != nil || len(ms) == 0 {
		return sprint.HookShared{}, false, err
	}
	rec, ok := sprint.HookSharedOf(identity, ms[0])
	return rec, ok, nil
}

func (h busHooker) set(ctx context.Context, identity string, fields map[string]string) error {
	key := sprint.HookSharedKey(identity)
	var marks []bus.Mark
	for _, k := range slices.Sorted(maps.Keys(fields)) {
		if fields[k] == "" {
			marks = append(marks, bus.Mark{Key: key, Field: k, Clear: true})
			continue
		}
		marks = append(marks, bus.Mark{Key: key, Field: k, Value: fields[k]})
	}
	return h.s.AddAll(ctx, nil, nil, marks...)
}

func hookStamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Arm records identity armed as the coordinator at at, both pushes armed and neither
// proven nor unhooked since.
func (h busHooker) Arm(ctx context.Context, identity string, at time.Time) error {
	return h.set(ctx, identity, map[string]string{"role": sprint.HookRoleCoordinator, "pushes": strings.Join(sprint.HookCoordinatorPushes, ","), "armed_at": hookStamp(at), "proved_at": "", "unhooked_at": ""})
}

// Prove records both pushes proven at at.
func (h busHooker) Prove(ctx context.Context, identity string, at time.Time) error {
	return h.set(ctx, identity, map[string]string{"proved_at": hookStamp(at)})
}

// Unhook records the unhook at at: no push is live after it.
func (h busHooker) Unhook(ctx context.Context, identity string, at time.Time) error {
	return h.set(ctx, identity, map[string]string{"unhooked_at": hookStamp(at), "proved_at": ""})
}

// hooker is this process's Hooker: the bus store NOVA_BUS_REDIS names, opened once and
// kept for the process. With no bus store named it is the refusal that names the
// variable: a hook cannot be armed or checked without its record (fail closed).
func (a *app) hooker(ctx context.Context) (sprint.Hooker, error) {
	a.hk.mu.Lock()
	defer a.hk.mu.Unlock()
	return a.hookerLocked(ctx)
}

func (a *app) hookerLocked(ctx context.Context) (sprint.Hooker, error) {
	if a.hk.cur != nil {
		return a.hk.cur, nil
	}
	addr := a.getenv(busRedisEnv)
	if addr == "" {
		return nil, errors.New(busRedisEnv + " is not set: the bus store, where the bus push is recorded (" + sprint.HookSharedPrefix + "<name>), is not named")
	}
	b, _, err := a.busOpen(ctx, addr, a.getenv(busUserEnv))
	if err != nil {
		return nil, err
	}
	a.hk.cur, a.hk.bus = busHooker{s: b.Store}, b
	return a.hk.cur, nil
}

// hookCacheFresh is how old a copy of a bus push record the gate takes: a second. A
// server keeps every copy fresh off its line of control (hookerRefresh), so a gate
// taken on the line reads memory and never waits on the bus store.
const (
	hookCacheFresh  = time.Second
	hookCacheRefill = 500 * time.Millisecond
)

// hookerBox holds a process's Hooker and the bus store under it, once opened, and the
// copies of the records the gate has read (by name, with when each was read, on this
// process's own clock): one per process, shared by the server's lane apps.
type hookerBox struct {
	mu         sync.Mutex
	cur        sprint.Hooker
	bus        *bus.Bus
	cache      map[string]hookCopy
	generation map[string]uint64 // incremented by hookerForget before an off-lock read may commit; see tla/HookCache.tla
	refilling  bool
}

type hookCopy struct {
	rec sprint.HookShared
	at  time.Time
}

// hookerShared is identity's record for the gate: the copy when it is at most
// hookCacheFresh old, else read now (and kept). In a server the copies are refilled every
// hookCacheRefill off the line (hookerRefresh), so the read here is the first one only.
func (a *app) hookerShared(ctx context.Context, identity string) (sprint.HookShared, error) {
	for {
		a.hk.mu.Lock()
		if c, ok := a.hk.cache[identity]; ok && time.Since(c.at) <= hookCacheFresh {
			a.hk.mu.Unlock()
			return c.rec, nil
		}
		generation := a.hk.generation[identity]
		h, err := a.hookerLocked(ctx)
		a.hk.mu.Unlock()
		if err != nil {
			return sprint.HookShared{}, err
		}
		rec, _, err := h.Shared(ctx, identity)
		a.hk.mu.Lock()
		if a.hk.generation[identity] != generation {
			a.hk.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return sprint.HookShared{}, err
			}
			continue // the proof changed during the read; use its new copy or read again
		}
		if err != nil {
			a.hk.mu.Unlock()
			return sprint.HookShared{}, err
		}
		if a.hk.cache == nil {
			a.hk.cache = map[string]hookCopy{}
		}
		a.hk.cache[identity] = hookCopy{rec: rec, at: time.Now()}
		start := a.serveAddr != "" && !a.hk.refilling
		if start {
			a.hk.refilling = true
		}
		a.hk.mu.Unlock()
		if start {
			go a.hookerRefresh(context.Background())
		}
		return rec, nil
	}
}

// hookerRefresh keeps a server's copies fresh: every hookCacheRefill it reads each record
// the gate has asked for, off the line, and replaces the copy; a read that fails leaves
// the copy to age past hookCacheFresh, so the gate then reads for itself and refuses on
// the error (fail closed). It runs for the life of the server.
func (a *app) hookerRefresh(ctx context.Context) {
	t := time.NewTicker(hookCacheRefill)
	defer t.Stop()
	for range t.C {
		a.hookerRefill(ctx)
	}
}

// hookerRefill takes each identity's generation with its cache entry. A proof
// written while the bus read is in flight invalidates that reply (SPEC-SPRINT,
// "The two pushes").
func (a *app) hookerRefill(ctx context.Context) {
	type entry struct {
		name       string
		generation uint64
	}
	a.hk.mu.Lock()
	h := a.hk.cur
	names := make([]entry, 0, len(a.hk.cache))
	for n := range a.hk.cache {
		names = append(names, entry{n, a.hk.generation[n]})
	}
	a.hk.mu.Unlock()
	if h == nil {
		return
	}
	for _, n := range names {
		rec, _, err := h.Shared(ctx, n.name)
		if err != nil {
			continue
		}
		a.hk.mu.Lock()
		if _, ok := a.hk.cache[n.name]; ok && a.hk.generation[n.name] == n.generation {
			a.hk.cache[n.name] = hookCopy{rec: rec, at: time.Now()}
		}
		a.hk.mu.Unlock()
	}
}

// hookerForget drops identity's copy: a write by this process (arm, prove, unhook) is
// read again at the next gate.
func (a *app) hookerForget(identity string) {
	a.hk.mu.Lock()
	if a.hk.generation == nil {
		a.hk.generation = map[string]uint64{}
	}
	a.hk.generation[identity]++
	delete(a.hk.cache, identity)
	a.hk.mu.Unlock()
}

// hookHub is the server's hold of the hooks: the one subscription open for each name
// (the seat's holder's, and each worker friend's). A new hook of a name replaces the
// one open for it.
//
// Lock order (a-card-read-never-runs-the-deal, docs/SPEC-SPRINT.md section 14, The
// server, "The tick's turn"): startMu -> the line of control (a.serial) -> mu. mu is a
// leaf: it is held across no call that can block, never across the line, because a seat
// verb on the line takes it (seatActs); v1.2.14..v1.2.24 held it across the line in
// start and the server deadlocked.
type hookHub struct {
	// startMu is start's order among the starts of this hub: the only lock held while
	// the line is taken. seatActs and current never take it.
	startMu sync.Mutex
	mu      sync.Mutex
	cur     map[string]*hookSession
}

// hookSession is one subscription on the server: its id, the seat it is for, its
// connection, the challenge outstanding and when it was sent, when the last was
// answered, and the pushes delivered over it: the next id, the ones not acknowledged,
// and the note keys sent.
type hookSession struct {
	a    *app
	id   string
	name string
	conn io.ReadWriteCloser

	wmu sync.Mutex // one line at a time on the connection

	mu     sync.Mutex
	chal   string
	chalAt time.Time
	proven time.Time
	seq    uint64
	open   []sprint.HookItem // delivered, not acknowledged, oldest first
	sent   map[string]bool
	// firstSeen is when this hook first saw each open judgment group (by id), and warnSet
	// the last waiting-judgment warning's set: its full text goes out when the set changes
	// (sprint.JudgmentWarnings). warned is the set the last look saw (by judgment id), and
	// warnedAt when the last warning was pushed: the next is pushed at once for a judgment
	// joining the set, else a sprint.JudgmentWarnEvery after it (sprint.JudgmentWarnDue).
	firstSeen map[string]time.Time
	warnSet   string
	warned    map[string]bool
	warnedAt  time.Time
	acked     map[string]bool // durably acknowledged note keys in the current sprint epoch
	epoch     uint64
	// acted is the last act seen over this subscription or from its seat: the proof, an
	// acknowledgement, a coordinator verb, or the hook client's keepalive. A keepalive
	// proves only that the transport is alive (the client's process holds the stream),
	// never that the seat, or a person, is attending: the pushes and the lapse judgment
	// are how a seat that is not attending is told (docs/SPEC-SPRINT.md, "The hook").
	acted   time.Time
	liveAt  time.Time // the Live the record was last written with
	ended   bool
	done    chan struct{}
	endOnce sync.Once
	// quietHappened says the seat wants judgments and challenges only: the
	// informational happened pushes are not delivered (hook --quiet-happened).
	quietHappened bool
}

func (a *app) hookHub() *hookHub {
	a.lanesMu.Lock()
	defer a.lanesMu.Unlock()
	if a.hooks == nil {
		a.hooks = &hookHub{cur: map[string]*hookSession{}}
	}
	return a.hooks
}

// hookNonce is a fresh id or challenge: 16 random bytes in hex, never guessed and never
// written anywhere but the stream.
func hookNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // ignored: crypto/rand.Read never fails (Go 1.24+)
	return hex.EncodeToString(b)
}

// hookRead runs f on a store for reading, off the server's line of control: the read
// lane's (servelanes.go), under the lane's own line, when the server has lanes; else (a
// twin file, or no server: a test's) on the line.
func (a *app) hookRead(ctx context.Context, f func(st *store.Store) error) error {
	l := a.lanesFor(ctx)
	if l == nil {
		return a.onLine(ctx, f)
	}
	if err := l.line.LockCtx(ctx); err != nil {
		return err
	}
	defer l.line.Unlock()
	st, err := l.read.store(common{redis: l.read.serveAddr})
	if err != nil {
		return err
	}
	return f(st)
}

// onLine runs f holding the server's line of control (a.serial), where every verb the
// server runs and the tick run: a record written here is never seen half-way by a
// forwarded verb.
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

// serveHook is the hook's endpoint: GET /api/hook?as=<name> with Upgrade: nova-sprint-hook,
// on the loopback listener only. The seat's holder alone hooks in; the connection is
// taken over and runs the subscription until it ends.
func (a *app) serveHook(w http.ResponseWriter, r *http.Request, local bool) {
	if !local {
		http.Error(w, "the hook is the coordinator's, on the server's loopback listener", http.StatusNotFound)
		return
	}
	name := r.URL.Query().Get("as")
	if !sprint.ValidID(name) {
		http.Error(w, "as=<name> names the seat's holder (letters, digits, _ and -)", http.StatusBadRequest)
		return
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), hookUpgrade) {
		http.Error(w, "the hook is a stream: Upgrade: "+hookUpgrade+"; run: nova-sprint hook", http.StatusUpgradeRequired)
		return
	}
	why, err := a.hookHolderWhy(r.Context(), name)
	if err != nil {
		http.Error(w, "the store did not answer: "+oneline.Err(err), http.StatusServiceUnavailable)
		return
	}
	if why != "" {
		http.Error(w, why, http.StatusForbidden)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "this listener cannot hold a stream", http.StatusInternalServerError)
		return
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{}) // ignored: the server's request deadlines do not bound a stream
	if _, err := brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: " + hookUpgrade + "\r\nConnection: Upgrade\r\n\r\n"); err == nil {
		err = brw.Flush()
	}
	if err != nil {
		_ = conn.Close() // ignored: the hook never began
		return
	}
	a.hookRun(context.Background(), name, &deadlineConn{Conn: conn, r: brw.Reader}, hookLook, r.URL.Query().Get("quiet_happened") == "1")
}

// hookHolderWhy is why name may not hook in here, "" when it may: the hook is the seat's
// holder's alone. (A worker friend's hook is the shared nova-tools hook module's, and
// lands with its client: docs/SPEC-SPRINT.md, "The two pushes".)
func (a *app) hookHolderWhy(ctx context.Context, name string) (why string, err error) {
	err = a.onLine(ctx, func(st *store.Store) error {
		holder, err := st.B.Coordinator(ctx)
		if err != nil {
			return err
		}
		if holder != name {
			why = fmt.Sprintf("the hook is the seat's holder's: %s, not %s; nothing was changed", orDashStr(holder, "nobody"), name)
		}
		return nil
	})
	return why, err
}

// deadlineConn is the hook's connection: reads through the buffer the upgrade read, and
// each write bounded by hookWriteBound.
type deadlineConn struct {
	net.Conn
	r *bufio.Reader
}

func (d *deadlineConn) Read(p []byte) (int, error) { return d.r.Read(p) }

func (d *deadlineConn) Write(p []byte) (int, error) {
	_ = d.SetWriteDeadline(time.Now().Add(hookWriteBound)) // ignored: a conn without deadlines writes unbounded
	return d.Conn.Write(p)
}

// hookRun is one subscription, from its start to its end: recorded hooked and unproven
// at once (SeatHook.tla, Hook), its hello and first challenge sent, the looks every
// look, and the session's lines read until the connection ends. An end that is not
// unhook is a drop, recorded gone and told (Drop).
func (a *app) hookRun(ctx context.Context, name string, conn io.ReadWriteCloser, look time.Duration, quiet ...bool) {
	s := &hookSession{a: a, id: hookNonce(), name: name, conn: conn, sent: map[string]bool{}, firstSeen: map[string]time.Time{}, acked: map[string]bool{}, done: make(chan struct{})}
	if len(quiet) > 0 {
		s.quietHappened = quiet[0]
	}
	if err := s.start(ctx); err != nil {
		// start may fail before publication, or after the new session and its
		// HookHooked record have been published (for example, if hello cannot
		// be written). end is conditional on owning the hub, so it cleans up
		// the latter without disturbing an older session in the former case.
		s.end(ctx, sprint.HookGone, err.Error())
		// end returns without closing when this session was never published.
		_ = s.conn.Close()
		s.a.serveSay("SEAT HOOK name=%s: the hook could not begin: %s", s.name, oneline.Escape(err.Error()))
		return
	}
	if look > 0 {
		go func() {
			t := time.NewTicker(look)
			defer t.Stop()
			for {
				select {
				case <-s.done:
					return
				case <-t.C:
					s.look(ctx)
				}
			}
		}()
	}
	why := s.read(ctx)
	s.end(ctx, sprint.HookGone, why)
}

// start replaces any subscription open (the old connection is closed, and the record is
// this one's), records this one hooked and unproven, and sends the hello and the first
// challenge.
func (s *hookSession) start(ctx context.Context) error {
	why, err := s.a.hookHolderWhy(ctx, s.name)
	if err != nil {
		return err
	}
	if why != "" {
		_ = s.send(hookMsg{T: "bye", Why: why}) // ignored: it ends either way
		return errors.New(why)
	}
	h := s.a.hookHub()
	// startMu, not mu, across the line: mu is a leaf (hookHub's lock order).
	h.startMu.Lock()
	now := s.a.now()
	if err := s.a.onLine(ctx, func(st *store.Store) error {
		old, ok, err := readHook(ctx, st)
		if err != nil {
			return err
		}
		epoch, err := st.EpochNow(ctx)
		if err != nil {
			return err
		}
		if !ok || old.Name != s.name || old.Epoch != epoch.N {
			old.Acked = nil
		}
		s.epoch = epoch.N
		// Push IDs are wire-level cumulative ACKs. They must never be reused across
		// sessions or epochs: a delayed ACK from an old stream could otherwise clear
		// a newly replayed push with the same number.
		s.seq = old.PushSeq
		s.acked = keySet(old.Acked)
		old.Name, old.Epoch, old.Session, old.State, old.Since = s.name, epoch.N, s.id, sprint.HookHooked, now
		old.Proven, old.Live, old.Why, old.Unacked, old.Oldest = time.Time{}, time.Time{}, "", 0, nil
		return writeHook(ctx, st, old)
	}); err != nil {
		h.startMu.Unlock()
		return err
	}
	h.mu.Lock()
	old := h.cur[s.name]
	h.cur[s.name] = s
	h.mu.Unlock()
	h.startMu.Unlock()
	if old != nil {
		old.replaced()
	}
	s.a.serveSay("SEAT HOOK name=%s session=%s: hooked, unproven until the challenge is answered", s.name, s.id[:8])
	if err := s.send(hookMsg{T: "hello", Name: s.name, Role: sprint.HookRoleCoordinator, Session: s.id}); err != nil {
		return err
	}
	return s.challenge(now)
}

// challenge sends a fresh challenge and starts its clock (SeatHook.tla, Hook and
// Rechallenge).
func (s *hookSession) challenge(now time.Time) error {
	c := hookNonce()
	s.mu.Lock()
	s.chal, s.chalAt = c, now
	s.mu.Unlock()
	return s.send(hookMsg{T: "challenge", Challenge: c})
}

// send writes one line to the session; a write that fails ends the subscription.
func (s *hookSession) send(m hookMsg) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err = s.conn.Write(append(b, '\n'))
	return err
}

// current says the session is the hub's open subscription.
func (s *hookSession) current() bool {
	h := s.a.hookHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cur[s.name] == s
}

// read takes the session's lines until the connection ends, and returns why it ended.
func (s *hookSession) read(ctx context.Context) string {
	sc := bufio.NewScanner(s.conn)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		var m hookMsg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			_ = s.send(hookMsg{T: "refused", Why: "a line of the hook is one JSON object: " + err.Error()}) // ignored: a session that does not read ends at its next write
			continue
		}
		switch m.T {
		case "prove":
			s.answer(m, s.prove(ctx, m.Challenge))
		case "ack":
			s.answer(m, s.ack(ctx, m.ID, m.Session))
		case "ack-all":
			s.answer(m, s.ackAll(ctx))
		case "alive":
			// the hook client's keepalive, every sprint.HookKeepalive while its process holds
			// the stream: the transport is alive (no more: it is no proof the seat attends),
			// so a connected hook is not lapsed for a quiet seat; answered by nothing
			s.touch(s.a.now())
		case "unhook":
			s.end(ctx, sprint.HookAway, "nova-sprint unhook")
			_ = s.send(hookMsg{T: "ok", Of: "unhook", Req: m.Req, Name: s.name}) // ignored: the end is recorded
			_ = s.send(hookMsg{T: "bye", Why: "unhooked: the seat is recorded away"})
			_ = s.conn.Close()
			return "nova-sprint unhook"
		default:
			_ = s.send(hookMsg{T: "refused", Req: m.Req, Of: m.T, Why: "the hook takes prove, ack, ack-all and unhook"})
		}
		if s.isEnded() {
			return "ended"
		}
	}
	if err := sc.Err(); err != nil {
		return "the connection dropped: " + err.Error()
	}
	return "the connection closed without nova-sprint unhook"
}

// answer sends the server's answer to one of the session's lines.
func (s *hookSession) answer(m hookMsg, reply hookMsg) {
	reply.Req, reply.Of = m.Req, m.T
	_ = s.send(reply) // ignored: a session that does not read ends at its next write
}

// prove takes the session's answer to the outstanding challenge (SeatHook.tla, Prove):
// only that challenge, only over this subscription, while it is the open one; the
// record is then proven and live until the next challenge's bound.
func (s *hookSession) prove(ctx context.Context, c string) hookMsg {
	s.mu.Lock()
	ok := c != "" && s.chal != "" && c == s.chal && !s.ended
	s.mu.Unlock()
	if !ok || !s.current() {
		return hookMsg{T: "refused", Why: "that is not the challenge outstanding on this hook: answer the last HOOK CHALLENGE its stream showed"}
	}
	now := s.a.now()
	var rec sprint.HookRecord
	err := s.a.onLine(ctx, func(st *store.Store) error {
		r, _, err := readHook(ctx, st)
		if err != nil {
			return err
		}
		if r.Session != s.id {
			return errors.New("the record is another hook's")
		}
		rec = sprint.HookProven(r, now)
		return writeHook(ctx, st, rec)
	})
	if err != nil {
		return hookMsg{T: "refused", Why: "the proof could not be recorded: " + err.Error()}
	}
	s.mu.Lock()
	s.chal, s.proven, s.acted, s.liveAt = "", now, now, rec.Live
	s.mu.Unlock()
	s.a.hookerForget(s.name) // the bus push's proof, written just before, is read afresh
	s.closeLapsed(ctx)
	s.a.serveSay("SEAT HOOK name=%s session=%s: proven until %s", s.name, s.id[:8], rec.Live.UTC().Format(time.RFC3339))
	return hookMsg{T: "ok", Name: s.name, Live: rec.Live, Unacked: rec.Unacked}
}

// ack takes the session's acknowledgement of every push delivered up to id (SeatHook.tla,
// Ack); the record then names the oldest still open.
func (s *hookSession) ack(ctx context.Context, id uint64, session string) hookMsg {
	if session == "" || session != s.id {
		return hookMsg{T: "refused", Why: "this acknowledgement belongs to another hook session; copy the session token from the PUSH line"}
	}
	rec, _, why, err := s.ackUpTo(ctx, id)
	if why != "" {
		return hookMsg{T: "refused", Why: why}
	}
	if err != nil {
		return hookMsg{T: "refused", Why: "the acknowledgement could not be recorded: " + err.Error()}
	}
	s.touch(s.a.now()) // an acknowledgement is an act of the seat
	return hookMsg{T: "ok", ID: id, Unacked: rec.Unacked, Live: rec.Live}
}

// ackAll acknowledges every push this hook has sent up to now, in the session it
// holds: the client sends no id or token, and the hook acks up to its own last id
// with its own session (SeatHook.tla, Ack — the same action as
// --ack <its last id> --session <its session>, so no model change is needed).
// With nothing sent yet it acks none and still answers ok.
func (s *hookSession) ackAll(ctx context.Context) hookMsg {
	s.mu.Lock()
	upTo := s.seq
	s.mu.Unlock()
	if upTo == 0 {
		s.touch(s.a.now())
		return hookMsg{T: "ok", ID: 0, Session: s.id, Acked: 0, Unacked: 0}
	}
	rec, acked, why, err := s.ackUpTo(ctx, upTo)
	if why != "" {
		return hookMsg{T: "refused", Why: why}
	}
	if err != nil {
		return hookMsg{T: "refused", Why: "the acknowledgement could not be recorded: " + err.Error()}
	}
	s.touch(s.a.now())
	return hookMsg{T: "ok", ID: upTo, Session: s.id, Acked: acked, Unacked: rec.Unacked, Live: rec.Live}
}

// ackUpTo is the shared cumulative acknowledgement: every open push up to id is
// acked, counted, and recorded; a failed record write rolls the ack back.
func (s *hookSession) ackUpTo(ctx context.Context, id uint64) (rec sprint.HookRecord, acked int, why string, err error) {
	err = s.a.onLine(ctx, func(st *store.Store) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if id == 0 || id > s.seq {
			why = fmt.Sprintf("no push %d was delivered on this hook (the last is %d)", id, s.seq)
			return nil
		}
		openBefore, ackedBefore := append([]sprint.HookItem(nil), s.open...), cloneKeys(s.acked)
		kept := s.open[:0]
		for _, item := range s.open {
			if item.ID <= id {
				acked++
				for _, key := range item.Keys {
					s.acked[key] = true
				}
				continue
			}
			kept = append(kept, item)
		}
		s.open = kept
		var rerr error
		rec, rerr = s.recordLocked(ctx, st)
		if rerr != nil {
			s.open, s.acked = openBefore, ackedBefore
			acked = 0
		}
		return rerr
	})
	return rec, acked, why, err
}

// recordLocked writes the session's transport state into the hook record. The caller
// holds s.mu and the store's onLine serialization, so pending delivery and its durable
// note keys are one recorded step.
func (s *hookSession) recordLocked(ctx context.Context, st *store.Store) (sprint.HookRecord, error) {
	r, _, err := readHook(ctx, st)
	if err != nil {
		return sprint.HookRecord{}, err
	}
	if r.Session != s.id {
		return sprint.HookRecord{}, errors.New("the record is another hook's")
	}
	r.Epoch = s.epoch
	r.PushSeq = s.seq
	r.Unacked, r.Oldest = len(s.open), nil
	if len(s.open) > 0 {
		o := s.open[0]
		r.Oldest = &o
	}
	r.Acked = keyList(s.acked)
	if err := writeHook(ctx, st, r); err != nil {
		return sprint.HookRecord{}, err
	}
	return r, nil
}

// deliveryKeys identifies each open note separately, including its explicit wait
// deadline. A changed Review/ReviewSet is a new delivery generation, so an ACK made
// before a wait cannot suppress that judgment's later due wake.
func deliveryKeys(g sprint.Group, open []sprint.Open, now time.Time, stopped func(time.Time, time.Time) time.Duration) (active, eligible []string) {
	keys := noteKeys(g)
	if g.HookKey != "" {
		keys = []string{g.HookKey}
	}
	if g.Kind != sprint.Judgment {
		return keys, keys
	}
	notes := make(map[string]sprint.Note, len(open))
	for _, o := range open {
		notes[noteKey(o.Note.ID)] = o.Note
	}
	for _, key := range keys {
		n, ok := notes[key]
		if ok && !n.Review.IsZero() {
			key += "\x00review=" + n.Review.UTC().Format(time.RFC3339Nano) + "\x00set=" + n.ReviewSet.UTC().Format(time.RFC3339Nano)
		}
		active = append(active, key)
		if ok && !n.Review.IsZero() && !sprint.DueNow(now, n.Review, n.ReviewSet, stopped) {
			continue
		}
		eligible = append(eligible, key)
	}
	return active, eligible
}

// look is the server's turn at the subscription every hookLook: the first challenge
// unanswered past its bound unhooks (SeatHook.tla, Miss); a proven hook with no act past
// sprint.HookSilentBound lapses, once, with one judgment (lapse; SeatHook.tla, Lapse); a
// hook with acts has its record's Live moved on (renewLive; SeatHook.tla, Act); and every
// judgment and note to the coordinator not yet sent on this subscription is delivered, each
// with its id (Deliver). No challenge is asked again on a connection that stays up (the
// rechallenge every HookEvery is gone: SeatHook.tla, HookStays). Deliver writes the record
// that names the pushes in the same step (docs/SPEC-SPRINT.md, "The hook";
// SeatHook.tla, AckedOrBlocking), so a look whose record write fails delivers nothing
// and undoes what it added, and the next look offers the pushes again.
func (s *hookSession) look(ctx context.Context) {
	if s.isEnded() || !s.current() {
		return
	}
	now := s.a.now()
	s.mu.Lock()
	chal, chalAt, proven, acted := s.chal, s.chalAt, s.proven, s.acted
	s.mu.Unlock()
	switch {
	case chal != "" && now.Sub(chalAt) > sprint.HookAnswerBound:
		_ = s.send(hookMsg{T: "bye", Why: "the challenge was not answered in time: the seat is recorded gone; run nova-sprint hook again"}) // ignored: it is ended either way
		s.end(ctx, sprint.HookGone, fmt.Sprintf("the challenge sent at %s was not answered within %s", chalAt.UTC().Format(time.RFC3339), sprint.HookAnswerBound))
		return
	case !proven.IsZero() && now.Sub(acted) > sprint.HookSilentBound:
		s.lapse(ctx, acted)
		return
	}
	s.renewLive(ctx)
	var pushes []hookMsg
	// The inbox snapshot, durable ACK state, and pending pushes move together. A
	// frame is sent only after the record contains each push; a failed write rolls the
	// whole look back so the next one offers the same work again.
	var sentBefore, ackedBefore map[string]bool
	var openBefore []sprint.HookItem
	var seqBefore uint64
	var epochBefore uint64
	var warnedBefore map[string]bool
	var warnedAtBefore time.Time
	var warnSetBefore string
	var warn *hookMsg
	err := s.a.onLine(ctx, func(st *store.Store) error {
		v, err := st.Inbox(ctx, defaultDeadline, defaultStale, 10000)
		if err != nil {
			return err
		}
		epoch, err := st.EpochNow(ctx)
		if err != nil {
			return err
		}
		var stopped func(time.Time, time.Time) time.Duration
		for _, o := range v.Open {
			if !o.Note.Review.IsZero() {
				machine, _, err := st.Machine(ctx)
				if err != nil {
					return err
				}
				stopped = machine.StoppedBetween
				break
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		seqBefore = s.seq
		epochBefore = s.epoch
		openBefore = append([]sprint.HookItem(nil), s.open...)
		sentBefore, ackedBefore = cloneKeys(s.sent), cloneKeys(s.acked)
		warnedBefore, warnedAtBefore, warnSetBefore = s.warned, s.warnedAt, s.warnSet
		if s.epoch != epoch.N {
			s.epoch, s.acked, s.sent = epoch.N, map[string]bool{}, map[string]bool{}
			s.open = nil
			// a new epoch's judgments are warned about afresh (sprint.JudgmentWarnDue)
			s.warnSet, s.warned, s.warnedAt = "", nil, time.Time{}
		}
		active := map[string]bool{}
		for _, g := range v.Groups {
			if !forCoordinator(g) {
				continue
			}
			if s.quietHappened && g.Kind == sprint.Happened {
				continue // hook --quiet-happened: judgments and challenges only
			}
			allKeys, keys := deliveryKeys(g, v.Open, now, stopped)
			for _, k := range allKeys {
				active[k] = true
			}
			if len(keys) == 0 {
				continue
			}
			fresh := false
			for _, k := range keys {
				if s.acked[k] {
					continue
				}
				if !s.sent[k] {
					fresh = true
				}
				s.sent[k] = true
			}
			if !fresh {
				continue
			}
			if s.seq == ^uint64(0) {
				s.seq, s.open, s.sent, s.acked, s.epoch = seqBefore, openBefore, sentBefore, ackedBefore, epochBefore
				pushes = nil
				return errors.New("the hook push sequence is exhausted; no push was sent")
			}
			s.seq++
			s.open = append(s.open, sprint.HookItem{ID: s.seq, What: g.ID, At: now, Keys: append([]string(nil), keys...)})
			pushes = append(pushes, hookMsg{T: "push", ID: s.seq, Session: s.id, What: g.ID, Text: groupText(g, now, false)})
		}
		for k := range s.acked {
			if !active[k] {
				delete(s.acked, k)
			}
		}
		for k := range s.sent {
			if !active[k] {
				delete(s.sent, k)
			}
		}
		// the waiting-judgment warning: one push listing every judgment open
		// sprint.JudgmentWarnAfter or longer, until it is answered, at a bounded rate: at
		// once when a judgment joins the set, else at most once a sprint.JudgmentWarnEvery
		// (sprint.JudgmentWarnDue; docs/SPEC-SPRINT.md, "The waiting-judgment warning"):
		// its full text when the set changed since the last push, else its one line. It is
		// not an open push (an ack does not silence it and it never blocks the gate); it
		// goes after this look's own pushes.
		if ws := sprint.JudgmentWarnings(v.Groups, now, s.firstSeen); len(ws) > 0 {
			seen := make(map[string]bool, len(ws))
			for _, w := range ws {
				seen[w.ID] = true
			}
			if sprint.JudgmentWarnDue(ws, s.warned, s.warnedAt, now) {
				s.seq++
				set := sprint.JudgmentWarningSet(ws)
				text := set
				if set != s.warnSet {
					text = sprint.JudgmentWarningText(ws)
				}
				s.warnSet, s.warnedAt = set, now
				warn = &hookMsg{T: "push", ID: s.seq, What: hookWarnWhat, Text: text}
			}
			s.warned = seen
		} else {
			s.warnSet, s.warned, s.warnedAt = "", nil, time.Time{}
		}
		if len(pushes) == 0 && epochBefore == s.epoch && maps.Equal(ackedBefore, s.acked) {
			return nil
		}
		if _, err := s.recordLocked(ctx, st); err != nil {
			s.seq, s.open, s.sent, s.acked, s.epoch = seqBefore, openBefore, sentBefore, ackedBefore, epochBefore
			s.warned, s.warnedAt, s.warnSet = warnedBefore, warnedAtBefore, warnSetBefore
			pushes = nil
			warn = nil
			return err
		}
		return nil
	})
	if err != nil {
		s.a.serveSay("SEAT HOOK name=%s: the inbox could not be read: %s", s.name, oneline.Escape(err.Error()))
		return
	}
	if len(pushes) == 0 {
		if warn != nil {
			if err := s.send(*warn); err != nil {
				s.end(ctx, sprint.HookGone, "a push could not be written to the hook: "+err.Error())
				_ = s.conn.Close()
			}
		}
		return
	}
	if warn != nil {
		pushes = append(pushes, *warn)
	}
	for _, m := range pushes {
		if err := s.send(m); err != nil {
			s.end(ctx, sprint.HookGone, "a push could not be written to the hook: "+err.Error())
			_ = s.conn.Close()
			return
		}
	}
}

// touch records an act at now: the proof, an acknowledgement, a coordinator verb, or the
// client's keepalive (transport only). The next look moves the record's Live on from it.
func (s *hookSession) touch(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.After(s.acted) {
		s.acted = now
	}
}

// hookRenewStep is how far the acts must move Live on before a look writes it: one record
// write a step, never one a look. The bound it leaves (Stella's read of #124): the recorded
// Live is never earlier than the last act + sprint.HookSilentBound - hookRenewStep -
// hookLook, so a gate reading the record sees a live hook for at least HookSilentBound
// less 32 s after the last act, and the lapse itself is judged on the act, not the record.
const hookRenewStep = 30 * time.Second

// renewLive writes the record's Live as sprint.HookActed says it from the last act, when
// that moves it on by hookRenewStep or more. A write that fails leaves the record as it was
// and is tried again at the next look.
func (s *hookSession) renewLive(ctx context.Context) {
	s.mu.Lock()
	acted, liveAt, proven := s.acted, s.liveAt, s.proven
	s.mu.Unlock()
	if proven.IsZero() || acted.Add(sprint.HookSilentBound).Sub(liveAt) < hookRenewStep {
		return
	}
	var live time.Time
	err := s.a.onLine(ctx, func(st *store.Store) error {
		r, _, err := readHook(ctx, st)
		if err != nil {
			return err
		}
		if r.Session != s.id {
			return errors.New("the record is another hook's")
		}
		r = sprint.HookActed(r, acted)
		live = r.Live
		return writeHook(ctx, st, r)
	})
	if err != nil {
		s.a.serveSay("SEAT HOOK name=%s: the act could not be recorded: %s", s.name, oneline.Escape(err.Error()))
		return
	}
	s.mu.Lock()
	s.liveAt = live
	s.mu.Unlock()
	// the bus push's proof moves on with the record: both pushes stay live together
	// (the seat keeps acting, so its bus subscription is still held)
	if hk, err := s.a.hooker(ctx); err == nil {
		if err := hk.Prove(ctx, s.name, s.a.now()); err == nil {
			s.a.hookerForget(s.name)
		}
	}
}

// lapse ends a connected, proven hook with no act past sprint.HookSilentBound: no verb, no
// acknowledgement, and not even the client's keepalive (the transport itself went quiet).
// One judgment (sprint.NHookLapsed, naming the seat in Who) is written first; only once it
// is written are the bye sent and the end recorded, gone, the owner told. A judgment write
// that fails leaves the subscription as it is, says so on the SEAT HOOK line, and the next
// look (hookLook later) tries again (Stella's read of #124: the lapse is never told without
// its judgment).
func (s *hookSession) lapse(ctx context.Context, acted time.Time) {
	why := fmt.Sprintf("no act past %s: the last at %s (no verb, no acknowledgement, and no keepalive from the hook client: the transport went quiet too)", sprint.HookSilentBound, acted.UTC().Format(time.RFC3339))
	err := s.a.onLine(ctx, func(st *store.Store) error {
		n := sprint.Note{Kind: sprint.Judgment, Type: sprint.NHookLapsed, StreamLevel: true, Who: s.name, What: s.name + "'s hook lapsed: " + why + "; run: nova-sprint hook",
			Decisions: []string{"nova-sprint hook", "ack"}}
		_, err := st.Run(ctx, store.NoteStep("hook", n))
		return err
	})
	if err != nil {
		s.a.serveSay("SEAT HOOK name=%s: the lapse's judgment could not be written, tried again at the next look: %s", s.name, oneline.Escape(err.Error()))
		return
	}
	_ = s.send(hookMsg{T: "bye", Why: why + ": the seat is recorded gone; run nova-sprint hook again"}) // ignored: it is ended either way
	s.end(ctx, sprint.HookGone, why)
}

// closeLapsed acknowledges the open NHookLapsed judgments of this seat (Who is its name)
// once it proves a new hook: its lapse is answered by the hook itself. Another seat's lapse
// stays open: a seat that proves in answers its own, never an earlier holder's.
func (s *hookSession) closeLapsed(ctx context.Context) {
	err := s.a.onLine(ctx, func(st *store.Store) error {
		open, err := st.B.OpenNotes(ctx)
		if err != nil {
			return err
		}
		var ids []string
		for _, o := range open {
			if o.Note.Type == sprint.NHookLapsed && o.Note.Who == s.name && !slices.Contains(ids, o.Note.ID) {
				ids = append(ids, o.Note.ID)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		_, err = st.Run(ctx, store.AckStep(sprint.AckReq{Notes: ids, Reason: "hooked in again", Who: s.name}))
		return err
	})
	if err != nil {
		s.a.serveSay("SEAT HOOK name=%s: the lapse could not be closed: %s", s.name, oneline.Escape(err.Error()))
	}
}

// seatActs is an act of the seat's holder: a verb it ran that the gate let through. The
// subscription open for it, if any, is touched.
func (a *app) seatActs(actor string) {
	if actor == "" {
		return
	}
	h := a.hookHub()
	h.mu.Lock()
	cur := h.cur[actor]
	h.mu.Unlock()
	if cur != nil {
		cur.touch(a.now())
	}
}

func (s *hookSession) isEnded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended
}

// replaced ends a subscription a new hook took the place of: its connection closed, and
// nothing recorded (the record is the new one's).
func (s *hookSession) replaced() {
	s.endOnce.Do(func() {
		s.mu.Lock()
		s.ended = true
		s.mu.Unlock()
		close(s.done)
		_ = s.send(hookMsg{T: "bye", Why: "replaced by a new nova-sprint hook"}) // ignored: it is closed next
		_ = s.conn.Close()
	})
}

// end ends the subscription once: the record says state (away for unhook, gone for
// anything else) with why, at once (SeatHook.tla, Drop, Unhook, Miss), and the owner is
// told.
func (s *hookSession) end(ctx context.Context, state, why string) {
	s.endOnce.Do(func() {
		s.mu.Lock()
		s.ended = true
		s.mu.Unlock()
		close(s.done)
		h := s.a.hookHub()
		h.mu.Lock()
		mine := h.cur[s.name] == s
		if mine {
			delete(h.cur, s.name)
		}
		h.mu.Unlock()
		if !mine {
			return
		}
		s.a.hookerForget(s.name)
		now := s.a.now()
		var owner string
		err := s.a.onLine(ctx, func(st *store.Store) error {
			rec, _, err := readHook(ctx, st)
			if err != nil {
				return err
			}
			if rec.Session != s.id {
				return errors.New("the record is another hook's")
			}
			s.mu.Lock()
			rec.Name, rec.Epoch, rec.Session, rec.State, rec.Since, rec.Why = s.name, s.epoch, s.id, state, now, why
			rec.Proven, rec.Live, rec.Unacked, rec.Oldest = time.Time{}, time.Time{}, 0, nil
			rec.Acked = keyList(s.acked)
			s.mu.Unlock()
			if err := writeHook(ctx, st, rec); err != nil {
				return err
			}
			owner, err = s.a.owner(ctx, st)
			return err
		})
		if err != nil {
			s.a.serveSay("SEAT HOOK name=%s: the end (%s) could not be recorded: %s", s.name, state, oneline.Escape(err.Error()))
		}
		s.a.tellOwner(ctx, owner, s.name, state, why)
		if state != sprint.HookAway {
			_ = s.conn.Close() // ignored: a drop's connection is gone already
		}
	})
}

// tellOwner pushes the seat's end to the owner: one bus message, and the server's line.
func (a *app) tellOwner(ctx context.Context, owner, name, state, why string) {
	line := fmt.Sprintf("SEAT HOOK name=%s state=%s why=%s", name, state, oneline.Quote(why))
	a.serveSay("%s", line)
	if owner == "" {
		a.serveSay("SEAT HOOK name=%s: the sprint names no owner: the end is told on this line only", name)
		return
	}
	subject := "seat " + name + " is " + state
	body := "The coordinator " + name + "'s hook ended: " + state + " (" + why + "). Every coordinator command is refused until it runs nova-sprint hook again and proves it."
	if err := a.bus(ctx, bus.Message{From: "nova-sprint", To: []string{owner}, Kind: bus.KindStatus, Subject: subject, Body: body}, func(l string) { a.serveSay("%s", l) }); err != nil {
		a.serveSay("SEAT HOOK name=%s: the push to %s failed: %s", name, owner, oneline.Escape(err.Error()))
	}
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

// hookServerStart is the server's start: a record left hooked by an earlier server is a
// subscription that server held, gone with it; it is recorded gone and told.
// tla/SeatHook.tla ServerStart
func (a *app) hookServerStart(ctx context.Context) {
	var rec sprint.HookRecord
	var ok bool
	var owner string
	err := a.onLine(ctx, func(st *store.Store) error {
		var err error
		rec, ok, err = readHook(ctx, st)
		if err != nil || !ok || rec.State != sprint.HookHooked {
			return err
		}
		rec.State, rec.Since, rec.Why = sprint.HookGone, a.now(), "the server restarted: the subscription it held is gone"
		rec.Proven, rec.Live, rec.Unacked, rec.Oldest = time.Time{}, time.Time{}, 0, nil
		if err := writeHook(ctx, st, rec); err != nil {
			return err
		}
		owner, err = a.owner(ctx, st)
		return err
	})
	if err != nil {
		a.serveSay("SEAT HOOK: the record could not be read at the server's start: %s", oneline.Escape(err.Error()))
		return
	}
	if ok && rec.State == sprint.HookGone && rec.Why == "the server restarted: the subscription it held is gone" {
		a.tellOwner(ctx, owner, rec.Name, rec.State, rec.Why)
	}
	// the bus store is dialled, and the holder's bus push read, off the line now, so the
	// first gate on the line reads memory (hookerShared)
	go func() {
		var holder string
		if err := a.onLine(ctx, func(st *store.Store) error {
			var err error
			holder, err = st.B.Coordinator(ctx)
			return err
		}); err == nil && holder != "" {
			_, _ = a.hookerShared(ctx, holder) // ignored: a gate reads it again, and refuses on its error
		}
	}()
}

// --- the session's side ---

// hookMonitor is the command the seat's session runs under a Monitor: the hook, held
// until it ends, one event a line.
func hookMonitor(name string) string {
	return "nova-sprint hook --actor " + orDashStr(name, "<seat>")
}

// hookSocket is where this machine's hook process takes its session's lines for name:
// the user's cache directory, nova-sprint/hook-<name>.sock, mode 0700 directory.
func (a *app) hookSocket(name string) (string, error) {
	if a.hookSock != nil {
		return a.hookSock(name)
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "nova-sprint")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "hook-"+name+".sock"), nil
}

// cmdHook is nova-sprint hook: with no flag, the seat's subscription, held until it ends;
// with --prove, --ack and --session, or --ack-all (no id or token), the session's word
// on it, carried by the running hook.
func (a *app) cmdHook(args []string, stdout, stderr io.Writer) int {
	const name = "hook"
	fs, c := a.verbSetup(name)
	prove := fs.String("prove", "", "the challenge the hook's stream showed (HOOK CHALLENGE <c>): the session's answer, carried over the running hook's connection; the hook counts once the server takes it")
	ack := fs.Uint64("ack", 0, "acknowledge every push the hook's stream showed up to this id (PUSH <id>), over the running hook's connection")
	ackAll := fs.Bool("ack-all", false, "acknowledge every push the running hook has sent up to now, in the session it holds: no id or session token to copy, over the running hook's connection")
	session := fs.String("session", "", "the session token printed on the PUSH line being acknowledged")
	quiet := fs.Bool("quiet-happened", false, "receive judgments and challenges only: the informational HAPPENED pushes are not delivered")
	compact := fs.Bool("compact", false, "print only what the seat acts on: the HOOK lines and each JUDGMENT cut to one line plus its first detail line (no HAPPENED pushes, no verb menus), each line written and flushed at once; with --overdue, a card-holding judgment unanswered that long is named by one OVERDUE line, again every interval until it is answered")
	overdue := fs.Duration("overdue", hookOverdueDefault, "with --compact: how long a card-holding judgment may go unanswered before one OVERDUE line names it, and how often the line repeats until it is answered")
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, name, argErr("takes no words ", err, pos...))
	}
	if !sprint.ValidID(c.actor) {
		return refuse(stderr, name, "hook wants --actor <name> (or NOVA_SPRINT_ACTOR): the seat's holder; nothing was changed")
	}
	if *overdue != hookOverdueDefault && !*compact {
		return refuse(stderr, name, "--overdue is only used with --compact")
	}
	switch {
	case *prove != "" && *ack != 0:
		return refuse(stderr, name, "--prove and --ack are one at a time")
	case *prove != "" && *ackAll:
		return refuse(stderr, name, "--prove and --ack-all are one at a time; run: nova-sprint hook --ack-all")
	case *ack != 0 && *ackAll:
		return refuse(stderr, name, "--ack and --ack-all are one at a time; run: nova-sprint hook --ack-all")
	case *session != "" && *ackAll:
		return refuse(stderr, name, "--session is not used with --ack-all: it sends no token, the hook uses the session it holds; nothing was changed; run: nova-sprint hook --ack-all")
	case *session != "" && *ack == 0:
		return refuse(stderr, name, "--session is only used with --ack")
	case *prove != "":
		return a.hookAsk(c.actor, hookMsg{T: "prove", Challenge: strings.TrimSpace(*prove)}, stdout, stderr)
	case *ackAll:
		return a.hookAsk(c.actor, hookMsg{T: "ack-all"}, stdout, stderr)
	case *ack != 0:
		if *session == "" {
			return refuse(stderr, name, "hook --ack requires --session <token> from the PUSH line; nothing was changed")
		}
		return a.hookAsk(c.actor, hookMsg{T: "ack", ID: *ack, Session: *session}, stdout, stderr)
	}
	// the sprint's server the environment names, else the seat's record: the same order
	// the coordinator's verbs use (forward.go, namedServer). (docs/SPEC-SPRINT.md, "The server".)
	addr, from := a.namedServer()
	if addr == "" {
		return refuse(stderr, name, a.noServerHint())
	}
	ctx, stop := a.notify(context.Background())
	defer stop()
	return a.hookHold(ctx, addr, from, c.actor, *quiet, *compact, *overdue, c.redis, stdout, stderr)
}

// cmdUnhook is nova-sprint unhook: the seat stops, over the running hook's connection;
// the server records it away and tells the owner, and the hook ends.
func (a *app) cmdUnhook(args []string, stdout, stderr io.Writer) int {
	const name = "unhook"
	fs, c := a.verbSetup(name)
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, name, argErr("takes no words ", err, pos...))
	}
	if !sprint.ValidID(c.actor) {
		return refuse(stderr, name, "unhook wants --actor <name> (or NOVA_SPRINT_ACTOR): the seat's holder; nothing was changed")
	}
	return a.hookAsk(c.actor, hookMsg{T: "unhook"}, stdout, stderr)
}

// hookAsk hands one of the session's lines to this machine's running hook and prints
// the server's answer: exit 0 taken, 1 refused or no hook running.
func (a *app) hookAsk(name string, m hookMsg, stdout, stderr io.Writer) int {
	verb := "hook"
	if m.T == "unhook" {
		verb = "unhook"
	}
	sock, err := a.hookSocket(name)
	if err != nil {
		fmt.Fprintf(stderr, "%s %s FAILED: %s\n", prog, verb, oneline.Escape(err.Error()))
		return 1
	}
	conn, err := net.DialTimeout("unix", sock, 5*time.Second)
	if err != nil {
		fmt.Fprintf(stderr, "%s %s REFUSED: no hook is running on this machine for %s (%s); run: nova-sprint hook, under a Monitor\n", prog, verb, oneline.Field(name), oneline.Escape(err.Error()))
		return 1
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(hookAskBound + 5*time.Second)) // ignored: a socket without deadlines waits
	b, _ := json.Marshal(m)                                            // ignored: a message of strings and numbers always encodes
	if _, err := conn.Write(append(b, '\n')); err != nil {
		fmt.Fprintf(stderr, "%s %s FAILED: %s\n", prog, verb, oneline.Escape(err.Error()))
		return 1
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	var r hookMsg
	if err == nil {
		err = json.Unmarshal(line, &r)
	}
	if err != nil {
		fmt.Fprintf(stderr, "%s %s FAILED: the hook did not answer: %s\n", prog, verb, oneline.Escape(err.Error()))
		return 1
	}
	if r.T != "ok" {
		fmt.Fprintf(stderr, "%s %s REFUSED: %s\n", prog, verb, oneline.Escape(r.Why))
		return 1
	}
	switch m.T {
	case "prove":
		fmt.Fprintf(stdout, "HOOK PROVEN name=%s live-until=%s unacked=%d\n", oneline.Field(name), r.Live.UTC().Format(time.RFC3339), r.Unacked)
	case "ack":
		fmt.Fprintf(stdout, "HOOK ACK OK name=%s through=%d unacked=%d\n", oneline.Field(name), r.ID, r.Unacked)
	case "ack-all":
		fmt.Fprintf(stdout, "HOOK ACK-ALL OK acked=%d up-to=%d session=%s\n", r.Acked, r.ID, oneline.Field(r.Session))
	default:
		fmt.Fprintf(stdout, "UNHOOK OK name=%s state=away: the owner is told; every coordinator command is refused until nova-sprint hook\n", oneline.Field(name))
	}
	return 0
}

// hookRelay is the hook process's carriage of its session's lines: each one is written
// on the connection with a req of its own, and the server's answer carrying that req is
// handed back.
type hookRelay struct {
	mu      sync.Mutex
	w       io.Writer
	n       int
	pending map[string]chan hookMsg
}

func (r *hookRelay) ask(m hookMsg) (hookMsg, error) {
	r.mu.Lock()
	r.n++
	m.Req = fmt.Sprintf("r%d", r.n)
	ch := make(chan hookMsg, 1)
	r.pending[m.Req] = ch
	b, _ := json.Marshal(m) // ignored: a message of strings and numbers always encodes
	_, err := r.w.Write(append(b, '\n'))
	r.mu.Unlock()
	if err != nil {
		return hookMsg{}, err
	}
	select {
	case a := <-ch:
		return a, nil
	case <-time.After(hookAskBound):
		r.mu.Lock()
		delete(r.pending, m.Req)
		r.mu.Unlock()
		return hookMsg{}, errors.New("the server did not answer within " + hookAskBound.String())
	}
}

// say writes one line on the connection that wants no answer (the keepalive).
func (r *hookRelay) say(m hookMsg) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, _ := json.Marshal(m) // ignored: a message of strings and numbers always encodes
	_, err := r.w.Write(append(b, '\n'))
	return err
}

func (r *hookRelay) answered(m hookMsg) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.pending[m.Req]
	if ok {
		delete(r.pending, m.Req)
		ch <- m
	}
	return ok
}

// hookBusSubject is the subject of the hook's bus challenge: the half of each challenge
// the hook process sends to its own name on the bus and reads back off its subscription,
// so an answer proves the bus push reaches the session (docs/SPEC-SPRINT.md, "The two
// pushes").
const hookBusSubject = "nova-sprint hook bus challenge "

// hookBusBlock is how long one read of the bus subscription waits for a message.
const hookBusBlock = 5 * time.Second

// hookHold is the caller's subscription, held until it ends: both its pushes. The bus
// push first (the shared record's store, NOVA_BUS_REDIS, and a subscription to the
// caller's own stream from its tail: refused, with nothing armed, when there is none),
// then the server's stream (the sprint push for the coordinator, the work push for a
// worker friend), the shared record armed for the role the server names, the socket for
// the session's lines opened, and every line from either push said on stdout, one
// event per line for the Monitor. Each challenge is answered once over both: the
// server's half and the bus half, read back off the bus (HOOK CHALLENGE <s>.<b>); the
// server takes its half, and the hook process records both proven in the shared record.
// Exit 0 after unhook; 1 for any other end, which the server records as gone.
func (a *app) hookHold(ctx context.Context, addr, from, name string, quiet, compact bool, overdue time.Duration, redis string, stdout, stderr io.Writer) int {
	const verb = "hook"
	hk, err := a.hooker(ctx)
	if err != nil {
		return refuse(stderr, verb, "the bus push cannot be armed: "+oneline.Escape(err.Error())+"; set "+busRedisEnv+" (and "+busUserEnv+" for its login) to the bus store, then run: "+hookMonitor(name)+"; nothing was armed")
	}
	w, err := a.hk.bus.Waiter()
	if err != nil {
		return refuse(stderr, verb, "the bus push cannot be armed: "+oneline.Escape(err.Error())+"; nothing was armed")
	}
	stream := bus.StreamOf(name)
	cursor, exists, err := w.Tail(ctx, stream)
	if err != nil {
		fmt.Fprintf(stderr, "%s %s FAILED: the bus subscription could not start: %s; nothing was armed\n", prog, verb, oneline.Escape(err.Error()))
		return 1
	}
	if !exists || cursor == "" {
		cursor = "0-0"
	}
	sock, err := a.hookSocket(name)
	if err != nil {
		fmt.Fprintf(stderr, "%s %s FAILED: %s\n", prog, verb, oneline.Escape(err.Error()))
		return 1
	}
	if c, err := net.DialTimeout("unix", sock, time.Second); err == nil {
		_ = c.Close()
		return refuse(stderr, verb, "a hook is already running on this machine for "+name+" ("+sock+"): one Monitor holds the seat; stop it, or answer through it")
	}
	_ = os.Remove(sock) // ignored: a socket left by a hook that died answers nothing
	ln, err := net.Listen("unix", sock)
	if err != nil {
		fmt.Fprintf(stderr, "%s %s FAILED: %s\n", prog, verb, oneline.Escape(err.Error()))
		return 1
	}
	defer func() { _ = ln.Close(); _ = os.Remove(sock) }() // ignored: the socket is this process's

	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return a.unanswered(verb, addr, err, stderr)
	}
	defer conn.Close()
	quietParam := ""
	if quiet {
		quietParam = "&quiet_happened=1"
	}
	fmt.Fprintf(conn, "GET %s?as=%s%s HTTP/1.1\r\nHost: %s\r\nUpgrade: %s\r\nConnection: Upgrade\r\n\r\n", hookPath, url.QueryEscape(name), quietParam, addr, hookUpgrade)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return a.unanswered(verb, addr, err, stderr)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096)) // ignored: the status says enough
		_ = resp.Body.Close()
		fmt.Fprintf(stderr, "%s %s REFUSED: %s\n", prog, verb, oneline.Escape(strings.TrimSpace(string(body))))
		return 1
	}
	out := &hookOut{w: stdout}
	hs := &hookHoldState{}
	subCtx, stopSub := context.WithCancel(ctx)
	defer stopSub()

	relay := &hookRelay{w: conn, pending: map[string]chan hookMsg{}}
	// the keepalive, every sprint.HookKeepalive while this process holds the stream: it
	// proves the transport alive, no more, so a connected hook is not lapsed for a quiet
	// seat; whether the seat attends is the pushes' and the judgments' to find out
	go func() {
		t := time.NewTicker(sprint.HookKeepalive)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if relay.say(hookMsg{T: "alive"}) != nil {
					return
				}
			}
		}
	}()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(hookAskBound + 5*time.Second)) // ignored: a socket without deadlines waits
				line, err := bufio.NewReader(c).ReadBytes('\n')
				var m hookMsg
				if err == nil {
					err = json.Unmarshal(line, &m)
				}
				var r hookMsg
				if err == nil {
					r, err = a.hookRelayed(ctx, hk, hs, relay, name, m)
				}
				if err != nil {
					r = hookMsg{T: "refused", Why: err.Error()}
				}
				b, _ := json.Marshal(r) // ignored: a message of strings and numbers always encodes
				_, _ = c.Write(append(b, '\n'))
			}(c)
		}
	}()
	go func() {
		<-ctx.Done()
		_ = conn.Close() // an interrupt ends the hook: the server records it gone
	}()
	// the bus push: every message to name, said as it arrives; the hook's own bus
	// challenge completes the HOOK CHALLENGE line
	go func() {
		fails := 0
		for subCtx.Err() == nil {
			es, err := w.BlockRead(subCtx, stream, cursor, hookBusBlock, 64)
			if err != nil {
				if subCtx.Err() != nil {
					return
				}
				if fails++; fails >= 3 {
					out.printf("HOOK BUS DOWN name=%s: the bus subscription failed (%s); the hook ends and every gated verb is refused; run: %s\n", oneline.Field(name), oneline.Escape(err.Error()), hookMonitor(name))
					_ = conn.Close()
					return
				}
				time.Sleep(time.Second)
				continue
			}
			fails = 0
			for _, e := range es {
				cursor = e.Entry
				m := e.Message()
				if c, ok := strings.CutPrefix(m.Subject, hookBusSubject); ok && m.From == name {
					if sc, ok := hs.busRead(c); ok {
						out.printf("HOOK CHALLENGE %s.%s: answer now, before anything else (one answer proves both pushes): nova-sprint hook --prove %s.%s --actor %s\n", sc, c, sc, c, oneline.Field(name))
					}
					continue
				}
				out.printf("BUS %s from=%s kind=%s subject=%s\n", oneline.Field(e.Entry), oneline.Field(m.From), m.KindName(), oneline.Quote(m.Subject))
				body := m.Body
				if len(body) > 2000 {
					body = body[:2000] + " ..."
				}
				for _, l := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
					if l != "" {
						out.printf("  %s\n", l)
					}
				}
			}
		}
	}()

	unhooked := false
	role := ""
	say := hookSay
	if compact {
		say = compactSay
		if overdue > 0 {
			stopOverdue := make(chan struct{})
			defer close(stopOverdue)
			go func() {
				t := time.NewTicker(overdue)
				defer t.Stop()
				for {
					select {
					case <-stopOverdue:
						return
					case <-ctx.Done():
						return
					case <-t.C:
						a.compactOverdue(ctx, redis, name, overdue, stdout)
					}
				}
			}()
		}
	}
	sc := bufio.NewScanner(br)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var m hookMsg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		if m.Req != "" && relay.answered(m) {
			if m.T == "ok" && m.Of == "prove" {
				out.printf("HOOK PROVEN name=%s live-until=%s: both pushes are live; every verb runs while the hook stays answered\n", oneline.Field(name), m.Live.UTC().Format(time.RFC3339))
			}
			if m.T == "ok" && m.Of == "unhook" {
				unhooked = true
			}
			continue
		}
		switch m.T {
		case "hello":
			role = cmp.Or(m.Role, sprint.HookRoleCoordinator)
			if err := hk.Arm(ctx, name, a.now()); err != nil {
				out.printf("HOOK BUS DOWN name=%s: the shared record could not be armed (%s); the hook ends; run: %s\n", oneline.Field(name), oneline.Escape(err.Error()), hookMonitor(name))
				_ = conn.Close()
				continue
			}
			out.printf("HOOK CONNECTED name=%s role=%s session=%s server=%s from=%s bus=%s: armed (the %s push and the %s push); not hooked until the challenge below is answered\n",
				oneline.Field(m.Name), role, oneline.Field(m.Session), oneline.Field(addr), oneline.Field(from), oneline.Field(stream), sprint.HookCoordinatorPushes[0], sprint.HookCoordinatorPushes[1])
			continue
		case "challenge":
			// the server's half; the bus half goes out on the bus and the line is said once
			// it comes back off the subscription
			busC := hookNonce()
			hs.challenge(m.Challenge, busC)
			msg := bus.Message{ID: "hook-" + busC, From: name, To: []string{name}, Kind: bus.KindStatus, Subject: hookBusSubject + busC, At: a.now(),
				Body: "nova-sprint hook's bus challenge for " + name + ": the hook reads it back off the bus, and the answer to HOOK CHALLENGE proves the bus push; nothing else to do"}
			if err := a.hk.bus.Store.AddAll(ctx, []string{stream}, msg.Fields()); err != nil {
				out.printf("HOOK BUS DOWN name=%s: the bus challenge could not be sent (%s); the hook ends; run: %s\n", oneline.Field(name), oneline.Escape(err.Error()), hookMonitor(name))
				_ = conn.Close()
			}
			continue
		}
		say(out, name, addr, from, m)
	}
	stopSub()
	if unhooked {
		hs.unhookWg.Wait()
		out.printf("HOOK ENDED name=%s: unhooked; both pushes are closed and recorded unhooked\n", oneline.Field(name))
		return 0
	}
	out.printf("HOOK DROPPED name=%s role=%s: the connection to the server ended; every gated verb is refused; run: %s\n", oneline.Field(name), orDashStr(role, "-"), hookMonitor(name))
	return 1
}

// hookHoldState is the hook process's challenge: the server's half outstanding, the
// bus half sent, and whether the bus half came back off the subscription.
type hookHoldState struct {
	mu       sync.Mutex
	sprintC  string
	busC     string
	busSeen  bool
	unhookWg sync.WaitGroup
}

func (h *hookHoldState) challenge(sprintC, busC string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sprintC, h.busC, h.busSeen = sprintC, busC, false
}

// busRead marks the bus half c read off the subscription, and answers the server's
// half when c is the one outstanding.
func (h *hookHoldState) busRead(c string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c == "" || c != h.busC {
		return "", false
	}
	h.busSeen = true
	return h.sprintC, true
}

// take is the server's half of answer when answer is <sprint>.<bus> for the challenge
// outstanding, its bus half read back off the bus, and the bus half; nothing is cleared
// here (used clears it once the server has taken its half).
func (h *hookHoldState) take(answer string) (sprintC, busC string, ok bool) {
	sc, bc, ok := strings.Cut(strings.TrimSpace(answer), ".")
	h.mu.Lock()
	defer h.mu.Unlock()
	if !ok || sc == "" || bc == "" || bc != h.busC || !h.busSeen {
		return "", "", false
	}
	return sc, bc, true
}

// used clears the bus half busC once the server took the answer: a challenge counts once.
func (h *hookHoldState) used(busC string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.busC == busC {
		h.busC, h.busSeen = "", false
	}
}

// hookRelayed carries one of the session's lines to the server and records what the
// server took in the shared record: a proof of both pushes (the bus half checked here,
// the server's half by the server), the unhook, or an acknowledgement.
func (a *app) hookRelayed(ctx context.Context, hk sprint.Hooker, hs *hookHoldState, relay *hookRelay, name string, m hookMsg) (hookMsg, error) {
	switch m.T {
	case "prove":
		sc, bc, ok := hs.take(m.Challenge)
		if !ok {
			return hookMsg{T: "refused", Why: "that is not the challenge outstanding on this hook: answer the last HOOK CHALLENGE its stream showed, both halves (<sprint>.<bus>)"}, nil
		}
		// the bus half is checked: its proof is recorded first, so the server's word,
		// once it takes its half, finds both (the gate wants both; a bus proof alone opens
		// nothing)
		if err := hk.Prove(ctx, name, a.now()); err != nil {
			return hookMsg{T: "refused", Why: "the bus push's proof could not be recorded in " + sprint.HookSharedKey(name) + ": " + err.Error()}, nil
		}
		r, err := relay.ask(hookMsg{T: "prove", Challenge: sc})
		if err != nil || r.T != "ok" {
			return r, err
		}
		hs.used(bc)
		return r, nil
	case "unhook":
		hs.unhookWg.Add(1)
		r, err := relay.ask(hookMsg{T: "unhook"})
		if err != nil || r.T != "ok" {
			hs.unhookWg.Done()
			return r, err
		}
		if err := hk.Unhook(ctx, name, a.now()); err != nil {
			hs.unhookWg.Done()
			return hookMsg{T: "refused", Why: "the server recorded the unhook, and the shared record " + sprint.HookSharedKey(name) + " could not be: " + err.Error()}, nil
		}
		hs.unhookWg.Done()
		return r, nil
	}
	return relay.ask(hookMsg{T: m.T, Challenge: m.Challenge, ID: m.ID, Session: m.Session})
}

// hookOut is the hook's stdout, written by the server's lines and the bus
// subscription's, one line at a time.
type hookOut struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *hookOut) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (l *hookOut) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(l, format, args...) // ignored: the Monitor's stdout; a write it cannot take has no one to tell
}

// hookSay is one line from the server as the Monitor shows it: one event a line, a
// push's text indented under its line. The connected line names the server the hook
// used and where it was named (addr, from: env or seat-record). (docs/SPEC-SPRINT.md,
// "The server".)
func hookSay(w io.Writer, name, addr, from string, m hookMsg) {
	var b strings.Builder
	switch m.T {
	case "hello":
		fmt.Fprintf(&b, "HOOK CONNECTED name=%s session=%s server=%s from=%s: not hooked until the challenge below is answered\n", oneline.Field(m.Name), oneline.Field(m.Session), oneline.Field(addr), oneline.Field(from))
	case "challenge":
		fmt.Fprintf(&b, "HOOK CHALLENGE %s: answer now, before anything else: nova-sprint hook --prove %s --actor %s\n", m.Challenge, m.Challenge, oneline.Field(name))
	case "push":
		if m.What == hookWarnWhat {
			if !strings.Contains(strings.TrimRight(m.Text, "\n"), "\n") {
				// the set has not changed: one line, at most once a minute
				fmt.Fprintf(w, "PUSH %d %s: %s\n", m.ID, hookWarnWhat, oneline.Escape(strings.TrimSpace(m.Text)))
				return
			}
			fmt.Fprintf(&b, "PUSH %d %s: answer the judgments below; an ack does not silence it\n", m.ID, hookWarnWhat)
		} else {
			fmt.Fprintf(&b, "PUSH %d %s session=%s: read it, then acknowledge: nova-sprint hook --ack %d --session %s --actor %s\n", m.ID, oneline.Field(m.What), oneline.Field(m.Session), m.ID, oneline.Field(m.Session), oneline.Field(name))
		}
		for _, l := range strings.Split(strings.TrimRight(m.Text, "\n"), "\n") {
			b.WriteString("  " + l + "\n")
		}
	case "bye":
		fmt.Fprintf(&b, "HOOK BYE name=%s: %s\n", oneline.Field(name), oneline.Escape(m.Why))
	case "refused":
		fmt.Fprintf(&b, "HOOK REFUSED name=%s: %s\n", oneline.Field(name), oneline.Escape(m.Why))
	}
	_, _ = io.WriteString(w, b.String())
}
