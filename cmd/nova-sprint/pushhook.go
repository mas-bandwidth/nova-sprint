package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

// The seat's push loop holds the seat's hook (docs/SPEC-SPRINT.md, "The hook";
// tla/SeatHookHolder.tla; the seat, 2026-10-12: the Studio's standing loops, friend sync
// among them, were refused "You must hook in first" for every pass of the hours no
// session held `nova-sprint hook` under a Monitor, and ran again only while a session
// held one by hand). The loop is the coordinator's own process: `inbox --wait --push seat`,
// the unit seat install writes, running for as long as the machine does. It delivers every
// judgment into the seat's inbox already; now it holds the connection the hook is, across
// every session of the seat, so a proof stands until the connection ends (a server
// restart, a drop), not until the session that proved it ends.
//
// What the loop does and does not do:
//   - It opens the hook's connection as the seat's holder of the moment (--push seat
//     follows the seat), at its start and again one beat (waitLook) after each end but
//     two: an unhook (the seat's word, which stands across the loop's own restarts: the
//     record says away) and a challenge unanswered past its bound (the loop would be
//     asked again every five minutes and the owner told each time: it holds nothing until
//     the seat's word, `nova-sprint hook --actor <seat>`, which hooks it in again at once).
//     Another process's live hook (a Monitor's) is left alone: the loop waits for its end.
//   - It answers no challenge. The proof is the session's alone (the owner, 2026-10-10:
//     "you must PROVE YOU HAVE HOOKED IN"): the challenge is delivered into the seat's
//     inbox through the seat's push target, where the session reads its judgments, and the
//     session answers it over the loop's socket, `nova-sprint hook --prove <c> --actor
//     <seat>`, exactly as it answers a Monitor's.
//   - It acknowledges a push over the connection once the judgment the push is of is
//     written into the seat's inbox (the loop's own delivery, inboxwait.go push), never
//     before: the acknowledgement says the push reached where the seat reads, no more; the
//     judgment stays open until it is answered, and the waiting-judgment warning repeats.
//     A push whose judgment cannot be written is never acknowledged, and past
//     sprint.HookAckBound blocks every coordinator verb, naming it: a loop that cannot
//     deliver is found.
//   - Without a bus store named (NOVA_BUS_REDIS) it holds nothing and says so once: a hook
//     wants both pushes.

// pushHookEvery is how long the loop waits after a connection ends before it opens the
// next one: the loop's beat (waitLook).
const pushHookEvery = waitLook

// pushHook is the push loop's hold of the seat's hook: the holder for the seat's holder
// of the moment, and what it needs from the loop (where the inbox is, the latest look).
type pushHook struct {
	a      *app
	src    inboxSource
	p      *pushTarget
	out    io.Writer
	asJSON bool

	mu     sync.Mutex
	holder string
	h      *hookHolder
	ln     net.Listener
	sock   string
	stop   context.CancelFunc
	done   chan struct{}
	look   inboxLook
	said   string // the last line said of a failure to open, once until it changes
	wanted bool   // the seat's word arrived: hook in whatever the record says
}

// pushHookStart is the loop's hold of the hook, for holder: nil, after one NOTE, when
// no bus store is named (the hook wants both pushes, and the bus push's record lives
// there).
func (a *app) pushHookStart(ctx context.Context, src inboxSource, p *pushTarget, holder string, asJSON bool, stdout io.Writer) *pushHook {
	if a.getenv(busRedisEnv) == "" {
		fmt.Fprintf(stdout, "NOTE the seat's hook is not held by this loop: %s is not set (the bus store, where the bus push is recorded); install the loop with it: nova-sprint seat install --bus <host:port> ...; judgments are pushed all the same\n", busRedisEnv)
		return nil
	}
	hh := &pushHook{a: a, src: src, p: p, out: stdout, asJSON: asJSON}
	hh.follow(ctx, holder)
	return hh
}

// say writes one line of the hold on the loop's log (one JSON object under --json).
func (hh *pushHook) say(line string) {
	if hh.asJSON {
		b, _ := json.Marshal(map[string]any{"hook": line, "at": hh.a.now()}) // ignored: a string and a time always encode
		fmt.Fprintln(hh.out, string(b))
		return
	}
	fmt.Fprintln(hh.out, line)
}

// follow holds the hook for holder: the holder of the moment is kept; a seat that moved
// ends the hold for the old holder and begins one for the new.
func (hh *pushHook) follow(ctx context.Context, holder string) {
	hh.mu.Lock()
	same := holder == hh.holder
	hh.mu.Unlock()
	if same {
		return
	}
	hh.end()
	if holder == "" {
		return
	}
	addr, from := hh.a.namedServer()
	if addr == "" {
		hh.say("HOOK NOT HELD name=" + oneline.Field(holder) + ": " + hh.a.noServerHint())
		return
	}
	h, err := hh.a.newHookHolder(ctx, holder, addr, from, true, true)
	if err != nil {
		hh.say("HOOK NOT HELD name=" + oneline.Field(holder) + ": " + oneline.Escape(err.Error()))
		return
	}
	ln, sock, err := h.listen()
	if err != nil {
		hh.say("HOOK NOT HELD name=" + oneline.Field(holder) + ": " + oneline.Escape(err.Error()) + "; the loop pushes judgments and holds no hook")
		return
	}
	hctx, stop := context.WithCancel(ctx)
	hh.mu.Lock()
	hh.holder, hh.h, hh.ln, hh.sock, hh.stop, hh.done, hh.said, hh.wanted = holder, h, ln, sock, stop, make(chan struct{}), "", false
	done := hh.done
	hh.mu.Unlock()
	go h.serve(hctx, ln)
	go func() {
		defer close(done)
		hh.run(hctx, h)
	}()
}

// end ends the hold: the connection closed (the server records it gone), the socket
// closed and removed.
func (hh *pushHook) end() {
	hh.mu.Lock()
	stop, ln, sock, done := hh.stop, hh.ln, hh.sock, hh.done
	hh.holder, hh.h, hh.ln, hh.sock, hh.stop, hh.done = "", nil, nil, "", nil, nil
	hh.mu.Unlock()
	if stop != nil {
		stop()
	}
	if ln != nil {
		_ = ln.Close()      // ignored: the listener is this process's
		_ = os.Remove(sock) // ignored: a socket left behind answers nothing
	}
	if done != nil {
		<-done
	}
}

// run holds the hook for h until ctx ends: a connection, then a beat, then the next,
// as the states say (hookholder.go).
func (hh *pushHook) run(ctx context.Context, h *hookHolder) {
	for ctx.Err() == nil {
		hh.mu.Lock()
		wanted := hh.wanted
		hh.wanted = false
		hh.mu.Unlock()
		if !wanted {
			// the record first: an unhook stands across the loop's restarts, a missed
			// challenge too, and another holder's live hook is left to its holder
			if state, why := hh.recordState(ctx, h.name); state != hookDown {
				if s, _ := h.State(); s != state {
					h.setState(state)
					hh.say(hh.stateLine(h, state, why))
				}
				if !hh.wait(ctx) {
					return
				}
				continue
			}
		}
		end, err := h.hold(ctx, hh)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			h.setState(hookDown)
			line := "HOOK NOT OPENED name=" + oneline.Field(h.name) + " server=" + oneline.Field(h.addr) + ": " + oneline.Escape(err.Error()) + "; said once until it changes; the loop tries again every " + pushHookEvery.String()
			hh.mu.Lock()
			changed := line != hh.said
			hh.said = line
			hh.mu.Unlock()
			if changed {
				hh.say(line)
			}
			if !hh.wait(ctx) {
				return
			}
			continue
		}
		hh.mu.Lock()
		hh.said = ""
		hh.mu.Unlock()
		switch {
		case end.unhooked:
			h.setState(hookAway)
			hh.say(hh.stateLine(h, hookAway, "nova-sprint unhook"))
		case end.missed:
			h.setState(hookMissed)
			hh.say(hh.stateLine(h, hookMissed, end.why))
			hh.deliver(ctx, h.name, "HOOK MISSED name="+h.name+": "+end.why+"; the push loop holds no hook until your word: nova-sprint hook --actor "+h.name+"\n")
		case end.replaced:
			h.setState(hookOther)
			hh.say(hh.stateLine(h, hookOther, end.why))
		default:
			h.setState(hookDown)
			hh.say(hh.stateLine(h, hookDown, cmpOr(end.why, "the connection to the server ended")))
		}
		if !hh.wait(ctx) {
			return
		}
	}
}

// stateLine is the line said when the hold's state changes.
func (hh *pushHook) stateLine(h *hookHolder, state, why string) string {
	_, since := h.State()
	at := since.UTC().Format(time.RFC3339)
	switch state {
	case hookAway:
		return fmt.Sprintf("HOOK AWAY name=%s since=%s: the seat unhooked (%s); the push loop holds no hook, across its own restarts, until the seat's word: nova-sprint hook --actor %s", oneline.Field(h.name), at, oneline.Escape(why), oneline.Field(h.name))
	case hookMissed:
		return fmt.Sprintf("HOOK MISSED name=%s since=%s: %s; the push loop holds no hook until the seat's word, so the owner is told once, not every %s: nova-sprint hook --actor %s", oneline.Field(h.name), at, oneline.Escape(why), sprint.HookAnswerBound, oneline.Field(h.name))
	case hookOther:
		return fmt.Sprintf("HOOK HELD ELSEWHERE name=%s since=%s: another process holds the seat's hook (%s); the push loop waits for its end, looking every %s", oneline.Field(h.name), at, oneline.Escape(why), pushHookEvery)
	}
	return fmt.Sprintf("HOOK DROPPED name=%s since=%s: %s; every gated verb is refused; the push loop opens a new connection in %s", oneline.Field(h.name), at, oneline.Escape(why), pushHookEvery)
}

// recordState is the hold's state as the server's record says it at the loop's start and
// before each connection: away (an unhook stands), missed (gone on an unanswered
// challenge), other (hooked: a live hook that is not this loop's), else down (none, gone
// otherwise: open one). A record that cannot be read is down: the server says the rest.
func (hh *pushHook) recordState(ctx context.Context, name string) (string, string) {
	state, why, err := hh.src.hookState(ctx, name)
	if err != nil {
		return hookDown, ""
	}
	switch state {
	case sprint.HookAway:
		return hookAway, cmpOr(why, "the record says away")
	case sprint.HookHooked, "unproven":
		return hookOther, "the record says hooked"
	case sprint.HookGone:
		if strings.HasPrefix(why, hookMissWhy) {
			return hookMissed, why
		}
	}
	return hookDown, why
}

// wait waits one beat, or for the seat's word (hook in now), false when ctx ended.
func (hh *pushHook) wait(ctx context.Context) bool {
	after := hh.a.after
	if after == nil {
		after = time.After
	}
	hh.mu.Lock()
	h := hh.h
	hh.mu.Unlock()
	var word chan struct{}
	if h != nil {
		word = h.word
	}
	select {
	case <-ctx.Done():
		return false
	case <-word:
		hh.mu.Lock()
		hh.wanted = true
		hh.mu.Unlock()
		return true
	case <-after(pushHookEvery):
		return true
	}
}

// looked keeps the latest look, so the pushes' judgments can be found in the inbox.
func (hh *pushHook) looked(l inboxLook) {
	hh.mu.Lock()
	hh.look = l
	hh.mu.Unlock()
}

// delivered acknowledges, over the connection, every push whose judgment the loop has
// written into the seat's inbox (hookHolder.delivered), after a delivery pass.
func (hh *pushHook) delivered(ctx context.Context, l inboxLook) {
	hh.looked(l)
	hh.mu.Lock()
	h, p := hh.h, hh.p
	hh.mu.Unlock()
	if h == nil {
		return
	}
	written := func(what string) bool {
		g, ok := sprint.FindGroup(l.groups, what)
		if !ok {
			return true // closed since: nothing to write
		}
		dir := p.dirOf(l.holder, g)
		if dir == "" {
			return false
		}
		for _, k := range noteKeys(g) {
			if _, err := os.Stat(filepath.Join(dir, k+".md")); err != nil {
				return false
			}
		}
		return true
	}
	if line := h.delivered(ctx, written); line != "" {
		hh.say(line)
	}
}

// deliver puts text into the seat's inbox, where the session reads its judgments: one
// file in the holder's inbox directory (never written twice), then through the seat's
// push target when its adapter is not that folder (pushJudgments).
func (hh *pushHook) deliver(ctx context.Context, holder, text string) string {
	dir, _, ok := hh.a.seatInbox(holder)
	if !ok {
		return "no inbox"
	}
	if _, err := hh.p.keys(dir); err != nil {
		return "the inbox cannot be read: " + err.Error()
	}
	path := filepath.Join(dir, "hook-"+hh.a.now().UTC().Format("20060102T150405Z")+"-"+pushNonce()[:8]+".md")
	if _, err := writeOnce(path, text); err != nil {
		return "the inbox cannot be written: " + err.Error()
	}
	hh.a.pushJudgments(ctx, hh.src, holder, []string{text}, hh.asJSON, hh.out)
	return path
}

// The hook's events, on the loop's log and into the seat's inbox (hookSink).

func (hh *pushHook) connected(m hookMsg, role string) {
	hh.mu.Lock()
	h := hh.h
	hh.mu.Unlock()
	if h != nil {
		h.setState(hookOpen)
	}
	hh.say(fmt.Sprintf("HOOK CONNECTED name=%s role=%s session=%s holder=push-loop: the push loop holds the seat's hook across its sessions; not hooked until the challenge, delivered into the inbox, is answered", oneline.Field(m.Name), role, oneline.Field(m.Session)))
}

func (hh *pushHook) challenge(sc, c string) {
	hh.mu.Lock()
	holder := hh.holder
	hh.mu.Unlock()
	answer := "nova-sprint hook --prove " + sc + "." + c + " --actor " + holder
	text := "HOOK CHALLENGE " + sc + "." + c + ": the seat's push loop holds your hook, unproven until you answer, and every coordinator verb is refused until then; answer now, before anything else (one answer proves both pushes): " + answer + "\n"
	where := hh.deliver(context.Background(), holder, text)
	hh.say(fmt.Sprintf("HOOK CHALLENGE %s.%s delivered=%s: answer now, before anything else (one answer proves both pushes): %s", sc, c, oneline.Field(where), answer))
}

func (hh *pushHook) proven(live time.Time) {
	hh.mu.Lock()
	holder := hh.holder
	hh.mu.Unlock()
	hh.say(fmt.Sprintf("HOOK PROVEN name=%s live-until=%s: both pushes are live; every verb runs while the push loop keeps the connection, across the seat's sessions", oneline.Field(holder), live.UTC().Format(time.RFC3339)))
}

func (hh *pushHook) push(m hookMsg) {
	if m.What == hookWarnWhat {
		return // the waiting-judgment warning: the judgments are in the inbox already
	}
	hh.say(fmt.Sprintf("HOOK PUSH %d %s session=%s: acknowledged once its judgment is written into the inbox", m.ID, oneline.Field(m.What), oneline.Field(m.Session)))
}

func (hh *pushHook) line(m hookMsg) {
	hh.mu.Lock()
	holder := hh.holder
	hh.mu.Unlock()
	switch m.T {
	case "bye":
		hh.say(fmt.Sprintf("HOOK BYE name=%s: %s", oneline.Field(holder), oneline.Escape(m.Why)))
	case "refused":
		hh.say(fmt.Sprintf("HOOK REFUSED name=%s: %s", oneline.Field(holder), oneline.Escape(m.Why)))
	case "bus":
		hh.say(fmt.Sprintf("BUS %s from=%s kind=%s subject=%s", oneline.Field(m.What), oneline.Field(m.Name), m.Role, oneline.Quote(m.Why)))
	}
}

func (hh *pushHook) busDown(what string) {
	hh.mu.Lock()
	holder := hh.holder
	hh.mu.Unlock()
	hh.say(fmt.Sprintf("HOOK BUS DOWN name=%s: %s; the connection is closed, and the push loop opens a new one in %s", oneline.Field(holder), what, pushHookEvery))
}

// cmpOr is the first of a and b that is not empty.
func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
