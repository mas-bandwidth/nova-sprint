package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The first real sprint's start: other streams' work is admitted, then a round-2
// sentinel at the head of an empty stream. With nothing placed before it and no need of
// its own it is not reached while other work moves: it is simply next
// (docs/SPEC-SPRINT.md section 16). No "sentinel reached" judgment is written for it, by
// the add or by any tick, and release lands it all the same. One with cards placed
// before it is reached when they have landed, as before.
func TestEmptySentinelAutoReleasesWhileOtherWorkMoves(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead")
	ta.ok("add --stream s1 --count 2 --actor lead")
	out := ta.ok("add --stream docs --sentinel docs-round2 --actor lead")
	assert.NotContains(t, out, "reached", "the add marks it reached")
	ta.ok("add --stream docs d1 --one --actor lead")
	ta.ok("start --actor lead")
	ta.ok("tick")
	ta.ok("tick")
	for _, g := range ta.inboxGroups() {
		assert.NotEqual(t, sprint.NSentinelReached, g.Type, "reached with nothing before it: %+v", g)
	}
	assert.Empty(t, ta.primary("docs-round2").F("reached"))
	require.Equal(t, sprint.Landed, ta.primary("docs-round2").Col)
	require.Equal(t, "no producers", ta.primary("docs-round2").F("release_reason"))
	code, _, errs := ta.do("release docs-round2 --reason 'already automatic' --actor lead")
	require.Equal(t, 1, code)
	require.Contains(t, errs, "is landed")
	ta.ok("tick")
	assert.Equal(t, sprint.Waiting, ta.primary("d1").Col, "no actual consumer repo/merge receipt: next deal remains blocked")

}

// The other half of the condition (sprint.Reachable's no-work-in-flight branch): the same
// sentinel, next while s1's work moves, is reached by the step that lands the last of that
// work, with nothing placed before it, and d1 behind it stays waiting for the release
// (docs/SPEC-SPRINT.md section 16). With the branch gone it would never be reached.
func TestEmptySentinelDoesNotWaitForOtherStreamLanding(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead")
	ta.ok("add --stream s1 --count 1 --one --actor lead --brief-file " + proBriefFile(t))
	ta.ok("add --stream docs --sentinel docs-round2 --actor lead")
	ta.ok("add --stream docs d1 --one --actor lead --brief-file " + proBriefFile(t))
	ta.deal(1)
	ta.ok("take --as m1 s1-1.w1@1")
	ta.ok("finish --as m1 s1-1.w1@1")
	ta.ok("ask --actor lead")
	ta.ok("read --as reader-a --ok s1-1.r1.reader-a")
	ta.ok("ask --actor lead") // the second read, the first ok
	ta.ok("read --as reader-b --ok s1-1.r1.reader-b")
	ta.ok("accept s1-1 --actor lead")
	assert.False(t, hasGroup(ta.inboxGroups(), sprint.NSentinelReached), "reached while s1-1 was merging")
	ta.ok("merge --stream s1")
	require.Equal(t, sprint.Landed, ta.primary("docs-round2").Col)
	require.Equal(t, "no producers", ta.primary("docs-round2").F("release_reason"))
	assert.Empty(t, ta.primary("docs-round2").F("reached"))
	assert.Equal(t, sprint.Waiting, ta.primary("d1").Col, "reached is not released")
}

// The tick as the backstop: the work in flight leaves the table by a drop, which reaches
// nothing itself; the next tick finds nothing else moving and marks the sentinel reached.
func TestEmptySentinelReleaseRemainsAutomaticAfterOtherWorkDrops(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead")
	ta.ok("add --stream s1 --count 1 --one --actor lead")
	ta.ok("add --stream docs --sentinel docs-round2 --actor lead")
	ta.ok("start --actor lead")
	ta.ok("tick")
	assert.False(t, hasGroup(ta.inboxGroups(), sprint.NSentinelReached), "reached while s1-1 was ready")
	ta.ok("drop s1-1 --reason 'done elsewhere' --actor lead")
	assert.False(t, hasGroup(ta.inboxGroups(), sprint.NSentinelReached), "the drop itself reached it")
	ta.ok("tick")
	require.Equal(t, sprint.Landed, ta.primary("docs-round2").Col)
	require.Equal(t, "no producers", ta.primary("docs-round2").F("release_reason"))
	require.False(t, hasGroup(ta.inboxGroups(), sprint.NSentinelReached))
}

// The sequence of the first real sprint: a round-2 sentinel added at the head of an empty
// stream, then the round-1 cards placed before it. It is reached when they have landed,
// and not before.
func TestASentinelIsReachedWhenTheCardsPlacedBeforeItLand(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead")
	ta.ok("add --stream s1 --sentinel stop --actor lead")
	ta.ok("add --stream s1 s1-1 --one --before stop --actor lead --brief-file " + proBriefFile(t))
	assert.False(t, hasGroup(ta.inboxGroups(), sprint.NSentinelReached), "reached before the card placed before it landed")
	ta.deal(1)
	ta.ok("take --as m1 s1-1.w1@1")
	ta.ok("finish --as m1 s1-1.w1@1")
	ta.ok("ask --actor lead")
	ta.ok("read --as reader-a --ok s1-1.r1.reader-a")
	ta.ok("ask --actor lead") // the second read, the first ok
	ta.ok("read --as reader-b --ok s1-1.r1.reader-b")
	ta.ok("accept s1-1 --actor lead")
	ta.ok("merge --stream s1")
	progress := sentinelProgressNotes(t, ta, "s1")
	require.Len(t, progress, 1)
	assert.Contains(t, progress[0].What, "sentinel stop reached: 1 cards of s1 have landed")
	require.False(t, hasGroup(ta.inboxGroups(), sprint.NSentinelReached))
	require.Equal(t, sprint.Waiting, ta.primary("stop").Col, "actual producer ancestry is still absent")
}

// wait <judgment> --for quiets it for the whole period: it is not overdue, and it is
// listed after every judgment that is not waiting, marked quiet until its review time,
// so it does not sit at the top of every inbox read (docs/SPEC-SPRINT.md section 8).
func TestAWaitQuietsAJudgmentForTheWholePeriod(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead")
	ta.ok("add --stream s1 --count 1 --one --actor lead")
	ta.ok("add --stream s2 --count 1 --one --actor lead")
	ta.deal(2)
	ta.ok("take --as m1 s2-1.w1@1")
	ta.mu.Lock()
	ta.now = ta.now.Add(time.Minute)
	ta.mu.Unlock()
	ta.ok("finish --as m1 s2-1.w1@1 --failed --report 'verdict not-done'")
	ta.timerOnce("look-again", "lead") // a judgment a wait may hold: it holds no card (wait_refused.go)
	g := ta.group(sprint.NTimer, "")
	ta.ok("wait " + g.ID + " --for 2h --actor lead")
	ta.ok("start --actor lead")
	for range 4 {
		ta.mu.Lock()
		ta.now = ta.now.Add(10 * time.Minute)
		ta.mu.Unlock()
		ta.ok("tick")
	}
	var judg []sprint.Group
	for _, x := range ta.inboxGroups() {
		if x.Kind == sprint.Judgment {
			judg = append(judg, x)
		}
	}
	require.GreaterOrEqual(t, len(judg), 2, "%+v", judg)
	last := judg[len(judg)-1]
	assert.Equal(t, g.ID, last.ID, "the waited judgment is listed after the others: %+v", judg)
	assert.True(t, last.Quiet, "marked quiet")
	assert.False(t, last.Overdue, "not overdue within its period")
	assert.Contains(t, ta.ok("inbox"), "quiet until=")
}

// hasGroup says the inbox holds an open judgment of the type.
func hasGroup(gs []sprint.Group, typ string) bool {
	for _, g := range gs {
		if g.Kind == sprint.Judgment && g.Type == typ {
			return true
		}
	}
	return false
}
