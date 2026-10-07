//go:build functional

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The lander half of the sprint-branch rule: a real git rig, so it builds only under the
// functional tag.
func TestTheLanderLandsOnlyOnTheSprintBranchAndPromotionReachesDev(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	for _, b := range []string{"dev", "sprint/s1"} {
		r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/"+b)
	}
	r.git(r.worker, "fetch", "-q", "origin") // r.head starts the card from refs/remotes/origin/sprint/s1
	r.ok("add --stream s1 --count 1 --one")
	r.queued(map[string]string{"s1-1": r.head("s1-1", "sprint/s1", "a.txt", "a\n")}, "s1-1")
	r.ok("stream set s1 --land-protected default") // queued marks every stream; s1 is an ordinary stream again
	dev := r.git(r.remote, "rev-parse", "dev")
	for _, land := range []string{"land --repo-dir " + r.clone + " --base dev --dry-run", "land --repo-dir " + r.clone + " --base dev"} {
		code, out, errs := r.do(land)
		assert.Equal(t, 1, code, land)
		assert.Contains(t, out+errs, "LAND REFUSED stream=s1 cards=1 base=dev tip=- ids=s1-1 ", land)
		assert.Contains(t, out+errs, "card s1-1 lands on dev, a protected branch of its repository (it names no REPO: line), and stream s1 is not marked to land on it", land)
		assert.Contains(t, out+errs, "every stream lands on the sprint branch, and promotion alone reaches dev; re-cut the card with BASE: <the sprint branch>", land)
		assert.Contains(t, out+errs, "; run: nova-sprint stream set s1 --land-protected any\n", land)
	}
	assert.Equal(t, dev, r.git(r.remote, "rev-parse", "dev"), "nothing was pushed to dev")
	assert.Equal(t, map[string]string{"s1-1": "merging/queued"}, r.places("s1-1"), "nothing was recorded")

	assert.Contains(t, r.ok("land --repo-dir "+r.clone+" --base sprint/s1"), "LAND OK stream=s1 cards=1 base=sprint/s1", "the sprint branch is where a stream lands")
	assert.Contains(t, r.git(r.remote, "log", "--first-parent", "--format=%s", "sprint/s1"), "land s1-1 (sprint stream s1)")
	assert.Equal(t, dev, r.git(r.remote, "rev-parse", "dev"), "dev moved only by promotion")
}
