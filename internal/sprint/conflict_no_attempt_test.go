package sprint_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// conflictT0 is the clock these tests hand the machine: an argument, starting at 1000s.
var conflictT0 = time.Unix(1000, 0).UTC()

// newConflictRigAt is newConflictRig with the clock given: the same twin store, flash and pro
// routes, members m1 and m2, readers reader-a..c, and stream s1 of three flash cards.
func newConflictRigAt(t *testing.T, now time.Time) *conflictRig {
	t.Helper()
	r := &conflictRig{t: t, m: store.NewMem(), ctx: context.Background(), now: now}
	n := 0
	r.st = &store.Store{B: r.m, Names: sprint.Names{Prefix: "t-"}, Actor: "coordinator", AnswerRules: true,
		Now:   func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
		NewID: func() string { r.mu.Lock(); defer r.mu.Unlock(); n++; return fmt.Sprint(n) },
		Sleep: func(time.Duration) {}}
	require.NoError(t, r.st.Init(r.ctx))
	require.NoError(t, r.m.RowsAdd(r.ctx, "t-readers", []string{"reader-a", "reader-b", "reader-c"}))
	require.NoError(t, r.m.SetCoordinator(r.ctx, "coordinator"))
	r.m.SetRoutes([]sprint.Route{
		{Name: "flash-a", Tier: "flash", Provider: "prov-flash-a", Model: "model-flash-a", Tokens: 1000, Deadline: conflictRouteSeconds, Enabled: true},
		{Name: "pro-a", Tier: "pro", Provider: "prov-pro-a", Model: "model-pro-a", Tokens: 1000, Deadline: conflictRouteSeconds, Enabled: true},
	})
	r.beat()
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 4}))
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m2", Width: 4}))
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", Count: 3, Brief: "c: the work (s1) tier: flash\nREPO: mas-bandwidth/nova-tools\n\nThe task.\n"}))
	_, _, _, err := r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	return r
}

// mergeConflict is the lander's refusal of a head that does not merge, in its own words, with
// the file the base moved.
func mergeConflict(head, path string) store.Step {
	return store.MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1, Conflict: "s1-1", ConflictKind: "file", ConflictPaths: []string{path},
		Note: "the head " + head + " of s1-1 does not merge: CONFLICT (content): Merge conflict in " + path})
}

// gateRefusal is the lander's refusal of a head whose merged tree fails the tree gate.
func gateRefusal(head string) store.Step {
	return store.MergeStep(sprint.MergeReq{Stream: "s1", Batch: 1, Conflict: "s1-1",
		Note: "the head " + head + " of s1-1 fails the tree gate: go vet ./...: exit status 1"})
}

// TestAConflictRefusalIsReworkedAtTheTipWithNoAttemptSpent: a landing refused only for a merge
// conflict with the advanced base is reworked in the refusing step, its head carried, no
// judgment, and the next attempt costs the cap nothing (sprint.landRefused, AttemptsSince;
// tla/WaitFor.tla ConflictSpendsNoAttempt; tla/Land.tla THE BOUND, ConflictNeverBounds). Red
// at BASE: the second refusal raised NBriefWrong at the attempt cap and the conflict counted.
func TestAConflictRefusalIsReworkedAtTheTipWithNoAttemptSpent(t *testing.T) {
	t.Parallel()
	r := newConflictRigAt(t, conflictT0)
	heads := []string{strings.Repeat("a1", 20), strings.Repeat("b2", 20), strings.Repeat("c3", 20)}
	r.toMergingAt("s1-1", heads[0])
	before := r.snap().Work.Card("s1-1")
	require.Equal(t, 0, sprint.AttemptsSince(before), "the accept is the last decision: the window starts at this attempt")

	r.must(mergeConflict(heads[0], "internal/x.go"))
	pr := r.snap().Work.Card("s1-1")
	require.Equal(t, sprint.Ready, pr.Col, "reworked at the tip in the refusing step")
	assert.Equal(t, heads[0], pr.F(sprint.FieldLandRefusedHead), "the refused head is kept (carried)")
	assert.Equal(t, "internal/x.go", pr.F(sprint.FieldConflictPaths), "the conflicting paths stay on the card")
	assert.Equal(t, "1", pr.F(sprint.FieldConflictRefusals), "the conflict is counted on the card")
	assert.Empty(t, pr.F(sprint.FieldBriefDefect), "no brief-defect mark")
	assert.Empty(t, r.openOnCard(sprint.NBriefWrong, "s1-1"), "no bound judgment")

	// the next attempt is the same one toward max_attempts: the conflict spent none
	r.toMergingAt("s1-1", heads[1])
	after := r.snap().Work.Card("s1-1")
	require.Equal(t, 2, after.Int("attempt"), "the reworked attempt is attempt 2")
	assert.Equal(t, sprint.AttemptsSince(before), sprint.AttemptsSince(after), "a conflict refusal spends no attempt toward max_attempts")

	// the reversed witness, with the conflict counted again by the test seam, is
	// TestConflictRefusalWitnessCountsTheAttemptAgain (bounds_test.go)
}

// TestTwoConflictRefusalsAreNeverABriefDefect is the java-fix-union-arm-kind-widened case of
// 2026-10-19: attempts 2 and 4 were both the landing's refusal for a merge conflict with the
// advanced base; neither raises NBriefWrong and the server default holds nothing
// (tla/WaitFor.tla ConflictSpendsNoAttempt; tla/Land.tla ConflictNeverBounds). Red at BASE:
// the second conflict at the cap raised the brief's judgment and the rule marked it.
func TestTwoConflictRefusalsAreNeverABriefDefect(t *testing.T) {
	t.Parallel()
	r := newConflictRigAt(t, conflictT0)
	heads := []string{strings.Repeat("a1", 20), strings.Repeat("b2", 20), strings.Repeat("c3", 20), strings.Repeat("d4", 20)}

	// attempt 1 fails as work, so the first conflict is attempt 2
	r.failedAttempt("s1-1", "build broken: internal/x/a.go:31: undefined: y")
	r.tick()
	require.Equal(t, 2, r.snap().Work.Card("s1-1").Int("attempt"), "attempt 2 after the failed rule")

	r.toMergingAt("s1-1", heads[1])
	r.must(mergeConflict(heads[1], "internal/x.go"))
	pr := r.snap().Work.Card("s1-1")
	require.Equal(t, sprint.Ready, pr.Col, "attempt 2's conflict is reworked at the tip")
	assert.Empty(t, r.openOnCard(sprint.NBriefWrong, "s1-1"), "no bound judgment at attempt 2")

	// attempt 3 fails as work, so the second conflict is attempt 4
	r.failedAttempt("s1-1", "tests red: TestX fails on the new branch")
	r.tick()
	require.Equal(t, 4, r.snap().Work.Card("s1-1").Int("attempt"), "attempt 4")
	r.toMergingAt("s1-1", heads[3])
	r.must(mergeConflict(heads[3], "internal/x.go"))
	pr = r.snap().Work.Card("s1-1")
	require.Equal(t, sprint.Ready, pr.Col, "attempt 4's conflict is reworked at the tip too")
	assert.Equal(t, "4", pr.F(sprint.FieldConflictRefusals), "the conflict is on the card; the accept at attempt 4 cleared the earlier window")
	assert.Empty(t, r.openOnCard(sprint.NBriefWrong, "s1-1"), "two conflicts are never a brief bound")
	for range 3 {
		r.tick()
	}
	pr = r.snap().Work.Card("s1-1")
	assert.Empty(t, pr.F(sprint.FieldBriefDefect), "never a brief defect")
	assert.Empty(t, r.openOnCard(sprint.NBriefWrong, "s1-1"), "no bound judgment, so no server-default hold on one")
}

// TestAConflictThenAGateRedStillBounds: a conflict does not excuse the head that fails after
// it. Two refusals of the tree gate, with a conflict before them, are still the brief's bound
// (sprint.AtBriefBound; tla/Land.tla SameRefusalHalts, ConflictNeverBounds).
func TestAConflictThenAGateRedStillBounds(t *testing.T) {
	t.Parallel()
	r := newConflictRigAt(t, conflictT0)
	heads := []string{strings.Repeat("a1", 20), strings.Repeat("b2", 20), strings.Repeat("c3", 20)}
	r.toMergingAt("s1-1", heads[0])
	r.must(mergeConflict(heads[0], "internal/x.go"))
	require.Equal(t, sprint.Ready, r.snap().Work.Card("s1-1").Col, "the conflict is reworked")

	r.toMergingAt("s1-1", heads[1])
	r.must(gateRefusal(heads[1]))
	require.Equal(t, sprint.Ready, r.snap().Work.Card("s1-1").Col, "the first gate refusal reworks")

	r.toMergingAt("s1-1", heads[2])
	r.must(gateRefusal(heads[2]))
	pr := r.snap().Work.Card("s1-1")
	require.Equal(t, sprint.Review, pr.Col, "the same gate failure twice is the brief's bound")
	open := r.openOnCard(sprint.NBriefWrong, "s1-1")
	require.Len(t, open, 1, "the bound's judgment")
	assert.Contains(t, open[0].Note.What, "failed the same way twice")
}
