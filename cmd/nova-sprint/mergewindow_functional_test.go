//go:build functional

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// land pauses a batch while the merge queue of its base holds a group: refused before any
// git, nothing pushed or recorded, the stream not stopped and the cards queued; asked again
// just before the push, a group that appeared under the check pauses the push; once the
// queue clears the same cards land (docs/SPEC-SPRINT.md section 7, the lander's pause).
func TestLandPausesWhileTheBasesMergeQueueHoldsAGroup(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.ok("add --stream s1 --count 1 --one")
	r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "s1-1.txt", "s1-1\n")}, "s1-1")
	land := "land --repo-dir " + r.clone + " --base main"

	r.queue.hold("main", true)
	code, out, errs := r.do(land)
	assert.Equal(t, 1, code, "a paused batch is refused: %s%s", out, errs)
	assert.Contains(t, out+errs, "paused: the merge queue of main holds a group; the cards stay queued")
	assert.Equal(t, []string{"base"}, r.mainLog(), "nothing is pushed while the queue holds a group")
	assert.Equal(t, map[string]string{"s1-1": "merging/queued"}, r.places("s1-1"))
	assert.Equal(t, "merging", r.streamState("s1"), "a pause stops no stream")

	// a group that appears while the batch is checked pauses the push: the queue is clear
	// at the first ask, before any git, and holds a group at the second, before the push
	r.queue.hold("main", false)
	r.queue.mu.Lock()
	r.queue.from = r.queue.asked + 2
	r.queue.mu.Unlock()
	code, out, errs = r.do(land)
	assert.Equal(t, 1, code, "%s%s", out, errs)
	assert.Contains(t, out+errs, "paused: the merge queue of main holds a group")
	assert.Contains(t, out+errs, "push=", "the batch was built and checked before the pause")
	assert.Equal(t, []string{"base"}, r.mainLog(), "the push paused")

	r.queue.mu.Lock()
	r.queue.from = 0
	r.queue.mu.Unlock()
	out = r.ok(land)
	assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=main")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged"}, r.places("s1-1"))
	r.clean()
}

// merge-window open pauses every landing for its duration with its reason shown, and asks no
// forge while it is open; a dry run shows the pause too; once the window ends the cards land.
// It is the coordinator's, and refused whole for a duration that is none.
func TestMergeWindowOpenPausesLandingForItsDurationWithTheReason(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.ok("add --stream s1 --count 1 --one")
	r.queued(map[string]string{"s1-1": r.head("s1-1", "main", "s1-1.txt", "s1-1\n")}, "s1-1")
	land := "land --repo-dir " + r.clone + " --base main"

	code, _, errs := r.do("merge-window open --reason 'no duration'")
	assert.Equal(t, 1, code, errs)
	assert.Contains(t, errs, "--for wants a duration above zero")

	out := r.ok("merge-window open --for 10m --reason 'the release merges by hand' --dry-run")
	assert.Contains(t, out, "MERGE-WINDOW OPEN DRY-RUN for=10m0s until=")
	out = r.ok(land + " --dry-run")
	assert.NotContains(t, out, "paused:", "a dry run opens no window")

	out = r.ok("merge-window open --for 10m --reason 'the release merges by hand'")
	assert.Contains(t, out, "merge window open until ")
	assert.Contains(t, out, "(the release merges by hand): landing pauses")
	asked := r.queue.asked
	for _, line := range []string{land, land + " --dry-run"} {
		code, out, errs := r.do(line)
		assert.Equal(t, 1, code, "%s: %s%s", line, out, errs)
		assert.Contains(t, out+errs, "paused: a merge window is open until ", line)
		assert.Contains(t, out+errs, "(the release merges by hand)", line)
	}
	assert.Equal(t, asked, r.queue.asked, "an open window asks no forge")
	assert.Equal(t, []string{"base"}, r.mainLog())
	assert.Equal(t, map[string]string{"s1-1": "merging/queued"}, r.places("s1-1"))

	r.a.sleep(10 * time.Minute)
	out = r.ok(land)
	assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=main")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged"}, r.places("s1-1"))
	r.clean()
}
