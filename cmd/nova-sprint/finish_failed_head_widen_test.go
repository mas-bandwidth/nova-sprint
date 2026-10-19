//go:build functional

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// brief --widen runs on the head a failed finish recorded (card
// v17-failed-finish-records-its-head; docs/SPEC-SPRINT.md section 2, "recut-widen-r.w1"):
// a HOLD that finished `--failed --head <sha>` widens in place and its next attempt
// starts from that sha (recut_widen.go reads last.F("head")), while a HOLD that finished
// `--failed` with no --head records the card id default, which is no full commit SHA, so
// the same widen is refused naming it and writes nothing. The stores and remotes are
// local temporary directories (newLandRig): no network and no outside service, but the
// rig runs git, so this is a functional test (internal/ci unit_rule_test.go).
func TestAFailedFinishRecordsItsHeadForWiden(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.commit("a.go", "package a\n", "a.go at the base")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")

	// a failed finish that pushed: widen starts the next attempt from its head
	{
		id, head := r.heldCard("s1", "HOLD: PATHS too narrow; PATHS-PROPOSED: b.go")
		out := r.ok("brief " + id + " --widen --repo-dir " + r.clone)
		assert.Contains(t, out, "NEXT "+id+" attempt 2 starts from attempt 1 head="+head)
		assert.Contains(t, r.ok("card "+id+" --brief"), "\nCARRY: "+id+" attempt 1 head="+head+"\n")
		assert.Equal(t, sprint.Ready, r.primary(id).Col)
	}

	// a failed finish with no --head keeps the card id default, and widen refuses it
	{
		id := "s2-1"
		brief := writeBrief(t, "fix the empty case, tier: pro\nREPO: "+r.remote+"\nBASE: main\nPATHS: a.go\nTEST: none a fixture of brief --widen")
		r.promotionStream("s2")
		r.ok("add --stream s2 --count 1 --one --brief-file " + brief)
		r.deal(1)
		r.ok("take --as m1 " + id + ".w1@1")
		r.ok("finish --as m1 " + id + ".w1@1 --failed --report 'HOLD: PATHS too narrow; PATHS-PROPOSED: b.go'")
		before := r.applies()
		code, out, errs := r.do("brief " + id + " --widen --repo-dir " + r.clone)
		assert.Equal(t, 1, code, out+errs)
		assert.Contains(t, errs, id+" attempt 1 pushed no head to start its next attempt from")
		assert.Equal(t, before, r.applies(), "nothing was written")
		assert.NotContains(t, r.ok("card "+id+" --brief"), "CARRY:")
	}
}
