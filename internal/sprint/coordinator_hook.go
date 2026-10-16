package sprint

import (
	"cmp"
	"fmt"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-tools/hook"
)

// The coordinator hooks in with nova-hook (docs/SPEC-SPRINT.md, "The hook"; the owner,
// 2026-10-10: "You MUST be required to 'hook' in as coordinator." "Every command fails
// with 'You must hook in first'."; and 2026-10-14, v1.3.6: the sprint's own seat hook and
// seat push are retired, and the coordinator proves only through nova-hook).
//
// The one truth is the nova-hook record (nova-tools hook, Redis beside the bus): the seat
// runs `nova-hook hook --as <name> --role coordinator` under a Monitor, `nova-hook serve`
// challenges it once a minute, only the model answers (`nova-hook prove`), and two misses
// unhook it. A coordinator's role carries two pushes: the bus push, which nova-hook holds
// and which the challenge proves, and the sprint push, which the sprint's server holds for
// the coordinator while it runs and reaches the bus (cmd/nova-sprint coordinator_push.go:
// every judgment and note for the coordinator goes to the coordinator's name on the bus,
// which nova-hook pushes into the session). Every coordinator command reads the record
// and runs only while the record is proven (hook.Status.Gate, as nova-tools' hookgate
// reads it).

// HookFirst leads every refusal of a coordinator command for want of the hook (the
// owner's words).
const HookFirst = "You must hook in first"

// CoordinatorWant is what a coordinator command asks of name's record: hooked in as the
// coordinator, every push of the role live and proven.
func CoordinatorWant(name string) hook.Want {
	return hook.Want{Name: name, Need: hook.PushesOf(hook.Coordinator), Roles: []hook.Role{hook.Coordinator}, Role: hook.Coordinator}
}

// SprintPushHeldBy is who holds a coordinator's sprint push: the sprint's server.
// (nova-tools' hook.HeldBy still names the retired holder; the gate's breadcrumb says
// this line in its place.)
const SprintPushHeldBy = "the sprint's server (nova-sprint run), which holds it for the coordinator while it runs and reaches the bus"

// CoordinatorGate is why name may not run a coordinator command on the record st, "" when
// it may (hook.Status.Gate over CoordinatorWant): HookFirst, what is not live, the
// command that hooks the coordinator in, and the rest of the breadcrumb, one line.
func CoordinatorGate(name string, st hook.Status) string {
	r := st.Gate(CoordinatorWant(name))
	if r == nil {
		return ""
	}
	return HookRefusal(r)
}

// HookRefusal is the gate's refusal r as the one line a coordinator command fails with.
func HookRefusal(r *hook.Refusal) string {
	lines := make([]string, 0, len(r.Lines))
	sprintLine := "the " + string(hook.Sprint) + " push of a " + string(hook.Coordinator) + " is held by: "
	for _, l := range r.Lines {
		if strings.HasPrefix(l, sprintLine) {
			l = sprintLine + SprintPushHeldBy
		}
		lines = append(lines, l)
	}
	// DO NOT REMOVE THIS WITHOUT GLENN'S EXPLICIT PERMISSION
	// the owner, 2026-10-16: an unanswered challenge stops every verb, "telling you to hook
	// in properly and receive push notifications".
	out := fmt.Sprintf("%s: %s; run: %s under a Monitor and receive push notifications, and answer each HOOK CHALLENGE it prints with nova-hook prove", HookFirst, r.Why, r.Hook)
	if len(lines) > 0 {
		out += "; " + strings.Join(lines, "; ")
	}
	return out
}

// HookUnread is the refusal of a coordinator command whose record could not be read:
// fail closed, naming why and where to look.
func HookUnread(name, why string) string {
	who := cmp.Or(name, "<seat>")
	return fmt.Sprintf("%s: the nova-hook record of %s could not be checked (%s), so nothing runs (fail closed); see it: nova-hook status --as %s; hook in: %s",
		HookFirst, who, why, who, hook.HookCommand(who, hook.Coordinator))
}

// HookSaid is the coordinator's record as the seat's status says it: nova-hook's own
// words (state, role, epoch and each push), and when the session last answered.
func HookSaid(st hook.Status, now time.Time) string {
	s := st.Words()
	if st.Down != "" {
		s += " down=" + strings.ReplaceAll(st.Down, " ", "_")
	}
	if !st.AnswerAt.IsZero() {
		s += " answered=" + now.Sub(st.AnswerAt).Round(time.Second).String() + "_ago"
	}
	return s
}

// HookM is the coordinator's nova-hook record as a check read it: the holder and its
// status; Measured false is a check that did not read it, which says no line.
type HookM struct {
	Measured bool        `json:"measured,omitempty"`
	Holder   string      `json:"holder,omitempty"`
	Status   hook.Status `json:"status,omitzero"`
}
