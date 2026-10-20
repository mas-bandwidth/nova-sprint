package sprint_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/require"
)

// exitKinds is every judgment kind the tick, wait refusal or wait exception names: the
// kinds a card can wait on that must have one JudgmentExits entry.
func exitKinds() []string {
	seen := map[string]bool{}
	var out []string
	for k := range sprint.TickDecisions {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for k := range sprint.WaitRefused {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for k := range sprint.WaitAllowedOnACard {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// Every judgment kind has one exit at its deadline: today's three safe server defaults,
// or the owner delivery. A kind with no entry fails naming it, and the reversed witness
// (one kind's entry hidden by the test-only seam) fails naming that kind.
// tla/WaitFor.tla ServerDefault, OwnerDeliver; docs/SPEC-SPRINT.md section 8.
func TestEveryJudgmentKindHasADeadlineExit(t *testing.T) {
	t.Parallel()
	kinds := exitKinds()
	require.NotEmpty(t, kinds)
	require.Empty(t, sprint.MissingJudgmentExits(kinds, nil), "every kind needs one JudgmentExits entry")
	for _, k := range kinds {
		exit := sprint.SafeDecisionDefault(k)
		require.Contains(t, []string{sprint.ExitHoldWithCause, sprint.ExitWaitReAsk, sprint.ExitOwner}, exit,
			"%s: %q is a safe default or the owner delivery, never a silent none", k, exit)
		require.True(t, sprint.ExitIsSafe(exit), "%s: %q is never terminal (DefaultIsSafe)", k, exit)
	}
	// The reversed witness: the one entry for NConflict hidden, and the check names it.
	missing := sprint.MissingJudgmentExits(kinds, func(kind string) bool { return kind == sprint.NConflict })
	require.Equal(t, []string{sprint.NConflict}, missing, "a kind with no entry fails by name")
}

// A stream stop unanswered past twice its deadline reaches the owner by the existing owner
// delivery, once, and again each owner window while it stays open. tla/WaitFor.tla
// OwnerDeliver; tla/DecisionsOwed.tla OwnerDue; docs/SPEC-SPRINT.md section 8.
func TestDeadlineExitAStreamStopUnansweredReachesTheOwner(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0).UTC()
	n := sprint.Note{ID: "judgment-conflict", Kind: sprint.Judgment, Type: sprint.NConflict,
		Primaries: []string{"s1-1"}, Count: 1, At: now.Add(-2 * sprint.DeadlineJudgment), What: "merge conflict"}
	open := []sprint.Open{{Key: sprint.OpenKey(n.ID, "s1-1"), Note: n}}
	require.True(t, sprint.OwnerExitDue(now, n, nil), "twice its deadline hands it to the owner")
	for _, at := range []time.Time{now, now.Add(sprint.OwnerWindow), now.Add(2 * sprint.OwnerWindow)} {
		set, body, _ := sprint.DecisionBatch(at, open, nil, "owner", nil, nil)
		require.Equal(t, n.ID, set, "%s: the owner delivery carries it once", at)
		require.Contains(t, body, "safe-default=owner", "%s: the owner exit is the judgment's exit", at)
	}
	require.Equal(t, sprint.OwnerWindow, 30*time.Minute, "the owner lane is repeated each window")
}

// A server-default hold still open after its second re-raise unanswered reaches the owner:
// not before, and once at and after that point. tla/WaitFor.tla OwnerDeliver.
func TestDeadlineExitAHeldByDefaultCardIsDeliveredToTheOwner(t *testing.T) {
	t.Parallel()
	r := newHoldRig(t, 1, 0)
	r.must(store.NoteStep("fixture bound", sprint.Note{Kind: sprint.Judgment, Type: sprint.NBound,
		Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, At: r.now, What: "attempt budget exhausted"}))
	r.mu.Lock()
	r.now = r.now.Add(2 * sprint.DeadlineJudgment)
	r.mu.Unlock()
	r.must(store.TickPartStep("overdue", sprint.TickOverdue, sprint.TickReq{}, nil, nil, nil))
	r.must(store.DrainStep())
	held := sprint.DefaultHeldDecisions(r.snap())
	require.Len(t, held, 1, "the bound's safe default holds the card")
	require.Equal(t, sprint.NServerDefaultHold, held[0].Type)

	set, _, _ := sprint.DecisionBatch(r.now, nil, held, "owner", nil, r.snap())
	require.Empty(t, set, "the hold reaches the owner only after its second re-raise unanswered")

	after := r.now.Add(2 * sprint.DeadlineJudgment)
	set, body, _ := sprint.DecisionBatch(after, nil, held, "owner", nil, r.snap())
	require.Equal(t, held[0].ID, set, "the second re-raise hands the hold to the owner")
	require.Contains(t, body, held[0].ID)
}

// No exit authorizes a terminal act: no exit drops, accepts, merges, lands or edits a
// brief. tla/DecisionsOwed.tla DefaultIsSafe; docs/SPEC-SPRINT.md section 8.
func TestDeadlineExitNoExitAuthorizesATerminalAct(t *testing.T) {
	t.Parallel()
	for kind, exit := range sprint.JudgmentExits {
		require.True(t, sprint.ExitIsSafe(exit), "%s: %q must be a safe exit", kind, exit)
		low := strings.ToLower(exit)
		for _, terminal := range []string{"drop", "accept", "merge", "land", "brief", "edit", "rework", "return"} {
			require.NotContains(t, low, terminal, "%s: %q authorizes a terminal act", kind, exit)
		}
		require.Equal(t, exit, sprint.SafeDecisionDefault(kind), "%s: SafeDecisionDefault reads the table", kind)
	}
}
