package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The ordering a cold read found still open at 91580cab: the landing is marked
// (sprint.MarkLanding pins the base), then the seat runs stream set --base, then the lander
// pushes. The pin was checked before the mark and inside it, but nothing fenced the
// interval after it, so the pass could push the old base while the brief named the new one.
// The fence is sprint.Set itself, the step that writes the base: while any card of the
// stream is landing (its landing marked), the whole base change is REFUSED naming that card,
// nothing written, and the push lands on the pinned base; the seat runs the verb again when
// the pass has ended. The pre-push re-read (basePinnedWhy) stays as a diagnostic of the fence
// and is not reached here. Driven through the beforePush seam: mark, real stream set --base,
// push. The stream's branch is sprint/s1 (the card is added on dev, the trunk, which add
// re-cuts onto the stream's branch); the re-point is to sprint/new, a branch level with the
// trunk.
func TestStreamSetBaseIsRefusedWhileACardIsLanding(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.files("the module", goModule)
	r.commit("a.txt", "seed\n", "seed a.txt")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main", "HEAD:refs/heads/dev", "HEAD:refs/heads/sprint/s1", "HEAD:refs/heads/sprint/new")
	r.git(r.worker, "fetch", "-q", "origin")
	r.stubGreenGate()
	brief := writeNeedsBrief(t, t.TempDir(), "a", "RESULT: a tier: flash\nREPO: "+r.remote+"\nBASE: dev\nPATHS: a.txt", "")
	r.ok("add --stream s1 a --one --brief-file " + brief)
	require.Contains(t, r.primary("a").F("brief"), "\nBASE: sprint/s1\n", "add re-cuts the card onto the stream's branch")
	head := r.head("a", "sprint/s1", "a.txt", "a\n")
	r.queued(map[string]string{"a": head}, "a")
	newTip := r.git(r.remote, "rev-parse", "sprint/new")

	// between the landing mark and the push: the real verb, refused by the fence
	var sets int
	var setOut, setErrs string
	var setCode int
	r.a.beforePush = func(int) {
		if sets == 0 {
			sets++
			setCode, setOut, setErrs = r.do("stream set s1 --base sprint/new")
		}
	}
	code, out, errs := r.do("land")
	require.Equal(t, 1, sets, "the seam ran once")
	assert.Equal(t, 1, setCode, "stream set --base is refused while the card is landing: %s%s", setOut, setErrs)
	assert.Contains(t, setErrs, "a of stream s1 is landing (its landing is marked at head "+head+")", setErrs)
	assert.Contains(t, setErrs, "nothing was written", setErrs)
	assert.NotContains(t, setOut, "base sprint/s1 -> sprint/new", "no card's base moved")

	assert.Equal(t, 0, code, out+errs)
	assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=sprint/s1", "the push lands on the pinned base")
	assert.NotContains(t, out+errs, "fence did not hold", "the pre-push re-read is not reached: the fence held")
	assert.Equal(t, map[string]string{"a": "landed/merged"}, r.places("a"))
	assert.Equal(t, []string{"land a (sprint stream s1)", "seed a.txt", "the module", "base"}, strings.Split(r.git(r.remote, "log", "--first-parent", "--format=%s", "sprint/s1"), "\n"))
	assert.Equal(t, newTip, r.git(r.remote, "rev-parse", "sprint/new"), "the new base is untouched")
	assert.Contains(t, r.primary("a").F("brief"), "\nBASE: sprint/s1\n", "the landed card's brief still names the base it landed on")

	// the pass has ended: the verb is taken (the landed card is not re-pointed; nothing is)
	r.a.beforePush = nil
	out = r.ok("stream set s1 --base sprint/new")
	assert.NotContains(t, out, "is landing", out)
	r.clean()
}
