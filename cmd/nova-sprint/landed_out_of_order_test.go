package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// outOfOrderNotes is the store's happened notes of the out-of-order landing.
func outOfOrderNotes(t *testing.T, a *app) []sprint.Note {
	t.Helper()
	st, err := a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	notes, _, err := st.B.NotesSince(context.Background(), "", 10000)
	require.NoError(t, err)
	var out []sprint.Note
	for _, n := range notes {
		if n.Type == sprint.NLandedOutOfOrder {
			out = append(out, n)
		}
	}
	return out
}

// `landed --sha` records a push that is a fact whatever its needs (tla/WaitFor.tla
// Report, PushedIsRecorded; docs/SPEC-SPRINT.md section 7): git says the card's head is
// an ancestor of --sha and --sha is on origin/<base>, so the card is recorded landed, and
// one happened note "landed out of order" names the LAND-AFTER target it landed before.
func TestTheLandedVerbRecordsAPushOutOfOrder(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	dir := t.TempDir()
	r.ok("add --stream s1 --one --brief-file " + writeSoftHeaderBrief(t, dir, "a", "", "", r.remote, "", "a.txt"))
	r.ok("add --stream s1 --one --brief-file " + writeSoftHeaderBrief(t, dir, "b", "", "a", r.remote, "", "b.txt"))
	heads := map[string]string{"a": r.head("a", "main", "a.txt", "a\n"), "b": r.head("b", "main", "b.txt", "b\n")}
	r.git(r.worker, "push", "-q", "origin", "refs/heads/sprint/*:refs/heads/sprint/*")
	r.deal(2)
	r.ok("take --as m1 --limit 100")
	r.ok("finish --as m1 a.w1@1 --head " + heads["a"])
	r.ok("finish --as m1 b.w1@1 --head " + heads["b"])
	r.ok("ask")
	r.ok("read --as reader-a --ok --limit 100")
	r.ok("read --as reader-b --ok --limit 100")
	r.ok("accept --read-ok")
	require.Equal(t, map[string]string{"a": "merging/queued", "b": "merging/queued"}, r.places("a", "b"))

	// the push that was never reported: b's head merged onto origin's main, a not landed
	r.git(r.worker, "fetch", "-q", "origin")
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.git(r.worker, "merge", "-q", "--no-ff", "-m", "land b", heads["b"])
	sha := r.git(r.worker, "rev-parse", "HEAD")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")

	out := r.ok("landed b --sha " + sha + " --reason pushed-never-reported --repo-dir " + r.clone + " --base main")
	assert.Contains(t, out, "b merging -> landed")
	assert.Equal(t, map[string]string{"a": "merging/queued", "b": "landed/merged"}, r.places("a", "b"))
	got := outOfOrderNotes(t, r.a)
	require.Len(t, got, 1, "one happened note for the out-of-order landing")
	assert.Equal(t, []string{"b"}, got[0].Primaries)
	assert.Contains(t, got[0].What, "b")
	assert.Contains(t, got[0].What, "LAND-AFTER target a")
	r.clean()
}
