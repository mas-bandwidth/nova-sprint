package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
	hookLook       = 2 * time.Second
	hookWriteBound = 10 * time.Second
	hookAskBound   = 30 * time.Second
)

// hookMsg is one line on the hook's connection, either way. The server sends hello,
// challenge, push, ok, refused and bye; the session sends prove, ack and unhook, each
// with its req, which the server's answer carries back.
type hookMsg struct {
	T         string    `json:"t"`
	Req       string    `json:"req,omitempty"`
	Of        string    `json:"of,omitempty"`
	Name      string    `json:"name,omitempty"`
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
// binary, and every name in it, is armed.
var (
	hookArmedDefault = true
	hookTests        sync.Map // name -> any
)

// hookArmed says name's coordinator commands want a live, proven hook.
func hookArmed(name string) bool {
	if hookArmedDefault {
		return true
	}
	_, ok := hookTests.Load(name)
	return ok
}

// hookExempt are the verbs the gate never refuses: the hook itself and its end.
var hookExempt = map[string]bool{"hook": true, "unhook": true}

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

// hookedIn is why the command may not run for want of the seat's hook, "" when it may
// (SeatHook.tla, Gate; sprint.HookGate): every coordinator verb, and every other verb
// but a worker's, a report's and the machine's when the seat's holder runs it. The
// hook and unhook are never refused, nor is anything on a sprint with no holder (the
// first init).
func hookedIn(ctx context.Context, st *store.Store, c common) (string, error) {
	if hookExempt[c.verb] {
		return "", nil
	}
	class := verbClasses[c.verb]
	switch class {
	case classCoordinator, classSeat, classRead:
	default:
		return "", nil
	}
	// a read or a seat verb is gated for the holder alone: an actor the gate is not
	// armed for costs no round trip (a coordinator verb by anyone but the holder is
	// coordinatorsAlone's refusal before this)
	if class != classCoordinator && (c.actor == "" || !hookArmed(c.actor)) {
		return "", nil
	}
	seat, err := st.B.Coordinator(ctx)
	if err != nil || seat == "" {
		return "", err
	}
	if class != classCoordinator && c.actor != seat {
		return "", nil
	}
	if !hookArmed(seat) {
		return "", nil
	}
	rec, ok, err := readHook(ctx, st)
	if err != nil {
		return "", err
	}
	return sprint.HookGate(rec, ok, seat, st.Now()), nil
}

// hookFirst runs the gate before the verb, on the verb's own words, so its refusal is
// the one line and nothing else, exit 1: "You must hook in first: run nova-sprint hook",
// or the push to acknowledge. refused is false when the gate lets it run or cannot be
// read here (no store named, a store that does not answer, a verb's help or a flag it
// refuses): the verb then runs, and its own store open passes the same gate
// (coordinatorOnly).
func (a *app) hookFirst(args []string, stderr io.Writer) (int, bool) {
	v := readVerb(args)
	if v.words == 0 || v.help || v.err != nil || hookExempt[v.name] {
		return 0, false
	}
	switch verbClasses[v.name] {
	case classCoordinator, classSeat, classRead:
	default:
		return 0, false
	}
	c := common{verb: v.name, redis: firstEnv(a.getenv, "NOVA_SPRINT_REDIS", "NOVA_REDIS_ADDR", seatLoginAddr), actor: a.getenv("NOVA_SPRINT_ACTOR")}
	if f := v.fs.Lookup("redis"); f != nil && v.given("redis") {
		c.redis = f.Value.String()
	}
	if f := v.fs.Lookup("actor"); f != nil && v.given("actor") {
		c.actor = f.Value.String()
	}
	// the gate refuses only the seat's holder (a coordinator verb by anyone else is
	// coordinatorsAlone's refusal), so an actor the gate is not armed for passes here
	if c.actor == "" || !hookArmed(c.actor) {
		return 0, false
	}
	if strings.TrimSpace(c.redis) == "" {
		return 0, false
	}
	st, err := a.storeCtx(context.Background(), common{redis: c.redis})
	if err != nil {
		return 0, false
	}
	why, err := hookedIn(context.Background(), st, c)
	if err != nil || why == "" {
		return 0, false
	}
	fmt.Fprintln(stderr, why)
	return 1, true
}

// hookHub is the server's hold of the seat's hook: the one subscription open, nil for
// none. A new hook replaces the one open.
type hookHub struct {
	mu  sync.Mutex
	cur *hookSession
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

	mu      sync.Mutex
	chal    string
	chalAt  time.Time
	proven  time.Time
	seq     uint64
	open    []sprint.HookItem // delivered, not acknowledged, oldest first
	sent    map[string]bool
	ended   bool
	done    chan struct{}
	endOnce sync.Once
}

func (a *app) hookHub() *hookHub {
	a.lanesMu.Lock()
	defer a.lanesMu.Unlock()
	if a.hooks == nil {
		a.hooks = &hookHub{}
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
	var holder string
	if err := a.onLine(r.Context(), func(st *store.Store) error {
		var err error
		holder, err = st.B.Coordinator(r.Context())
		return err
	}); err != nil {
		http.Error(w, "the store did not answer: "+oneline.Err(err), http.StatusServiceUnavailable)
		return
	}
	if holder != name {
		http.Error(w, fmt.Sprintf("the hook is the seat's holder's: %s, not %s; nothing was changed", orDashStr(holder, "nobody"), name), http.StatusForbidden)
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
	s := &hookSession{a: a, id: hookNonce(), name: name, conn: conn, sent: map[string]bool{}, done: make(chan struct{})}
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
	h := s.a.hookHub()
	h.mu.Lock()
	old := h.cur
	h.cur = s
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
	if err := s.send(hookMsg{T: "hello", Name: s.name, Session: s.id}); err != nil {
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
	return h.cur == s
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
// this subscription is delivered, each with its id (Deliver).
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
	}
	var pushes []hookMsg
	err := s.a.onLine(ctx, func(st *store.Store) error {
		v, err := st.Inbox(ctx, defaultDeadline, defaultStale, 10000)
		if err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, g := range v.Groups {
			if !forCoordinator(g) {
				continue
			}
			keys := noteKeys(g)
			fresh := false
			for _, k := range keys {
				fresh = fresh || !s.sent[k]
				s.sent[k] = true
			}
			if !fresh {
				continue
			}
			s.seq++
			s.open = append(s.open, sprint.HookItem{ID: s.seq, What: g.ID, At: now})
			pushes = append(pushes, hookMsg{T: "push", ID: s.seq, What: g.ID, Text: groupText(g, now, false)})
		}
		return nil
	})
	if err != nil {
		s.a.serveSay("SEAT HOOK name=%s: the inbox could not be read: %s", s.name, oneline.Escape(err.Error()))
		return
	}
	if len(pushes) == 0 {
		return
	}
	if _, err := s.record(ctx); err != nil {
		s.a.serveSay("SEAT HOOK name=%s: the pushes could not be recorded: %s", s.name, oneline.Escape(err.Error()))
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
		mine := h.cur == s
		if mine {
			h.cur = nil
		}
		h.mu.Unlock()
		if !mine {
			return
		}
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
	addr := a.getenv(ServerEnv)
	if addr == "" {
		return refuse(stderr, name, "the hook is a subscription the sprint's server holds: set "+ServerEnv+"=<host:port> (run --listen prints it); run: "+ServerEnv+"=127.0.0.1:<port> nova-sprint hook")
	}
	ctx, stop := a.notify(context.Background())
	defer stop()
	return a.hookHold(ctx, addr, c.actor, stdout, stderr)
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

// hookHold is the seat's subscription, held until it ends: the connection upgraded, the
// socket for the session's lines opened, and every line from the server said on stdout,
// one event per line for the Monitor. Exit 0 after unhook; 1 for any other end, which
// the server records as gone.
func (a *app) hookHold(ctx context.Context, addr, name string, stdout, stderr io.Writer) int {
	const verb = "hook"
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
					r, err = relay.ask(hookMsg{T: m.T, Challenge: m.Challenge, ID: m.ID})
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
	unhooked := false
	sc := bufio.NewScanner(br)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var m hookMsg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		if m.Req != "" && relay.answered(m) {
			if m.T == "ok" && m.Of == "prove" {
				fmt.Fprintf(stdout, "HOOK PROVEN name=%s live-until=%s: every coordinator command runs while the hook stays answered\n", oneline.Field(name), m.Live.UTC().Format(time.RFC3339))
			}
			if m.T == "ok" && m.Of == "unhook" {
				unhooked = true
			}
			continue
		}
		hookSay(stdout, name, m)
	}
	if unhooked {
		fmt.Fprintf(stdout, "HOOK ENDED name=%s: unhooked; the seat is recorded away\n", oneline.Field(name))
		return 0
	}
	fmt.Fprintf(stdout, "HOOK DROPPED name=%s: the connection to the server ended; the seat is recorded gone and every coordinator command is refused; run: nova-sprint hook\n", oneline.Field(name))
	return 1
}

// hookSay is one line from the server as the Monitor shows it: one event a line, a
// push's text indented under its line.
func hookSay(w io.Writer, name string, m hookMsg) {
	switch m.T {
	case "hello":
		fmt.Fprintf(w, "HOOK CONNECTED name=%s session=%s: not hooked until the challenge below is answered\n", oneline.Field(m.Name), oneline.Field(m.Session))
	case "challenge":
		fmt.Fprintf(w, "HOOK CHALLENGE %s: answer now, before anything else: nova-sprint hook --prove %s --actor %s\n", m.Challenge, m.Challenge, oneline.Field(name))
	case "push":
		var b strings.Builder
		fmt.Fprintf(&b, "PUSH %d %s: read it, then acknowledge: nova-sprint hook --ack %d --actor %s\n", m.ID, oneline.Field(m.What), m.ID, oneline.Field(name))
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
