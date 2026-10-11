package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A clear mid-wait is survived (the seat ledger v1.2.4-held-2026-10-10, bug 6):
// the sprint's epoch advanced under the loop, and its next look at the inbox
// was refused MEMBEREPOCH ("member belongs to another epoch") and the loop
// exited. A long-running loop re-reads the active epoch and carries on; it
// never exits on a clear (docs/SPEC-SPRINT.md section 13: "run goes on at the
// new epoch").
//
// The wait's guard is the test app's clock: the clear runs at one of its
// steps, as the other sleeps-under tests run the sprint under the wait.

// atBeat runs f once, at the loop's next sleep of the clock that is not a
// sleep of f's own commands: the sprint moves under the waiting loop.
func atBeat(f func()) func(int) {
	busy, done := false, false
	return func(int) {
		if busy || done {
			return // a sleep of the hook's own commands is not a beat of the loop
		}
		busy = true
		defer func() { busy = false }()
		done = true
		f()
	}
}

// inbox --wait ends at the clear as at a machine stop, exit 0, and the inbox
// it prints reports the clear as a happened line: never a MEMBEREPOCH refusal.
func TestInboxWaitEndsAtAClearReportingTheHappenedLine(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 1 --one")
	ta.ok("start")
	ta.ok("tick")
	ta.atSleep(atBeat(func() {
		ta.ok("clear --confirm sprint") // the epoch advances under the wait
	}))
	out := ta.ok("inbox --wait --timeout 1m")
	assert.Contains(t, out, "inbox --wait: the machine stopped\n", "the wait ends at the clear as at a stop:\n%s", out)
	assert.Contains(t, out, "HAPPENED", "the inbox reports the clear:\n%s", out)
	assert.Contains(t, out, "the machine stopped", "the clear's happened line:\n%s", out)
	assert.NotContains(t, out, "MEMBEREPOCH", "the wait was refused by the new epoch:\n%s", out)
}

// The seat's push loop goes on at the new epoch: the clear mid-wait neither
// ends it nor is pushed twice; the first judgment raised in the new epoch is
// pushed, and the interrupt ends the loop with exit 0.
func TestInboxWaitPushKeepsRunningThroughAClear(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 1 --one")
	ta.ok("start")
	ta.ok("tick")
	dir := filepath.Join(t.TempDir(), "inbox", "sprint-judgments")
	in := ta.interruptible()
	phase := 0
	ta.atSleep(func(int) {
		if phase == 2 {
			return
		}
		switch {
		case phase == 0:
			phase = 1
			ta.ok("clear --confirm sprint") // the epoch advances under the loop
			ta.ok("start")
			ta.ok("add --stream s2 --count 1 --one")
			ta.deal(1)
			ta.failOnce("m1", "s2-1.w1@1", "tests red") // a judgment of the new epoch
			ta.ok("tick")
		case phase == 1:
			// the loop went on at the new epoch and pushed the judgment:
			// now the interrupt ends it
			if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
				phase = 2
				in.now(t)
			}
		}
	})
	code, out, errs := ta.do("inbox --wait --push " + dir + " --timeout 10m")
	require.Equal(t, 0, code, "the loop ended in the clear: exit %d\n%s%s", code, out, errs)
	fresh := ta.group(sprint.NWorkFailed, "s2")
	assert.Contains(t, out, "INBOX OK pushed="+fresh.Notes[0]+" file="+filepath.Join(dir, fresh.Notes[0]+".md")+"\n",
		"the new epoch's judgment was pushed:\n%s%s", out, errs)
	assert.NotContains(t, out+errs, "MEMBEREPOCH", "the loop was refused by the new epoch:\n%s%s", out, errs)
	assert.Equal(t, 1, in.released, "the interrupt was released")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "one file: the new epoch's judgment")
}
