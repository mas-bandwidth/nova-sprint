package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/friend"
)

// friend sync is a writer and a finisher of a friend's cards beside her daemon
// (docs/SPEC-FRIEND.md, a reworked brief opens with the fix): a mismatch can raise a seat
// judgment, but it does not rewrite the friend's reported verdict.
func TestAKeywordMissIsAJudgmentNeverAHold(t *testing.T) {
	t.Parallel()
	const fix = "assert the queue bound under shuffle"
	ta, _ := friendApp(t, "amy")
	ta.a.tip = tipIs(t, landHead)
	root := t.TempDir()
	ta.ok("friend sync --root " + root)
	ta.beatUp("amy")
	brief := filepath.Join(t.TempDir(), "s1-1.md")
	require.NoError(t, os.WriteFile(brief, []byte(passingBrief("s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy\nSTOP: the whole card is done")), 0o644))
	ta.ok("add --stream s1 --brief-dir " + filepath.Dir(brief))
	ta.ok("start")
	ta.ok("tick")
	ta.ok("friend sync --root " + root)
	outboxReport(t, root, "amy", "s1-1.w1", "Verdict: HOLD\nHead: "+landHead+"\n\nThe gate is red.\n")
	assert.Contains(t, ta.ok("friend sync --root "+root), "result=failed head="+landHead)
	ta.ok("tick")
	ta.ok("rework s1-1 --fix '" + fix + "'")
	ta.ok("tick")
	require.Contains(t, ta.ok("friend sync --root "+root), "FRIEND-CARD DELIVERED friend=amy card=s1-1.w2")

	jobs, err := filepath.Glob(filepath.Join(root, "amy-working", "inbox", "s1-1.w2*"))
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	job := filepath.Base(jobs[0])
	text, err := os.ReadFile(filepath.Join(jobs[0], "BRIEF.md"))
	require.NoError(t, err)
	lines := strings.Split(string(text), "\n")
	require.Greater(t, len(lines), 4)
	assert.True(t, strings.HasPrefix(lines[0], "STATUS: nova-sprint card s1-1.w2, epoch 0, attempt 2; "), lines[0])
	assert.Equal(t, friend.OneThingLeft+fix, lines[1], "the first line after STATUS is the fix")
	assert.True(t, strings.HasPrefix(lines[2], friend.ReaderFoundLabel), lines[2])
	assert.Contains(t, lines[3], "The carried work: attempt 1's head "+landHead+", carried onto sprint/s1-1.w2.g1.e0", "the failed result still recorded the pushed head, which the next attempt must carry")
	assert.Contains(t, string(text), "\nSTOP: "+fix+"\n", "the STOP is the fix alone")
	assert.NotContains(t, string(text), "STOP: the whole card is done")
	assert.NotContains(t, string(text), "The coordinator asks:", "the fix is said once, first")
	assert.Equal(t, string(text), friend.ReworkedBrief(string(text)), "the daemon's form: reworking it again changes nothing")

	// a LAND that does not name the fix is held by friend sync, its head kept
	outboxReport(t, root, "amy", job, "Verdict: LAND\nHead: "+landHead+"\n\nThe change is pushed and the gate is green.\n")
	assert.Contains(t, ta.ok("friend sync --root "+root), "FRIEND-CARD FINISHED friend=amy card=s1-1.w2 result=ok head="+landHead+": friend amy LAND: The change is pushed and the gate is green.")
	ta.ok("tick")
	var c cardView
	ta.json("card s1-1", &c)
	assert.Equal(t, sprint.Review, c.Primary.Col, "an unaddressed LAND reaches review")
	assert.Equal(t, "ok", c.Primary.F("result"))
}

// friendFinish, the one finish of friend sync and collect, holds a reworked
// card's LAND by the daemon's check: one that names the fix's key words lands at origin's tip,
// one that does not is a HOLD with its head kept, and a card that asks no fix lands as before.
func TestFriendFinishKeepsAnUnaddressedLandAndRaisesASeatJudgment(t *testing.T) {
	t.Parallel()
	p := sprint.Packet{Card: "s1-1.w2", Attempt: 2, Gen: 1, Branch: "sprint/s1-1.w2.g1.e0", BaseHead: landHead, BaseAttempt: 1, Fix: "assert the queue bound under shuffle",
		Primary: "s1-1", Brief: "s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nWHO: friend amy\n\nThe task."}
	ctx := context.Background()
	r, err := friendFinish(ctx, "amy", p, "Verdict: LAND\nHead: "+landHead+"\n\nThe change is pushed.\n", tipIs(t, landHead))
	require.NoError(t, err)
	assert.False(t, r.Failed, "the report's stated LAND is authoritative")
	assert.Equal(t, landHead, r.Head)
	assert.Contains(t, r.SeatJudgment, "does not name: assert, queue, bound, under, shuffle")
	assert.Equal(t, "friend amy LAND: The change is pushed.", r.Report)

	r, err = friendFinish(ctx, "amy", p, "Verdict: LAND\nHead: "+landHead+"\n\nThe queue bound is asserted under -shuffle=on.\n", tipIs(t, landHead))
	require.NoError(t, err)
	assert.False(t, r.Failed)
	assert.Equal(t, landHead, r.Head)
	assert.Empty(t, r.SeatJudgment)

	p.Fix, p.Attempt, p.BaseHead = "", 1, ""
	r, err = friendFinish(ctx, "amy", p, "Verdict: LAND\nHead: "+landHead+"\n\nDone.\n", tipIs(t, landHead))
	require.NoError(t, err)
	assert.False(t, r.Failed, "a card that asks no fix lands as before")
}

func TestARedealCarriesTheNewestPushedHead(t *testing.T) {
	const newest = "89abcdef0123456789abcdef0123456789abcdef"
	p := sprint.Packet{Card: "s1-1.w6", Primary: "s1-1", Attempt: 6, Gen: 1, Branch: "sprint/s1-1.w6.g1.e0", BaseHead: newest, BaseAttempt: 5,
		Brief: "s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nCARRY: s1-1 attempt 4 head=" + landHead + "\nTHE TASK. continue"}
	got := friendServerBrief("amy", p)
	assert.Contains(t, got, "CARRY: s1-1 attempt 5 head="+newest+"\n")
	assert.NotContains(t, got, "CARRY: s1-1 attempt 4 head="+landHead)
}
