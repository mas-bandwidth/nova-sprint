package sprint

import "strings"

// A judgment that changes a fleet member's or a friend's state (the sprint holds a member on
// an environment fault, a member reads down, a friend stalls) is told the coordinator on the
// bus as a message of its own, kind judgment, its subject naming the machine or the friend
// and the reason (the owner, 2026-10-14: "if a machine is auto held, you should get a warning
// notify"; six machines once sat held and unseen while the judgment went only to the retired
// seat hook). One message an episode: the judgment is one note while it stays open (a fault
// of another cause rewrites it in place), and the server's coordinator push tells each note
// once (cmd/nova-sprint/coordinator_push.go).

// StateJudgmentTypes are the judgments that change a member's or a friend's state.
var StateJudgmentTypes = map[string]bool{
	NMemberEnvFault:  true, // the member held on an environment fault
	NMemberIdleReaps: true, // the member held: its takes end with no model call
	NAdoptFailed:     true, // the member held: its adoption failed
	NStopMemberDown:  true, // the member down, not held by the coordinator
	NFriendStalled:   true, // the friend stalled: two wakes unanswered
	NHoldNoCause:     true, // a member held with no cause
}

// StateJudgment is the machine or friend a state judgment g is about and its reason; ok is
// false for any other group. The name is the member subject's (member:<m>) or the group's
// one primary; the reason is the first line of its text, else its type.
func StateJudgment(g Group) (who, reason string, ok bool) {
	if g.Kind != Judgment || !StateJudgmentTypes[g.Type] {
		return "", "", false
	}
	switch {
	case strings.HasPrefix(g.Stream, "member:"):
		who = strings.TrimPrefix(g.Stream, "member:")
	case len(g.Primaries) == 1: // the one name the judgment is on (a friend's stall)
		who = strings.TrimPrefix(strings.Join(g.Primaries, ""), "member:")
	}
	reason, _, _ = strings.Cut(strings.TrimSpace(g.What), "\n")
	if reason == "" {
		reason = g.Type
	}
	if len(reason) > 200 {
		reason = reason[:200] + "..."
	}
	return who, reason, true
}
