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
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
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
// <challenge>` and `hook --ack <id>` hand their word to the hook process over a socket
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
	hookLook       = time.Second // the waiting-judgment warning repeats every second (sprint.JudgmentWarnings)
	hookWriteBound = 10 * time.Second
	hookAskBound   = 30 * time.Second
)

// hookWarnWhat is what a waiting-judgment warning push is (sprint.JudgmentWarnings).
const hookWarnWhat = "WARNING-judgments-waiting"

// hookMsg is one line on the hook's connection, either way. The server sends hello,
// challenge, push, ok, refused and bye; the session sends prove, ack and unhook, each
// with its req, which the server's answer carries back.
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
	if c.actor == "" || exempt || !hookArmed(c.actor) {
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
	mu        sync.Mutex
	cur       sprint.Hooker
	bus       *bus.Bus
	cache     map[string]hookCopy
	refilling bool
}

type hookCopy struct {
	rec sprint.HookShared
	at  time.Time
}

// hookerShared is identity's record for the gate: the copy when it is at most
// hookCacheFresh old, else read now (and kept). In a server the copies are refilled every
// hookCacheRefill off the line (hookerRefresh), so the read here is the first one only.
func (a *app) hookerShared(ctx context.Context, identity string) (sprint.HookShared, error) {
	a.hk.mu.Lock()
	if c, ok := a.hk.cache[identity]; ok && time.Since(c.at) <= hookCacheFresh {
		a.hk.mu.Unlock()
		return c.rec, nil
	}
	h, err := a.hookerLocked(ctx)
	a.hk.mu.Unlock()
	if err != nil {
		return sprint.HookShared{}, err
	}
	rec, _, err := h.Shared(ctx, identity)
	if err != nil {
		return sprint.HookShared{}, err
	}
	a.hk.mu.Lock()
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

// hookerRefresh keeps a server's copies fresh: every hookCacheRefill it reads each record
// the gate has asked for, off the line, and replaces the copy; a read that fails leaves
// the copy to age past hookCacheFresh, so the gate then reads for itself and refuses on
// the error (fail closed). It runs for the life of the server.
func (a *app) hookerRefresh(ctx context.Context) {
	t := time.NewTicker(hookCacheRefill)
	defer t.Stop()
	for range t.C {
		a.hk.mu.Lock()
		h := a.hk.cur
		names := slices.Collect(maps.Keys(a.hk.cache))
		a.hk.mu.Unlock()
		if h == nil {
			continue
		}
		for _, n := range names {
			rec, _, err := h.Shared(ctx, n)
			if err != nil {
				continue
			}
			a.hk.mu.Lock()
			a.hk.cache[n] = hookCopy{rec: rec, at: time.Now()}
			a.hk.mu.Unlock()
		}
	}
}

// hookerForget drops identity's copy: a write by this process (arm, prove, unhook) is
// read again at the next gate.
func (a *app) hookerForget(identity string) {
	a.hk.mu.Lock()
	delete(a.hk.cache, identity)
	a.hk.mu.Unlock()
}

// hookHub is the server's hold of the hooks: the one subscription open for each name
// (the seat's holder's, and each worker friend's). A new hook of a name replaces the
// one open for it.
type hookHub struct {
	mu  sync.Mutex
	cur map[string]*hookSession
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
	// (sprint.JudgmentWarnings).
	firstSeen map[string]time.Time
	warnSet   string
	ended     bool
	done      chan struct{}
	endOnce   sync.Once
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
	a.hookRun(context.Background(), name, &deadlineConn{Conn: conn, r: brw.Reader}, hookLook)
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
func (a *app) hookRun(ctx context.Context, name string, conn io.ReadWriteCloser, look time.Duration) {
	s := &hookSession{a: a, id: hookNonce(), name: name, conn: conn, sent: map[string]bool{}, firstSeen: map[string]time.Time{}, done: make(chan struct{})}
	if err := s.start(ctx); err != nil {
		s.end(ctx, sprint.HookGone, "the hook could not begin: "+err.Error())
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
	h.mu.Lock()
	old := h.cur[s.name]
	h.cur[s.name] = s
	h.mu.Unlock()
	if old != nil {
		old.replaced()
	}
	now := s.a.now()
	if err := s.a.onLine(ctx, func(st *store.Store) error {
		return writeHook(ctx, st, sprint.HookRecord{Name: s.name, Session: s.id, State: sprint.HookHooked, Since: now})
	}); err != nil {
		return err
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

// challengeLive moves the record's Live to a re-challenge's own bound: the challenge
// sent at now may be answered until now + HookAnswerBound, and the record is live until
// then (SeatHook.tla, Rechallenge; docs/SPEC-SPRINT.md, "The hook": Live is the
// outstanding challenge's bound). It goes through readHook and writeHook under onLine,
// the record's one writer (as record does), and keeps the later of the two bounds. A
// write that fails leaves the record as it was (the earlier bound: the safe side), says
// so on the server's SEAT HOOK line, and does not end the subscription.
func (s *hookSession) challengeLive(ctx context.Context, now time.Time) {
	err := s.a.onLine(ctx, func(st *store.Store) error {
		r, _, err := readHook(ctx, st)
		if err != nil {
			return err
		}
		if r.Session != s.id {
			return errors.New("the record is another hook's")
		}
		if live := now.Add(sprint.HookAnswerBound); live.After(r.Live) {
			r.Live = live
			return writeHook(ctx, st, r)
		}
		return nil
	})
	if err != nil {
		s.a.serveSay("SEAT HOOK name=%s: the challenge's bound could not be recorded: %s", s.name, oneline.Escape(err.Error()))
	}
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
			s.answer(m, s.ack(ctx, m.ID))
		case "unhook":
			s.end(ctx, sprint.HookAway, "nova-sprint unhook")
			_ = s.send(hookMsg{T: "ok", Of: "unhook", Req: m.Req, Name: s.name}) // ignored: the end is recorded
			_ = s.send(hookMsg{T: "bye", Why: "unhooked: the seat is recorded away"})
			_ = s.conn.Close()
			return "nova-sprint unhook"
		default:
			_ = s.send(hookMsg{T: "refused", Req: m.Req, Of: m.T, Why: "the hook takes prove, ack and unhook"})
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
	s.chal, s.proven = "", now
	s.mu.Unlock()
	s.a.hookerForget(s.name) // the bus push's proof, written just before, is read afresh
	s.a.serveSay("SEAT HOOK name=%s session=%s: proven until %s", s.name, s.id[:8], rec.Live.UTC().Format(time.RFC3339))
	return hookMsg{T: "ok", Name: s.name, Live: rec.Live, Unacked: rec.Unacked}
}

// ack takes the session's acknowledgement of every push delivered up to id (SeatHook.tla,
// Ack); the record then names the oldest still open.
func (s *hookSession) ack(ctx context.Context, id uint64) hookMsg {
	s.mu.Lock()
	if id == 0 || id > s.seq {
		seq := s.seq
		s.mu.Unlock()
		return hookMsg{T: "refused", Why: fmt.Sprintf("no push %d was delivered on this hook (the last is %d)", id, seq)}
	}
	s.open = slices.DeleteFunc(s.open, func(it sprint.HookItem) bool { return it.ID <= id })
	s.mu.Unlock()
	rec, err := s.record(ctx)
	if err != nil {
		return hookMsg{T: "refused", Why: "the acknowledgement could not be recorded: " + err.Error()}
	}
	return hookMsg{T: "ok", ID: id, Unacked: rec.Unacked, Live: rec.Live}
}

// record writes the pushes still open into the record (the gate's AckedOrBlocking), and
// returns it.
func (s *hookSession) record(ctx context.Context) (sprint.HookRecord, error) {
	var rec sprint.HookRecord
	err := s.a.onLine(ctx, func(st *store.Store) error {
		r, _, err := readHook(ctx, st)
		if err != nil {
			return err
		}
		if r.Session != s.id {
			return errors.New("the record is another hook's")
		}
		s.mu.Lock()
		r.Unacked, r.Oldest = len(s.open), nil
		if len(s.open) > 0 {
			o := s.open[0]
			r.Oldest = &o
		}
		s.mu.Unlock()
		rec = r
		return writeHook(ctx, st, r)
	})
	return rec, err
}

// look is the server's turn at the subscription every hookLook: a challenge unanswered
// past its bound unhooks (SeatHook.tla, Miss); a proven hook HookEvery old is challenged
// again (Rechallenge); and every judgment and note to the coordinator not yet sent on
// this subscription is delivered, each with its id (Deliver). Deliver writes the record
// that names the pushes in the same step (docs/SPEC-SPRINT.md, "The hook";
// SeatHook.tla, AckedOrBlocking), so a look whose record write fails delivers nothing
// and undoes what it added, and the next look offers the pushes again.
func (s *hookSession) look(ctx context.Context) {
	if s.isEnded() || !s.current() {
		return
	}
	now := s.a.now()
	s.mu.Lock()
	chal, chalAt, proven := s.chal, s.chalAt, s.proven
	s.mu.Unlock()
	switch {
	case chal != "" && now.Sub(chalAt) > sprint.HookAnswerBound:
		_ = s.send(hookMsg{T: "bye", Why: "the challenge was not answered in time: the seat is recorded gone; run nova-sprint hook again"}) // ignored: it is ended either way
		s.end(ctx, sprint.HookGone, fmt.Sprintf("the challenge sent at %s was not answered within %s", chalAt.UTC().Format(time.RFC3339), sprint.HookAnswerBound))
		return
	case chal == "" && !proven.IsZero() && now.Sub(proven) >= sprint.HookEvery:
		if err := s.challenge(now); err != nil {
			s.end(ctx, sprint.HookGone, "the challenge could not be sent: "+err.Error())
			return
		}
		s.challengeLive(ctx, now)
	}
	var pushes []hookMsg
	// what this look adds, to undo when the record write fails: the sent keys it set,
	// and the sequence and open pushes before it ran. A push is delivered only once the
	// record names it, so a look whose record write failed delivers none of its pushes
	// and offers them again next time (SeatHook.tla, Deliver: the record and the push
	// are one step; AckedOrBlocking).
	var addedSent []string
	var seqBefore uint64
	var openBefore int
	var warn *hookMsg
	// the inbox is read off the server's line of control (hookRead: the read lane), so a
	// look each second never holds the line; only a look with a push to record takes it
	var v store.InboxView
	err := s.a.hookRead(ctx, func(st *store.Store) error {
		var err error
		v, err = st.Inbox(ctx, defaultDeadline, defaultStale, 10000)
		return err
	})
	if err == nil {
		s.mu.Lock()
		seqBefore, openBefore = s.seq, len(s.open)
		for _, g := range v.Groups {
			if !forCoordinator(g) {
				continue
			}
			keys := noteKeys(g)
			fresh := false
			for _, k := range keys {
				if !s.sent[k] {
					fresh = true
					addedSent = append(addedSent, k)
				}
				s.sent[k] = true
			}
			if !fresh {
				continue
			}
			s.seq++
			s.open = append(s.open, sprint.HookItem{ID: s.seq, What: g.ID, At: now})
			pushes = append(pushes, hookMsg{T: "push", ID: s.seq, What: g.ID, Text: groupText(g, now, false)})
		}
		// the waiting-judgment warning: one push a look listing every judgment open
		// sprint.JudgmentWarnAfter or longer, every look until it is answered: its full
		// text when the set changed, else its one line. It is not an open push (an ack
		// does not silence it and it never blocks the gate); it goes after this look's
		// own pushes.
		if ws := sprint.JudgmentWarnings(v.Groups, now, s.firstSeen); len(ws) > 0 {
			s.seq++
			set := sprint.JudgmentWarningSet(ws)
			text := set
			if set != s.warnSet {
				text = sprint.JudgmentWarningText(ws)
			}
			s.warnSet = set
			warn = &hookMsg{T: "push", ID: s.seq, What: hookWarnWhat, Text: text}
		} else {
			s.warnSet = ""
		}
		s.mu.Unlock()
	}
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
	if _, err := s.record(ctx); err != nil {
		// the record was not written: deliver none of this look's pushes, and undo what
		// it added so the next look offers them again
		s.mu.Lock()
		for _, k := range addedSent {
			delete(s.sent, k)
		}
		if len(s.open) > openBefore {
			s.open = s.open[:openBefore]
		}
		s.seq = seqBefore
		s.mu.Unlock()
		s.a.serveSay("SEAT HOOK name=%s: the pushes could not be recorded: %s", s.name, oneline.Escape(err.Error()))
		return
	}
	for _, m := range pushes {
		if err := s.send(m); err != nil {
			s.end(ctx, sprint.HookGone, "a push could not be written to the hook: "+err.Error())
			_ = s.conn.Close()
			return
		}
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
			if err := writeHook(ctx, st, sprint.HookRecord{Name: s.name, Session: s.id, State: state, Since: now, Why: why}); err != nil {
				return err
			}
			var err error
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
// with --prove or --ack, the session's word on it, carried by the running hook.
func (a *app) cmdHook(args []string, stdout, stderr io.Writer) int {
	const name = "hook"
	fs, c := a.verbSetup(name)
	prove := fs.String("prove", "", "the challenge the hook's stream showed (HOOK CHALLENGE <c>): the session's answer, carried over the running hook's connection; the hook counts once the server takes it")
	ack := fs.Uint64("ack", 0, "acknowledge every push the hook's stream showed up to this id (PUSH <id>), over the running hook's connection")
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, name, argErr("takes no words ", err, pos...))
	}
	if !sprint.ValidID(c.actor) {
		return refuse(stderr, name, "hook wants --actor <name> (or NOVA_SPRINT_ACTOR): the seat's holder; nothing was changed")
	}
	switch {
	case *prove != "" && *ack != 0:
		return refuse(stderr, name, "--prove and --ack are one at a time")
	case *prove != "":
		return a.hookAsk(c.actor, hookMsg{T: "prove", Challenge: strings.TrimSpace(*prove)}, stdout, stderr)
	case *ack != 0:
		return a.hookAsk(c.actor, hookMsg{T: "ack", ID: *ack}, stdout, stderr)
	}
	// the sprint's server the environment names, else the seat's record: the same order
	// the coordinator's verbs use (forward.go, namedServer). (docs/SPEC-SPRINT.md, "The server".)
	addr, from := a.namedServer()
	if addr == "" {
		return refuse(stderr, name, a.noServerHint())
	}
	ctx, stop := a.notify(context.Background())
	defer stop()
	return a.hookHold(ctx, addr, from, c.actor, stdout, stderr)
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
func (a *app) hookHold(ctx context.Context, addr, from, name string, stdout, stderr io.Writer) int {
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
	cursor, _, err := w.Tail(ctx, stream)
	if err != nil {
		fmt.Fprintf(stderr, "%s %s FAILED: the bus subscription could not start: %s; nothing was armed\n", prog, verb, oneline.Escape(err.Error()))
		return 1
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
	fmt.Fprintf(conn, "GET %s?as=%s HTTP/1.1\r\nHost: %s\r\nUpgrade: %s\r\nConnection: Upgrade\r\n\r\n", hookPath, url.QueryEscape(name), addr, hookUpgrade)
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
		hookSay(out, name, addr, from, m)
	}
	stopSub()
	if unhooked {
		out.printf("HOOK ENDED name=%s: unhooked; both pushes are closed and recorded unhooked\n", oneline.Field(name))
		return 0
	}
	out.printf("HOOK DROPPED name=%s role=%s: the connection to the server ended; every gated verb is refused; run: %s\n", oneline.Field(name), orDashStr(role, "-"), hookMonitor(name))
	return 1
}

// hookHoldState is the hook process's challenge: the server's half outstanding, the
// bus half sent, and whether the bus half came back off the subscription.
type hookHoldState struct {
	mu      sync.Mutex
	sprintC string
	busC    string
	busSeen bool
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
		r, err := relay.ask(hookMsg{T: "unhook"})
		if err != nil || r.T != "ok" {
			return r, err
		}
		if err := hk.Unhook(ctx, name, a.now()); err != nil {
			return hookMsg{T: "refused", Why: "the server recorded the unhook, and the shared record " + sprint.HookSharedKey(name) + " could not be: " + err.Error()}, nil
		}
		return r, nil
	}
	return relay.ask(hookMsg{T: m.T, Challenge: m.Challenge, ID: m.ID})
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
	switch m.T {
	case "hello":
		fmt.Fprintf(w, "HOOK CONNECTED name=%s session=%s server=%s from=%s: not hooked until the challenge below is answered\n", oneline.Field(m.Name), oneline.Field(m.Session), oneline.Field(addr), oneline.Field(from))
	case "challenge":
		fmt.Fprintf(w, "HOOK CHALLENGE %s: answer now, before anything else: nova-sprint hook --prove %s --actor %s\n", m.Challenge, m.Challenge, oneline.Field(name))
	case "push":
		var b strings.Builder
		if m.What == hookWarnWhat {
			if !strings.Contains(strings.TrimRight(m.Text, "\n"), "\n") {
				// the set has not changed: one line a second
				fmt.Fprintf(w, "PUSH %d %s: %s\n", m.ID, hookWarnWhat, oneline.Escape(strings.TrimSpace(m.Text)))
				return
			}
			fmt.Fprintf(&b, "PUSH %d %s: answer the judgments below; an ack does not silence it\n", m.ID, hookWarnWhat)
		} else {
			fmt.Fprintf(&b, "PUSH %d %s: read it, then acknowledge: nova-sprint hook --ack %d --actor %s\n", m.ID, oneline.Field(m.What), m.ID, oneline.Field(name))
		}
		for _, l := range strings.Split(strings.TrimRight(m.Text, "\n"), "\n") {
			b.WriteString("  " + l + "\n")
		}
		_, _ = io.WriteString(w, b.String())
	case "bye":
		fmt.Fprintf(w, "HOOK BYE name=%s: %s\n", oneline.Field(name), oneline.Escape(m.Why))
	case "refused":
		fmt.Fprintf(w, "HOOK REFUSED name=%s: %s\n", oneline.Field(name), oneline.Escape(m.Why))
	}
}
