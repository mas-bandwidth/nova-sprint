package sprint

import (
	"strings"
	"time"
)

// A judgment that changes a fleet member's or a friend's state (the sprint holds a member on
// an environment fault, a member reads down, a friend reads down) is told the coordinator on the
// bus as a message of its own, kind judgment, its subject naming the machine or the friend
// and the reason (the owner, 2026-10-14: "if a machine is auto held, you should get a warning
// notify"; six machines once sat held and unseen while the judgment went only to the retired
// seat hook). Each open note owns a frozen coordinator message and its durable
// success receipt. Cause rewrites and waits preserve it; a counted down transition
// begins a new status episode (cmd/nova-sprint/coordinator_push.go).

// StateJudgmentTypes are the judgments that change a member's or a friend's state.
var StateJudgmentTypes = map[string]bool{
	NMemberEnvFault:  true, // the member held on an environment fault
	NMemberIdleReaps: true, // the member held: its takes end with no model call
	NAdoptFailed:     true, // the member held: its adoption failed
	NStopMemberDown:  true, // the member down, not held by the coordinator
	NHoldNoCause:     true, // a member held with no cause
}

// StateJudgment is the machine or friend a state judgment g is about and its reason; ok is
// false for any other group. The name is the member subject's (member:<m>) or the group's
// one primary; the reason is the first line of its text, else its type.
func StateJudgment(g Group) (who, reason string, ok bool) {
	if g.Kind != Judgment || (!StateJudgmentTypes[g.Type] && !(g.Type == NStatus && (strings.Contains(g.What, " is down ") || strings.Contains(g.What, " is held ")))) {
		return "", "", false
	}
	switch {
	case strings.HasPrefix(g.Stream, "member:"):
		who = strings.TrimPrefix(g.Stream, "member:")
	case len(g.Primaries) == 1: // the one name the judgment is on (a friend's status)
		who = strings.TrimPrefix(strings.TrimPrefix(strings.Join(g.Primaries, ""), "member:"), "friend.")
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

// StateEpisodeKey distinguishes transitions that reuse a status note ID.
// Cause changes to any other judgment keep the same episode.
func StateEpisodeKey(n Note) string {
	if n.Type == NStatus {
		return n.ID + ":" + n.At.UTC().Format(time.RFC3339Nano)
	}
	return n.ID
}

// StateStatusOwner identifies an existing causal judgment that owns a fleet
// down/held status notification. The status audit remains, but sends no duplicate.
func StateStatusOwner(g Group, groups []Group) string {
	if g.Type != NStatus && g.Type != NStatusChanged {
		return ""
	}
	if !strings.Contains(g.What, " is down ") && !strings.Contains(g.What, " is held ") {
		return ""
	}
	if len(g.Primaries) != 1 {
		return ""
	}
	row := strings.TrimPrefix(strings.Join(g.Primaries, ""), "member:")
	if strings.HasPrefix(row, "friend.") {
		return ""
	}
	for _, cause := range groups {
		if cause.Type == NStatus {
			continue
		}
		who, _, ok := StateJudgment(cause)
		if ok && who == row {
			return cause.ID
		}
	}
	return ""
}
