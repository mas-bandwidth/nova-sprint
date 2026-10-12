package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

// The seat's hook, held by a process of the seat's machine (docs/SPEC-SPRINT.md, "The
// hook"; tla/SeatHookHolder.tla). Two processes hold it: the session's Monitor
// (`nova-sprint hook`, hookHold: one connection, held until it ends) and the seat's push
// loop (`inbox --wait --push seat`, the unit seat install writes: pushHook, below), which
// holds it for as long as the loop runs, across every session of the seat. Both are one
// hookHolder: the socket the session's lines come in on (prove, ack, unhook, and hook, the
// seat's word to hook in again), the bus subscription the bus half of each challenge is
// read back off, and the connection of the moment (one at a time; the loop opens the next
// one a beat after the last ended). The proof is the session's alone: a holder answers no
// challenge. A challenge the loop's connection is asked is delivered into the seat's inbox
// through the seat's push target, where the session reads its judgments, and the session
// answers it with `nova-sprint hook --prove <c> --actor <seat>` over the loop's socket.
//
// The holder's states (hookHolder.state): hookDown (no connection; the loop opens one at
// its next beat), hookOpen (a connection is open), hookAway (the seat unhooked: the loop
// holds nothing until the seat's word, across its own restarts: the record says away),
// hookMissed (a challenge went unanswered past its bound: the loop holds nothing until the
// seat's word, so an absent seat is told once, not every five minutes), hookOther (another
// process holds the seat's hook: the loop waits for its end).
const (
	hookDown   = "down"
	hookOpen   = "open"
	hookAway   = "away"
	hookMissed = "missed"
	hookOther  = "other"
)

// hookMissWhy begins the server's reason for a hook gone on an unanswered challenge
// (hookSession.look): the holder reads it off the record at its start.
const hookMissWhy = "the challenge sent at "

// hookRefused is the server's refusal of a hook connection: its line.
type hookRefused struct{ why string }

func (e *hookRefused) Error() string { return e.why }

// hookStream opens the hook's stream to the server at addr as name: the connection to the
// loopback listener upgraded to the hook's protocol (hookPath, hookUpgrade), reads through
// the reader the upgrade left. A test's app opens a pipe to its own server (hookDial). The
// server's refusal is a hookRefused; a dial that fails is its own error.
func (a *app) hookStream(ctx context.Context, addr, name string, quiet bool) (io.ReadWriteCloser, error) {
	if a.hookDial != nil {
		return a.hookDial(ctx, addr, name, quiet)
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	quietParam := ""
	if quiet {
		quietParam = "&quiet_happened=1"
	}
	fmt.Fprintf(conn, "GET %s?as=%s%s HTTP/1.1\r\nHost: %s\r\nUpgrade: %s\r\nConnection: Upgrade\r\n\r\n", hookPath, url.QueryEscape(name), quietParam, addr, hookUpgrade)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		_ = conn.Close() // ignored: the hook never began
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096)) // ignored: the status says enough
		_ = resp.Body.Close()
		_ = conn.Close() // ignored: the hook never began
		return nil, &hookRefused{strings.TrimSpace(string(body))}
	}
	return &deadlineConn{Conn: conn, r: br}, nil
}

// hookSink is where one connection's events go: the Monitor's stdout (monitorSink), or
// the push loop's log and the seat's inbox (pushHook).
type hookSink interface {
	// connected: the server's hello, the shared record armed.
	connected(m hookMsg, role string)
	// challenge: the whole challenge, said once its bus half came back off the bus.
	challenge(sprintC, busC string)
	// proven: the server took the answer; the hook is live until live.
	proven(live time.Time)
	// push: a push delivered over the connection, with its id.
	push(m hookMsg)
	// line: every other line of the server (bye, refused).
	line(m hookMsg)
	// busDown: the bus subscription or the shared record failed; the connection is closed.
	busDown(what string)
}

// hookPush is a push delivered over the holder's connection and not yet acknowledged:
// its id, what it is (the group's id), and the session token the acknowledgement carries.
type hookPush struct {
	ID      uint64
	What    string
	Session string
}

// hookHolder holds the seat's hook for a process: the bus push's record and subscription,
// the socket the session's lines arrive on, and the connection of the moment.
type hookHolder struct {
	a      *app
	name   string // the seat's holder the hook is for
	addr   string // the server's hook endpoint
	from   string // where the server was named (env, seat-record)
	quiet  bool
	loop   bool // the push loop's holder: hook on the socket is the seat's word; the Monitor's refuses it
	hk     sprint.Hooker
	w      bus.Waiter
	stream string
	cursor string

	mu      sync.Mutex
	relay   *hookRelay // the open connection's; nil while none
	hs      *hookHoldState
	role    string
	state   string
	since   time.Time
	pending []hookPush // delivered over the connection and not acknowledged, oldest first
	word    chan struct{}
	ended   chan struct{} // closed when the open connection ends
}

// newHookHolder opens what every connection shares: the bus store (the shared record's
// and the subscription's), and the subscription's cursor at the tail of name's stream.
// hookHolders is every holder this process made, by name: a test reaches the push
// loop's through it (the session's lines, which the binary carries over the socket).
var hookHolders sync.Map // name -> *hookHolder

func (a *app) newHookHolder(ctx context.Context, name, addr, from string, quiet, loop bool) (*hookHolder, error) {
	hk, err := a.hooker(ctx)
	if err != nil {
		return nil, fmt.Errorf("the bus push cannot be armed: %s; set %s (and %s for its login) to the bus store, then run: %s; nothing was armed", oneline.Escape(err.Error()), busRedisEnv, busUserEnv, hookMonitor(name))
	}
	w, err := a.hk.bus.Waiter()
	if err != nil {
		return nil, fmt.Errorf("the bus push cannot be armed: %s; nothing was armed", oneline.Escape(err.Error()))
	}
	stream := bus.StreamOf(name)
	cursor, exists, err := w.Tail(ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("the bus subscription could not start: %s; nothing was armed", oneline.Escape(err.Error()))
	}
	if !exists || cursor == "" {
		cursor = "0-0"
	}
	h := &hookHolder{a: a, name: name, addr: addr, from: from, quiet: quiet, loop: loop, hk: hk, w: w, stream: stream, cursor: cursor,
		hs: &hookHoldState{}, state: hookDown, since: a.now(), word: make(chan struct{}, 1)}
	hookHolders.Store(name, h)
	return h, nil
}

// listen opens the machine's socket for name's session lines: refused when a hook is
// already running on this machine for name (one holder a machine). The listener is
// closed, and the socket removed, by the caller's stop.
func (h *hookHolder) listen() (net.Listener, string, error) {
	if h.a.hookListen != nil {
		return h.a.hookListen(h.name)
	}
	sock, err := h.a.hookSocket(h.name)
	if err != nil {
		return nil, "", err
	}
	if c, err := net.DialTimeout("unix", sock, time.Second); err == nil {
		_ = c.Close()
		return nil, sock, errors.New("a hook is already running on this machine for " + h.name + " (" + sock + "): one Monitor holds the seat; stop it, or answer through it")
	}
	_ = os.Remove(sock) // ignored: a socket left by a hook that died answers nothing
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, sock, err
	}
	return ln, sock, nil
}

// serve answers the session's lines on the socket until the listener closes: each one
// read, taken (take), and the answer written back.
func (h *hookHolder) serve(ctx context.Context, ln net.Listener) {
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
				r, err = h.take(ctx, m)
			}
			if err != nil {
				r = hookMsg{T: "refused", Why: err.Error()}
			}
			b, _ := json.Marshal(r) // ignored: a message of strings and numbers always encodes
			_, _ = c.Write(append(b, '\n'))
		}(c)
	}
}

// take is one of the session's lines: hook is the seat's word to the loop's holder (hook
// in again now, whatever ended the last connection); every other line is carried to the
// server over the open connection (hookRelayed), or refused while there is none.
func (h *hookHolder) take(ctx context.Context, m hookMsg) (hookMsg, error) {
	if m.T == "hook" {
		if !h.loop {
			return hookMsg{T: "refused", Why: "a hook is already running on this machine for " + h.name + ": one Monitor holds the seat; stop it, or answer through it"}, nil
		}
		h.mu.Lock()
		state := h.state
		h.mu.Unlock()
		if state != hookOpen {
			select {
			case h.word <- struct{}{}:
			default:
			}
		}
		return hookMsg{T: "ok", Of: "hook", Name: h.name, Why: state}, nil
	}
	h.mu.Lock()
	relay, state, since := h.relay, h.state, h.since
	h.mu.Unlock()
	if relay == nil {
		return hookMsg{T: "refused", Why: fmt.Sprintf("the seat's hook is %s since %s, with no connection to the server to carry this on; the push loop opens one at its next beat, or at once with: nova-sprint hook --actor %s", state, since.UTC().Format(time.RFC3339), h.name)}, nil
	}
	return h.a.hookRelayed(ctx, h.hk, h.hs, relay, h.name, m)
}

// setState records the holder's state and when it changed.
func (h *hookHolder) setState(s string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state != s {
		h.state, h.since = s, h.a.now()
	}
}

// State is the holder's state and since when.
func (h *hookHolder) State() (string, time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state, h.since
}

// hookEnd is how a connection ended: unhooked (the seat's word), missed (the challenge
// unanswered past its bound, the server's bye), replaced (another hook took the seat),
// or dropped (anything else), with the server's line when it said one.
type hookEnd struct {
	unhooked, missed, replaced bool
	why                        string
}

// hold is one connection, from its dial to its end: the bus subscription read beside
// it, the keepalive every sprint.HookKeepalive, each server line taken (the hello arms
// the shared record; a challenge's bus half goes out and the line is said once it comes
// back; a proof is said), and the session's lines carried over it (take). It returns how
// the connection ended; a dial or refusal is the error.
func (h *hookHolder) hold(ctx context.Context, sink hookSink) (hookEnd, error) {
	conn, err := h.a.hookStream(ctx, h.addr, h.name, h.quiet)
	if err != nil {
		return hookEnd{}, err
	}
	defer conn.Close()
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	relay := &hookRelay{w: conn, pending: map[string]chan hookMsg{}}
	h.mu.Lock()
	h.relay, h.ended = relay, make(chan struct{})
	ended := h.ended
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.relay = nil
		h.mu.Unlock()
		close(ended)
	}()
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
		<-ctx.Done()
		_ = conn.Close() // an interrupt ends the hook: the server records it gone
	}()
	// the bus push: every message to name, said as it arrives; the hook's own bus
	// challenge completes the HOOK CHALLENGE line
	go h.readBus(ctx, conn, sink)

	end := hookEnd{}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var m hookMsg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		if m.Req != "" && relay.answered(m) {
			if m.T == "ok" && m.Of == "prove" {
				sink.proven(m.Live)
			}
			if m.T == "ok" && m.Of == "unhook" {
				end.unhooked = true
			}
			continue
		}
		switch m.T {
		case "hello":
			role := cmp.Or(m.Role, sprint.HookRoleCoordinator)
			h.mu.Lock()
			h.role = role
			h.mu.Unlock()
			if err := h.hk.Arm(ctx, h.name, h.a.now()); err != nil {
				sink.busDown("the shared record could not be armed (" + oneline.Escape(err.Error()) + ")")
				_ = conn.Close()
				continue
			}
			sink.connected(m, role)
			continue
		case "challenge":
			// the server's half; the bus half goes out on the bus and the line is said once
			// it comes back off the subscription
			busC := hookNonce()
			h.hs.challenge(m.Challenge, busC)
			msg := bus.Message{ID: "hook-" + busC, From: h.name, To: []string{h.name}, Kind: bus.KindStatus, Subject: hookBusSubject + busC, At: h.a.now(),
				Body: "nova-sprint hook's bus challenge for " + h.name + ": the hook reads it back off the bus, and the answer to HOOK CHALLENGE proves the bus push; nothing else to do"}
			if err := h.a.hk.bus.Store.AddAll(ctx, []string{h.stream}, msg.Fields()); err != nil {
				sink.busDown("the bus challenge could not be sent (" + oneline.Escape(err.Error()) + ")")
				_ = conn.Close()
			}
			continue
		case "push":
			if m.What != hookWarnWhat {
				h.mu.Lock()
				h.pending = append(h.pending, hookPush{ID: m.ID, What: m.What, Session: m.Session})
				h.mu.Unlock()
			}
			sink.push(m)
			continue
		case "bye":
			end.why = m.Why
			end.missed = strings.Contains(m.Why, "was not answered in time")
			end.replaced = strings.HasPrefix(m.Why, "replaced by")
		}
		sink.line(m)
	}
	if end.unhooked {
		h.hs.unhookWg.Wait()
	}
	h.mu.Lock()
	h.pending = nil
	h.mu.Unlock()
	return end, nil
}

// readBus reads the holder's own bus stream while the connection is open: each message
// said through the sink, and the bus half of a challenge completing its line. Three
// failed reads in a row end the connection (the gate wants both pushes: a hook with no
// bus subscription is no hook).
func (h *hookHolder) readBus(ctx context.Context, conn io.Closer, sink hookSink) {
	fails := 0
	for ctx.Err() == nil {
		es, err := h.w.BlockRead(ctx, h.stream, h.cursor, hookBusBlock, 64)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if fails++; fails >= 3 {
				sink.busDown("the bus subscription failed (" + oneline.Escape(err.Error()) + ")")
				_ = conn.Close()
				return
			}
			h.a.sleepFor(time.Second)
			continue
		}
		fails = 0
		for _, e := range es {
			h.cursor = e.Entry
			m := e.Message()
			if c, ok := strings.CutPrefix(m.Subject, hookBusSubject); ok && m.From == h.name {
				if sc, ok := h.hs.busRead(c); ok {
					sink.challenge(sc, c)
				}
				continue
			}
			sink.line(hookMsg{T: "bus", ID: 0, What: e.Entry, Name: m.From, Role: m.KindName(), Text: m.Body, Why: m.Subject})
		}
	}
}

// sleepFor sleeps on this process's clock: a test's steps its clock, never a wall.
func (a *app) sleepFor(d time.Duration) {
	if a.sleep != nil {
		a.sleep(d)
		return
	}
	time.Sleep(d)
}

// delivered marks the pushes delivered into the seat's inbox: every pending push whose
// group is written there (or gone from the inbox since) is acknowledged over the
// connection, cumulatively up to the last one in a row that is, so a push whose group
// is not yet written holds every later acknowledgement back. It says what was
// acknowledged, "" for nothing.
func (h *hookHolder) delivered(ctx context.Context, written func(what string) bool) string {
	h.mu.Lock()
	relay, pending := h.relay, append([]hookPush(nil), h.pending...)
	h.mu.Unlock()
	if relay == nil || len(pending) == 0 {
		return ""
	}
	var last *hookPush
	for i := range pending {
		if !written(pending[i].What) {
			break
		}
		last = &pending[i]
	}
	if last == nil {
		return ""
	}
	r, err := relay.ask(hookMsg{T: "ack", ID: last.ID, Session: last.Session})
	if err != nil || r.T != "ok" {
		why := "the server refused it"
		if err != nil {
			why = err.Error()
		} else if r.Why != "" {
			why = r.Why
		}
		return fmt.Sprintf("HOOK ACK FAILED through=%d: %s", last.ID, oneline.Escape(why))
	}
	h.mu.Lock()
	kept := h.pending[:0]
	for _, p := range h.pending {
		if p.ID > last.ID {
			kept = append(kept, p)
		}
	}
	h.pending = kept
	h.mu.Unlock()
	return fmt.Sprintf("HOOK ACK OK name=%s through=%d unacked=%d: delivered into the inbox", oneline.Field(h.name), last.ID, r.Unacked)
}

// Pending is the pushes delivered over the connection and not acknowledged.
func (h *hookHolder) Pending() []hookPush {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hookPush(nil), h.pending...)
}

// monitorSink is the Monitor's stdout: one event a line, as hookSay (or compactSay)
// prints them.
type monitorSink struct {
	out  *hookOut
	name string
	addr string
	from string
	say  func(w io.Writer, name, addr, from string, m hookMsg)
}

func (s *monitorSink) connected(m hookMsg, role string) {
	s.out.printf("HOOK CONNECTED name=%s role=%s session=%s server=%s from=%s bus=%s: armed (the %s push and the %s push); not hooked until the challenge below is answered\n",
		oneline.Field(m.Name), role, oneline.Field(m.Session), oneline.Field(s.addr), oneline.Field(s.from), oneline.Field(bus.StreamOf(s.name)), sprint.HookCoordinatorPushes[0], sprint.HookCoordinatorPushes[1])
}

func (s *monitorSink) challenge(sc, c string) {
	s.out.printf("HOOK CHALLENGE %s.%s: answer now, before anything else (one answer proves both pushes): nova-sprint hook --prove %s.%s --actor %s\n", sc, c, sc, c, oneline.Field(s.name))
}

func (s *monitorSink) proven(live time.Time) {
	s.out.printf("HOOK PROVEN name=%s live-until=%s: both pushes are live; every verb runs while the hook stays answered\n", oneline.Field(s.name), live.UTC().Format(time.RFC3339))
}

func (s *monitorSink) push(m hookMsg) { s.say(s.out, s.name, s.addr, s.from, m) }

func (s *monitorSink) line(m hookMsg) {
	if m.T == "bus" {
		s.out.printf("BUS %s from=%s kind=%s subject=%s\n", oneline.Field(m.What), oneline.Field(m.Name), m.Role, oneline.Quote(m.Why))
		body := m.Text
		if len(body) > 2000 {
			body = body[:2000] + " ..."
		}
		for _, l := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
			if l != "" {
				s.out.printf("  %s\n", l)
			}
		}
		return
	}
	s.say(s.out, s.name, s.addr, s.from, m)
}

func (s *monitorSink) busDown(what string) {
	s.out.printf("HOOK BUS DOWN name=%s: %s; the hook ends; run: %s\n", oneline.Field(s.name), what, hookMonitor(s.name))
}
