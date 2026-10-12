package main

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A card re-pointed while its batch builds is never pushed onto the old base. stream set
// --base reaches a merging card (the set-base reach fix), so the seat can move a stream's
// base while the lander, which read the card's BASE: at the start of the pass, is gating
// the batch it cut onto that base. The landing pins the base with the head and attempt
// (sprint.PushedPin, headWhy, sprint.MarkLanding): a card whose BASE: differs from the
// pinned one at the push is refused, nothing pushed, the card left queued, and the next
// pass lands it on its new base. Before the fix the pass pushed sprint/old and reported
// LAND OK (a cold reader's probe, 2026-10-12).
func TestACardRepointedDuringTheGateIsNotPushedOntoTheOldBase(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	// a Go module at the base: a clone with no go.mod has no tree gate, and the gate seam
	// is where the re-point is injected
	r.files("the module", goModule)
	r.commit("a.txt", "seed\n", "seed a.txt")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main", "HEAD:refs/heads/sprint/old", "HEAD:refs/heads/sprint/new")
	r.git(r.worker, "fetch", "-q", "origin")
	brief := writeNeedsBrief(t, t.TempDir(), "a", "RESULT: a tier: flash\nREPO: "+r.remote+"\nBASE: sprint/old\nPATHS: a.txt", "")
	r.ok("add --stream s1 a --one --brief-file " + brief)
	head := r.head("a", "sprint/old", "a.txt", "a\n")
	r.queued(map[string]string{"a": head}, "a")
	oldTip, newTip := r.git(r.remote, "rev-parse", "sprint/old"), r.git(r.remote, "rev-parse", "sprint/new")

	// the re-point lands while the batch's tree gate runs: the lander read sprint/old
	// before it, and the gate seam is where the batch is between its cut and its push
	sets := 0
	r.a.landState().gateBench = func(context.Context, string, string, [][]string, bool) (string, int, error) {
		if sets == 0 {
			sets++
			out := r.ok("stream set s1 --base sprint/new")
			assert.Contains(t, out, "a base sprint/old -> sprint/new (merging)", out)
		}
		return "", 0, nil
	}
	code, out, errs := r.do("land")
	assert.Equal(t, 1, code, out+errs)
	assert.Contains(t, errs, "LAND REFUSED stream=s1")
	assert.Contains(t, errs, "a was re-pointed to BASE sprint/new during the gate, not sprint/old as it was read and built")
	assert.NotContains(t, out, "LAND OK")
	assert.Equal(t, oldTip, r.git(r.remote, "rev-parse", "sprint/old"), "nothing was pushed onto the old base")
	assert.Equal(t, newTip, r.git(r.remote, "rev-parse", "sprint/new"), "nothing was pushed onto the new base either")
	assert.Equal(t, map[string]string{"a": "merging/queued"}, r.places("a"), "the card stays queued")
	assert.Equal(t, 1, sets, "the gate ran once")

	// the next pass lands it on its new base
	code, out, errs = r.do("land")
	assert.Equal(t, 0, code, out+errs)
	assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=sprint/new")
	assert.Equal(t, map[string]string{"a": "landed/merged"}, r.places("a"))
	assert.Equal(t, oldTip, r.git(r.remote, "rev-parse", "sprint/old"), "the old base is untouched")
	assert.Equal(t, []string{"land a (sprint stream s1)", "seed a.txt", "the module", "base"}, strings.Split(r.git(r.remote, "log", "--first-parent", "--format=%s", "sprint/new"), "\n"))
	r.clean()
}
