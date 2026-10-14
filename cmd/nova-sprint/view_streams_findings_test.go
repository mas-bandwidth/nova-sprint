package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestViewWorkerAcceptsFriendRowNameAndStreamsHonourMax pins two findings read on the
// v1.2.22 base (docs/SPEC-SPRINT.md section 11, "Role views"):
//
//   - view worker --as <friend> took only the friend's bare name; it must also take her
//     fleet row's spelling friend.<name>, the one queue accepts, and name both spellings
//     when it refuses an unknown name.
//   - streams registers --max (verbSetup) and ignored it; it must list at most --max
//     streams and --max cards of a stream with a MORE line, 0 is all. The worker view's
//     cards list does the same.
func TestViewWorkerAcceptsFriendRowNameAndStreamsHonourMax(t *testing.T) {
	t.Parallel()

	// a friend's view, named her two ways: the bare name and her fleet row friend.<name>
	ta, _ := friendCardApp(t, "friend amy", "amy")
	ta.ok("tick")
	ta.startFriend("amy", 1)
	bare := ta.ok("view worker --as amy")
	assert.Contains(t, bare, "VIEW worker friend amy:")
	assert.Equal(t, bare, ta.ok("view worker --as friend.amy"), "friend.<name> is the bare name")

	// an unknown name is refused, naming both spellings
	code, _, errs := ta.do("view worker --as nobody")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "nobody is no fleet member and no friend")
	assert.Contains(t, errs, "friend.nobody")
	code, _, errs = ta.do("view worker --as friend.nobody")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "friend.nobody")

	// streams honours --max: three streams, one line and a MORE line at 1, every one at 0
	ta.ok("add --stream s2 --count 1 --one")
	ta.ok("add --stream s3 --count 1 --one")
	out := ta.ok("streams")
	assert.Equal(t, 3, strings.Count(out, "STREAM "))
	assert.NotContains(t, out, "MORE kind=stream")
	out = ta.ok("streams --max 1")
	assert.Equal(t, 1, strings.Count(out, "STREAM "))
	assert.Contains(t, out, "MORE kind=stream shown=1 total=3")
	out = ta.ok("streams --max 0")
	assert.Equal(t, 3, strings.Count(out, "STREAM "))
	assert.NotContains(t, out, "MORE kind=stream")

	// the worker view's cards list honours --max too
	ta2 := newTestApp(t)
	ta2.ok("init --readers reader-a,reader-b --members m1")
	ta2.ok("add --stream s1 --count 3")
	ta2.deal(3)
	out = ta2.ok("view worker --as m1 --max 1")
	assert.Equal(t, 1, strings.Count(out, "CARD "))
	assert.Contains(t, out, "MORE kind=card shown=1 total=3")
	out = ta2.ok("view worker --as m1 --max 0")
	assert.Equal(t, 3, strings.Count(out, "CARD "))
	assert.NotContains(t, out, "MORE kind=card")
}
