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
// challenge it read off that stream, over the same connection; the server challenges
// again every HookEvery, and a challenge unanswered for HookAnswerBound unhooks. The
// server keeps its word in the store (HookRecord, the key seat-hook) and every
// coordinator command reads it (HookGate).

// The hook's states, as the record says them.
const (
	HookHooked = "hooked" // a subscription is open (proven or not: HookRecord.Proven)
	HookAway   = "away"   // the seat ran nova-sprint unhook
	HookGone   = "gone"   // the subscription ended without unhook: a drop, a missed challenge, a server restart
)

// HookEvery is how often the server challenges a proven hook again; HookAnswerBound is
// how long a challenge waits for its answer before the server unhooks; HookAckBound is
// how long a delivered push may go unacknowledged before every coordinator command is
// refused naming it.
const (
	HookEvery       = 10 * time.Minute
	HookAnswerBound = 5 * time.Minute
	HookAckBound    = 10 * time.Minute
)

// HookFirst is the one line every coordinator command fails with while the seat has no
// live, proven hook (the owner's words, exactly).
const HookFirst = "You must hook in first: run nova-sprint hook"

// HookItem is a push delivered over the hook: its id on the subscription, what it is
// (the inbox group's id, or the server's own word), and when it was delivered.
type HookItem struct {
	ID   uint64    `json:"id"`
	What string    `json:"what"`
	At   time.Time `json:"at"`
}

// HookRecord is the server's word on the seat's hook, the one the gate reads: who
// hooked, the subscription's id, its state and since when, the last challenge answered
// over it (Proven) and the time the hook counts until (Live: the outstanding
// challenge's bound), why it ended, and the pushes delivered and not acknowledged, with
// the oldest of them. Only the server writes it (cmd/nova-sprint/hook.go).
type HookRecord struct {
	Name    string    `json:"name"`
	Session string    `json:"session,omitempty"`
	State   string    `json:"state"`
	Since   time.Time `json:"since,omitzero"`
	Proven  time.Time `json:"proven,omitzero"`
	Live    time.Time `json:"live,omitzero"`
	Why     string    `json:"why,omitempty"`
	Unacked int       `json:"unacked,omitempty"`
	Oldest  *HookItem `json:"oldest,omitempty"`
}

// HookLive says the record holds a live, proven hook for name at now: hooked, proven
// over its subscription, and within the outstanding challenge's bound (SeatHook.tla,
// Gate: rec.state = "hooked", rec.proven, ~lateC). Live is that bound: the proof's plus
// HookEvery + HookAnswerBound, moved to a re-challenge's own bound when one is sent. A
// server that died leaves its record hooked, and the bound lapses it.
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
	return fmt.Sprintf("You must acknowledge push %d first (%s, delivered %s, unacknowledged past %s; %d unacknowledged): read it in the hook's stream, then run: nova-sprint hook --ack %d",
		o.ID, o.What, o.At.UTC().Format(time.RFC3339), HookAckBound, unacked, o.ID)
}

// HookProven is the record after the session answered the outstanding challenge at
// now: proven, and live until the outstanding challenge's bound (SeatHook.tla, Prove):
// the proof's plus HookEvery + HookAnswerBound, moved to a re-challenge's own bound
// when one is sent.
func HookProven(rec HookRecord, now time.Time) HookRecord {
	rec.Proven = now
	rec.Live = now.Add(HookEvery + HookAnswerBound)
	return rec
}

// --- every caller's two pushes (docs/SPEC-SPRINT.md, "The two pushes") ---
//
// The owner, 2026-10-11: "Nothing works unless hooked in, including bus, friend comms,
// sprint as friend, sprint as coordinator." "If you are not hooked in to push
// notifications, every verb except "nova-sprint hook" must fail, and tell you to hook
// in. For a coordinator, this is hooking into to sprint push notifications and message
// bus push notifications. For a friend in sprint as worker, it is message bus and work
// notifications as push." "You can "nova-sprint unhook" to unhook from the
// notifications." And: "the error leads you with breadcrumbs to what you need to do".
//
// Each caller's hook is one shared record in the bus store, hook:<identity> (HookShared),
// the layout nova-tools' nova-hook reads and writes too, so `nova-hook status` shows the
// sprint's seat and the sprint's gate reads a friend's hook armed by nova-hook. The gate
// reads it through Hooker, so the record's home can move to the shared module
// (github.com/mas-bandwidth/nova-tools/hook) with no change to the gate.

// The roles a hook is armed for, and the pushes each one wants live.
const (
	HookRoleCoordinator = "coordinator"
	HookRoleWorker      = "worker"

	HookPushSprint = "sprint" // judgments and pushes for the coordinator, over the server's hook stream
	HookPushBus    = "bus"    // messages to the caller's name on the message bus
	HookPushWork   = "work"   // cards dealt to a worker friend, over the server's hook stream
)

// HookRolePushes is the pushes role wants live, in the order the breadcrumb names them;
// nil for a role that is none.
func HookRolePushes(role string) []string {
	switch role {
	case HookRoleCoordinator:
		return []string{HookPushSprint, HookPushBus}
	case HookRoleWorker:
		return []string{HookPushBus, HookPushWork}
	}
	return nil
}

// HookSharedPrefix is the shared record's key prefix in the bus store: hook:<identity>,
// a hash with the fields role, pushes (comma separated), armed_at, proved_at and
// unhooked_at (RFC 3339, UTC; empty or absent for never).
const HookSharedPrefix = "hook:"

// HookSharedKey is identity's shared record key.
func HookSharedKey(identity string) string { return HookSharedPrefix + identity }

// HookShared is one caller's hook as the shared record says it: the role it was armed
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

// HookSharedOf is identity's record from the hash's fields; ok false when the hash
// holds no role (no record). A time that does not read is never: the safe side.
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

// HookSharedBound is how long a proof of the pushes holds: the next challenge comes
// HookEvery after it and may be answered for HookAnswerBound, as the sprint push's.
const HookSharedBound = HookEvery + HookAnswerBound

// PushLive says push is live on the record for role at now: armed for role with push,
// not unhooked since, and proven since it was armed, within HookSharedBound. A record
// with any of these missing is not live (fail closed).
func (h HookShared) PushLive(role, push string, now time.Time) bool {
	switch {
	case h.Identity == "" || h.Role != role || !slices.Contains(h.Pushes, push):
		return false
	case h.ArmedAt.IsZero() || h.ProvedAt.IsZero() || h.ProvedAt.Before(h.ArmedAt):
		return false
	case !h.UnhookedAt.IsZero() && !h.UnhookedAt.Before(h.ArmedAt):
		return false
	}
	return !now.After(h.ProvedAt.Add(HookSharedBound))
}

// Hooker is where the shared records live (the bus store today; the shared module's
// store once it is tagged): read one, arm one for a role, record both pushes proven,
// and record the unhook. Every write is the record's own; none of them deletes.
type Hooker interface {
	Shared(ctx context.Context, identity string) (HookShared, bool, error)
	Arm(ctx context.Context, identity, role string, at time.Time) error
	Prove(ctx context.Context, identity string, at time.Time) error
	Unhook(ctx context.Context, identity string, at time.Time) error
}

// HookCall is what the gate knows of one call: the caller's role and name, the
// server's word on the coordinator's sprint push (Seat, SeatOK), the shared record
// (Shared, SharedOK), and, when something could not be read, why (Unread): the gate
// then refuses with it.
type HookCall struct {
	Role, Identity string
	Seat           HookRecord
	SeatOK         bool
	Shared         HookShared
	SharedOK       bool
	Unread         string
}

// HookGateCall is why the call may not run at now, "" when it may: the first push of
// the role's that is not live, named in the breadcrumb (HookBreadcrumb); then, for the
// coordinator, a push delivered and unacknowledged past its bound (HookGate). A call it
// cannot judge (Unread, or a role that is none) is refused: fail closed.
func HookGateCall(c HookCall, now time.Time) string {
	pushes := HookRolePushes(c.Role)
	if len(pushes) == 0 {
		return HookBreadcrumb(c.Role, c.Identity, "", "the caller's role "+orNone(c.Role)+" is no hook role")
	}
	if c.Unread != "" {
		return HookBreadcrumb(c.Role, c.Identity, "", c.Unread)
	}
	for _, p := range pushes {
		live := c.Shared.PushLive(c.Role, p, now)
		if p == HookPushSprint {
			live = HookLive(c.Seat, c.SeatOK, c.Identity, now)
		}
		if !live {
			return HookBreadcrumb(c.Role, c.Identity, p, "")
		}
	}
	if c.Role == HookRoleCoordinator {
		return HookGate(c.Seat, c.SeatOK, c.Identity, now)
	}
	return ""
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// HookBreadcrumb is the gate's one line: HookFirst, then what is not live (the push, or
// why the hook could not be read), the caller's role and name, and the exact commands
// that hook it in, answer the challenge, and stop. The owner: "the error leads you with
// breadcrumbs to what you need to do -- which is to HOOK IN (here is exactly how)".
func HookBreadcrumb(role, identity, push, why string) string {
	who := identity
	if who == "" {
		who = "<name>"
	}
	pushes := HookRolePushes(role)
	if len(pushes) == 0 {
		pushes = []string{HookPushBus, HookPushWork}
	}
	what := "the " + push + " push is not live"
	if push == "" {
		what = why
	}
	return fmt.Sprintf("%s --actor %s (%s; the %s %s wants the %s push and the %s push live, both armed by that one command, under a Monitor); "+
		"then answer the HOOK CHALLENGE it prints: nova-sprint hook --prove <challenge> --actor %s; "+
		"to stop: nova-sprint unhook --actor %s; outside the sprint the same hook is: nova-hook hook --as %s --role %s",
		HookFirst, who, what, orNone(role), who, pushes[0], pushes[1], who, who, who, orNone(role))
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
// must be push notifications." Every look of the coordinator's hook (each second) that
// finds a judgment open JudgmentWarnAfter or longer pushes one WARNING over the hook
// listing every such judgment, again every second until it is answered: an ack of the
// push does not silence it, and nothing turns it off.

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
// since the warning cannot be turned off.
func JudgmentWarnings(groups []Group, now time.Time) []JudgmentWarning {
	var out []JudgmentWarning
	for _, g := range groups {
		if g.Kind != Judgment {
			continue
		}
		waited := g.Waited
		if !g.Oldest.IsZero() {
			waited = now.Sub(g.Oldest)
		}
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
	return out
}

// JudgmentWarningText is the warning push's text: one line naming how many, then each
// judgment with its cards, how long it has waited, and what answers it.
func JudgmentWarningText(ws []JudgmentWarning) string {
	var b strings.Builder
	fmt.Fprintf(&b, "WARNING %d judgment(s) waiting on the coordinator %s or longer: answer each; an ack of this push does not silence it, and it repeats every second until they are answered\n", len(ws), JudgmentWarnAfter)
	for _, w := range ws {
		fmt.Fprintf(&b, "%s %s cards=%s waited=%s\n", cmp.Or(w.Alias, "-"), w.ID, cmp.Or(strings.Join(w.Cards, ","), "-"), w.Waited)
		for _, a := range w.Answer {
			fmt.Fprintf(&b, "  %s\n", a)
		}
	}
	return b.String()
}
