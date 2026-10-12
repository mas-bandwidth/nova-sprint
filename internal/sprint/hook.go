package sprint

import (
	"fmt"
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
// over it (Proven) and the persisted activity lease deadline (Live), why it ended, and the pushes delivered and not acknowledged, with
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
// over its subscription, and within the persisted activity lease (SeatHook.tla,
// Gate). Live is the last persisted act plus HookSilentBound (HookProven, HookActed);
// batching and failed renewals can leave it behind newer acts. A dead server leaves it hooked, and
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
