package sprint

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// The coordinator hooks in (docs/SPEC-SPRINT.md, "The hook"; tla/SeatHook.tla; the
// owner, 2026-10-10: "You MUST be required to 'hook' in as coordinator." "This is not
// optional. Every command fails with 'You must hook in first'." "And you must PROVE
// YOU HAVE HOOKED IN before it will allow you to do anything."). It replaces the push
// proof: the seat answered PROOF files by hand while nothing in its session read the
// pushes, and about 1,200 went unread for hours.
//
// The sprint's server owns the hook: the seat's session runs `nova-sprint hook` under
// a Monitor, one connection to the server, which streams every judgment and push over
// it, each with an id, and a challenge. The hook counts once the session answers the
// challenge it read off that stream, over the same connection, once a connection; it
// stays live while the seat acts over it (HookActed), and a challenge unanswered for
// HookAnswerBound, or a seat silent past HookSilentBound, unhooks. The
// server keeps its word in the store (HookRecord, the key seat-hook) and every
// coordinator command reads it (HookGate).

// The hook's states, as the record says them.
const (
	HookHooked = "hooked" // a subscription is open (proven or not: HookRecord.Proven)
	HookAway   = "away"   // the seat ran nova-sprint unhook
	HookGone   = "gone"   // the subscription ended without unhook: a drop, a missed challenge, a server restart
)

// The hook stays live while its seat acts (the seat, 2026-10-11/12: the hook dropped about 15
// minutes after each proof though the seat's stream stayed connected and the seat kept acting,
// every lapse refused every coordinator verb, ended the stream and replayed ~100 pushes; the
// owner: "This must never happen again"). A challenge is asked only on a new connection, and
// answered within HookAnswerBound or the hook is gone. Once proven, every act of the seat over
// a connected stream (any coordinator verb it runs, any acknowledgement or answer, and the
// keepalive the hook client sends on its own every HookKeepalive) renews the record's Live to
// HookSilentBound after it. The keepalive proves only that the transport is alive (the hook
// client's process holds the stream), never that the seat or a person attends: an
// inattentive seat is found by its unacknowledged pushes (HookAckBound) and the judgments'
// deadlines. The hook lapses only when the stream ends, or when no act at all, keepalive
// included, reaches the server past HookSilentBound (once, with one judgment, NHookLapsed,
// naming the seat in its Who). HookAckBound is how
// long a delivered push may go unacknowledged before every coordinator command is refused
// naming it.
const (
	HookSilentBound = 15 * time.Minute
	HookKeepalive   = time.Minute
	HookAnswerBound = 5 * time.Minute
	HookAckBound    = 10 * time.Minute
)

// NHookLapsed is the one judgment of a hook that lapsed while its stream was connected: no
// act (no verb, no answer, and no keepalive, so the transport went quiet too) reached the
// server past HookSilentBound. Its Who is the seat; it is closed when that seat proves a new
// hook, never by another seat's proof.
const NHookLapsed = "the seat's hook lapsed: no act reached the server past its bound"

// HookFirst is the one line every coordinator command fails with while the seat has no
// live, proven hook (the owner's words, exactly).
const HookFirst = "You must hook in first: run nova-sprint hook"

// HookItem is a push delivered over the hook: its id on the subscription, what it is
// (the inbox group's id, or the server's own word), and when it was delivered.
type HookItem struct {
	ID   uint64    `json:"id"`
	What string    `json:"what"`
	At   time.Time `json:"at"`
	Keys []string  `json:"keys,omitempty"`
}

// HookRecord is the server's word on the seat's hook, the one the gate reads: who
// hooked, the subscription's id, its state and since when, the last challenge answered
// over it (Proven) and the time the hook counts until (Live: the outstanding
// challenge's bound), why it ended, and the pushes delivered and not acknowledged, with
// the oldest of them. Only the server writes it (cmd/nova-sprint/hook.go).
type HookRecord struct {
	Name    string    `json:"name"`
	Epoch   uint64    `json:"epoch,omitempty"`
	Session string    `json:"session,omitempty"`
	State   string    `json:"state"`
	Since   time.Time `json:"since,omitzero"`
	Proven  time.Time `json:"proven,omitzero"`
	Live    time.Time `json:"live,omitzero"`
	Why     string    `json:"why,omitempty"`
	Unacked int       `json:"unacked,omitempty"`
	Oldest  *HookItem `json:"oldest,omitempty"`
	Acked   []string  `json:"acked,omitempty"`
	PushSeq uint64    `json:"push_seq,omitempty"`
}

// HookLive says the record holds a live, proven hook for name at now: hooked, proven
// over its subscription, and within the outstanding challenge's bound (SeatHook.tla,
// Gate: rec.state = "hooked", rec.proven, ~lateC). Live is the seat's last act plus
// HookSilentBound (HookProven, HookActed). A server that died leaves its record hooked, and
// the bound lapses it.
func HookLive(rec HookRecord, ok bool, name string, now time.Time) bool {
	return ok && name != "" && rec.Name == name && rec.State == HookHooked && rec.Session != "" &&
		!rec.Proven.IsZero() && !rec.Live.IsZero() && !now.After(rec.Live)
}

// HookGate is why a coordinator command of name may not run at now, "" when it may
// (SeatHook.tla, Gate): HookFirst while the hook is not live and proven; else, while
// the oldest push delivered over it is unacknowledged past HookAckBound, the line
// naming it (AckedOrBlocking).
func HookGate(rec HookRecord, ok bool, name string, now time.Time) string {
	if !HookLive(rec, ok, name, now) {
		return HookFirst
	}
	if o := rec.Oldest; o != nil && now.Sub(o.At) > HookAckBound {
		return HookUnacked(*o, rec.Unacked)
	}
	return ""
}

// HookUnacked is the line a coordinator command fails with while a delivered push is
// unacknowledged past its bound: the push by its id and what it is, and the answer.
func HookUnacked(o HookItem, unacked int) string {
	return fmt.Sprintf("You must acknowledge push %d first (%s, delivered %s, unacknowledged past %s; %d unacknowledged): read it in the hook's stream, then run: nova-sprint hook --ack %d --session <token>",
		o.ID, o.What, o.At.UTC().Format(time.RFC3339), HookAckBound, unacked, o.ID)
}

// HookProven is the record after the session answered the outstanding challenge at
// now: proven, and live until HookSilentBound after it (SeatHook.tla, Prove).
func HookProven(rec HookRecord, now time.Time) HookRecord {
	rec.Proven = now
	rec.Live = now.Add(HookSilentBound)
	return rec
}

// HookActed is the record after its seat acted at now over its connected, proven hook: live
// until HookSilentBound after the act, never earlier than it was. A record that is not a
// proven, hooked one is returned as it is.
func HookActed(rec HookRecord, now time.Time) HookRecord {
	if rec.State != HookHooked || rec.Proven.IsZero() {
		return rec
	}
	if live := now.Add(HookSilentBound); live.After(rec.Live) {
		rec.Live = live
	}
	return rec
}

// --- the coordinator's two pushes (docs/SPEC-SPRINT.md, "The two pushes") ---
//
// The owner, 2026-10-11: "Nothing works unless hooked in, including bus, friend comms,
// sprint as friend, sprint as coordinator." "If you are not hooked in to push
// notifications, every verb except "nova-sprint hook" must fail, and tell you to hook
// in. For a coordinator, this is hooking into to sprint push notifications and message
// bus push notifications." "You can "nova-sprint unhook" to unhook from the
// notifications." And: "the error leads you with breadcrumbs to what you need to do".
//
// This is the coordinator's gate. A worker friend's (the bus push and the work push) lands
// with nova-tools' hook module and its client (github.com/mas-bandwidth/nova-tools/hook,
// docs/SPEC-HOOK.md there); this gate reads the bus push through Hooker so that module
// replaces the interim record below with no change here.

// The coordinator's role and its pushes, in the order the breadcrumb names them.
const (
	HookRoleCoordinator = "coordinator"

	HookPushSprint = "sprint" // judgments and pushes for the coordinator, over the server's hook stream
	HookPushBus    = "bus"    // messages to the coordinator's name on the message bus
)

// HookCoordinatorPushes is the pushes the coordinator wants live.
var HookCoordinatorPushes = []string{HookPushSprint, HookPushBus}

// HookSharedPrefix is the interim bus push record's key prefix in the bus store:
// sprint-hook:<identity>, a hash with the fields role, pushes (comma separated),
// armed_at, proved_at and unhooked_at (RFC 3339, UTC; empty or absent for never). It is
// outside the hook module's hook: namespace, which replaces it.
const HookSharedPrefix = "sprint-hook:"

// HookSharedKey is identity's record key.
func HookSharedKey(identity string) string { return HookSharedPrefix + identity }

// HookShared is the coordinator's bus push as its record says it: the role it was armed
// for, the pushes armed, when it was armed, when both pushes were last proven (the
// answer to one challenge that came over both), and when it was last unhooked.
type HookShared struct {
	Identity   string
	Role       string
	Pushes     []string
	ArmedAt    time.Time
	ProvedAt   time.Time
	UnhookedAt time.Time
}

// Fields is the record as the hash holds it.
func (h HookShared) Fields() map[string]string {
	at := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	return map[string]string{
		"role": h.Role, "pushes": strings.Join(h.Pushes, ","),
		"armed_at": at(h.ArmedAt), "proved_at": at(h.ProvedAt), "unhooked_at": at(h.UnhookedAt),
	}
}

// HookSharedOf is identity's record from the hash's fields; ok false when the hash holds
// no role (no record). A time that does not read is never: the safe side.
func HookSharedOf(identity string, f map[string]string) (HookShared, bool) {
	at := func(k string) time.Time {
		t, _ := time.Parse(time.RFC3339, f[k]) // ignored: a time that does not read is never (fail closed)
		return t
	}
	h := HookShared{Identity: identity, Role: f["role"], ArmedAt: at("armed_at"), ProvedAt: at("proved_at"), UnhookedAt: at("unhooked_at")}
	for _, p := range strings.Split(f["pushes"], ",") {
		if p = strings.TrimSpace(p); p != "" {
			h.Pushes = append(h.Pushes, p)
		}
	}
	return h, h.Role != ""
}

// HookSharedBound is how long a proof of the pushes holds: HookSilentBound, as the
// sprint push's (a proof is renewed by the seat's acts).
const HookSharedBound = HookSilentBound

// PushLive says push is live on the record at now: armed for the coordinator with push,
// not unhooked since, and proven since it was armed, within HookSharedBound. A record
// with any of these missing is not live (fail closed).
func (h HookShared) PushLive(push string, now time.Time) bool {
	switch {
	case h.Identity == "" || h.Role != HookRoleCoordinator || !slices.Contains(h.Pushes, push):
		return false
	case h.ArmedAt.IsZero() || h.ProvedAt.IsZero() || h.ProvedAt.Before(h.ArmedAt):
		return false
	case !h.UnhookedAt.IsZero() && !h.UnhookedAt.Before(h.ArmedAt):
		return false
	}
	return !now.After(h.ProvedAt.Add(HookSharedBound))
}

// Hooker is where the bus push's record lives (the bus store today; the hook module once
// it is tagged): read one, arm one, record both pushes proven, and record the unhook.
type Hooker interface {
	Shared(ctx context.Context, identity string) (HookShared, bool, error)
	Arm(ctx context.Context, identity string, at time.Time) error
	Prove(ctx context.Context, identity string, at time.Time) error
	Unhook(ctx context.Context, identity string, at time.Time) error
}

// HookCall is what the gate knows of a call by the seat's holder: its name, the server's
// word on the sprint push (Seat, SeatOK), the bus push's record (Shared), and, when
// something could not be read, why (Unread): the gate then refuses with it.
type HookCall struct {
	Identity string
	Seat     HookRecord
	SeatOK   bool
	Shared   HookShared
	Unread   string
}

// HookGateCall is why the holder's call may not run at now, "" when it may: the first
// push that is not live, named in the breadcrumb (HookBreadcrumb); then a push delivered
// and unacknowledged past its bound (HookGate). A call it cannot judge (Unread) is
// refused: fail closed.
func HookGateCall(c HookCall, now time.Time) string {
	if c.Unread != "" {
		return HookBreadcrumb(c.Identity, "", c.Unread)
	}
	if !HookLive(c.Seat, c.SeatOK, c.Identity, now) {
		return HookBreadcrumb(c.Identity, HookPushSprint, "")
	}
	if !c.Shared.PushLive(HookPushBus, now) {
		return HookBreadcrumb(c.Identity, HookPushBus, "")
	}
	return HookGate(c.Seat, c.SeatOK, c.Identity, now)
}

// HookBreadcrumb is the gate's one line: HookFirst, then what is not live (the push, or
// why the hook could not be checked), and the exact commands that hook the coordinator
// in, answer the challenge, and stop. The owner: "the error leads you with breadcrumbs to
// what you need to do -- which is to HOOK IN (here is exactly how)".
func HookBreadcrumb(identity, push, why string) string {
	who := cmp.Or(identity, "<seat>")
	what := "the " + push + " push is not live"
	if push == "" {
		what = why
	}
	return fmt.Sprintf("%s --actor %s (%s; the coordinator %s wants the sprint push and the bus push live, both armed by that one command, under a Monitor); "+
		"then answer the HOOK CHALLENGE it prints: nova-sprint hook --prove <challenge> --actor %s; "+
		"to stop: nova-sprint unhook --actor %s",
		HookFirst, who, what, who, who, who)
}

// HookSaid is the hook as the seat's status says it: state=<s> proven=<RFC3339|->
// unacked=<n>.
func HookSaid(rec HookRecord, ok bool, name string, now time.Time) string {
	if !ok {
		return "state=none proven=- unacked=0"
	}
	proven := "-"
	if HookLive(rec, ok, name, now) {
		proven = rec.Proven.UTC().Format(time.RFC3339)
	}
	state := rec.State
	if state == HookHooked && rec.Proven.IsZero() {
		state = "unproven"
	}
	return fmt.Sprintf("state=%s proven=%s unacked=%d", state, proven, rec.Unacked)
}

// --- the waiting-judgment warning ---
//
// The owner, 2026-10-11: "Any card waiting for judgement should alert you via warning
// every second once it has been in this state for 10 seconds or longer." and "These
// must be push notifications." A look of the coordinator's hook (each second) that
// finds a judgment open JudgmentWarnAfter or longer pushes one warning over the hook
// listing every such judgment, and again until it is answered: an ack of the push does
// not silence it, and nothing turns it off. It repeats at a bounded rate (v1.2.17: a
// warning every second drowned a seat preparing a cleared sprint, its reads refused
// once ten minutes of them went unacknowledged): at once when a judgment joins the
// set, else no more often than once a JudgmentWarnEvery (JudgmentWarnDue;
// docs/SPEC-SPRINT.md, "The waiting-judgment warning").

// JudgmentWarnEvery is the most often the warning repeats for the same set of waiting
// judgments; a judgment joining the set is warned about at once.
const JudgmentWarnEvery = time.Minute

// JudgmentWarnDue says the look at now pushes the warning ws: the first warning of a
// set (none sent since the set was last empty: warnedAt zero), a judgment of ws not in
// the set last seen (warned, by id: it joined since), or JudgmentWarnEvery or longer
// since the last warning was sent (warnedAt). A set that only shrank waits its minute.
func JudgmentWarnDue(ws []JudgmentWarning, warned map[string]bool, warnedAt, now time.Time) bool {
	if warnedAt.IsZero() || now.Sub(warnedAt) >= JudgmentWarnEvery {
		return true
	}
	for _, w := range ws {
		if !warned[w.ID] {
			return true
		}
	}
	return false
}

// JudgmentWarnAfter is how long a judgment waits on the coordinator before it is warned
// about every look.
const JudgmentWarnAfter = 10 * time.Second

// JudgmentWarning is one judgment the warning names: its alias and id, the cards that
// wait on it, how long it has waited, and the commands that answer it.
type JudgmentWarning struct {
	Alias, ID string
	Cards     []string
	Waited    time.Duration
	Answer    []string
}

// JudgmentWarnings is every open judgment of groups that has waited JudgmentWarnAfter or
// longer at now, in the inbox's order; a quiet one (a review time set with wait) too,
// since the warning cannot be turned off. A judgment's wait starts at the older of its
// oldest note and when this hook first saw its group (firstSeen, by group id: a group
// computed at read time, machine:stopped or stale:<stream>, carries its read time as its
// oldest). firstSeen is kept up to date: a group not seen before is noted at now, and a
// group no longer open is forgotten.
func JudgmentWarnings(groups []Group, now time.Time, firstSeen map[string]time.Time) []JudgmentWarning {
	var out []JudgmentWarning
	open := map[string]bool{}
	for _, g := range groups {
		if g.Kind != Judgment {
			continue
		}
		open[g.ID] = true
		since, ok := firstSeen[g.ID]
		if !ok {
			since = now
			firstSeen[g.ID] = now
		}
		if !g.Oldest.IsZero() && g.Oldest.Before(since) {
			since = g.Oldest
		}
		waited := now.Sub(since)
		if waited < JudgmentWarnAfter {
			continue
		}
		w := JudgmentWarning{Alias: g.Alias, ID: g.ID, Cards: g.Members, Waited: waited.Truncate(time.Second)}
		for _, c := range g.Commands {
			if len(c.Lines) > 0 {
				w.Answer = append(w.Answer, c.Decision+": "+strings.Join(c.Lines, " && "))
			}
		}
		if len(w.Answer) == 0 {
			w.Answer = []string{"read it: nova-sprint inbox --open " + g.ID}
		}
		out = append(out, w)
	}
	for id := range firstSeen {
		if !open[id] {
			delete(firstSeen, id)
		}
	}
	return out
}

// JudgmentWarningSet is the warning's set: each judgment's alias (else id), in order, the
// one line said again, at most once a JudgmentWarnEvery, while the set does not change.
func JudgmentWarningSet(ws []JudgmentWarning) string {
	ids := make([]string, len(ws))
	for i, w := range ws {
		ids[i] = cmp.Or(w.Alias, w.ID)
	}
	return fmt.Sprintf("WARNING %d judgment(s) waiting on the coordinator %s or longer: %s", len(ws), JudgmentWarnAfter, strings.Join(ids, " "))
}

// JudgmentWarningText is the warning push's text when its set changed: the set's line,
// then each judgment with its cards, how long it has waited, and what answers it.
func JudgmentWarningText(ws []JudgmentWarning) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s; answer each: an ack of this push does not silence it, and it repeats once a minute until they are answered, at once when another joins\n", JudgmentWarningSet(ws))
	for _, w := range ws {
		fmt.Fprintf(&b, "%s %s cards=%s waited=%s\n", cmp.Or(w.Alias, "-"), w.ID, cmp.Or(strings.Join(w.Cards, ","), "-"), w.Waited)
		for _, a := range w.Answer {
			fmt.Fprintf(&b, "  %s\n", a)
		}
	}
	return b.String()
}
