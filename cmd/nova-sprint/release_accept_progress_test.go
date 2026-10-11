package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReleaseAcceptAClearedSprintLandsWithNoSeatAction is the progress check of the release
// acceptance set (docs/TESTS.md, "Release acceptance"): "defaults are the working machine"
// and "automatic progress is a build gate". A sprint is cleared, so every setting is the
// default; its cards are added and the machine started; from then on no seat verb runs.
// The machine's tick deals, asks and accepts, the members take and finish, the readers read,
// and the machine's land pass lands every card on origin.
func TestReleaseAcceptAClearedSprintLandsWithNoSeatAction(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.ok("clear --confirm sprint")
	// a clear leaves the new epoch's readers table blank (the members are carried): the
	// re-add is the seat's setup, before the start, as after any clear
	r.ok("reader add reader-a")
	r.ok("reader add reader-b")
	r.ok("add --stream s1 --count 2")
	heads := map[string]string{
		"s1-1": r.head("s1-1", "main", "a.txt", "a\n"),
		"s1-2": r.head("s1-2", "main", "b.txt", "b\n"),
	}
	r.git(r.worker, "push", "-q", "origin", "refs/heads/sprint/*:refs/heads/sprint/*")
	r.markProtected() // the rig's origin has one branch, main: the stream lands there
	r.ok("start")

	// from here: the machine (tick, land), the members (take, finish) and the readers (read)
	more := []string{"--repo-dir", r.clone, "--base", "main"}
	cols := func() map[string]string {
		out := map[string]string{}
		for _, id := range []string{"s1-1", "s1-2"} {
			var v cardView
			r.json("card "+id, &v)
			out[id] = v.Primary.Col
		}
		return out
	}
	done := func() bool {
		c := cols()
		return c["s1-1"] == "landed" && c["s1-2"] == "landed"
	}
	rounds := 0
	for ; rounds < 12 && !done(); rounds++ {
		r.ok("tick")
		for _, m := range []string{"m1", "m2"} {
			code, out, _ := r.do("take --as " + m + " --limit 100 --json")
			if code != 0 {
				continue
			}
			var take output
			require.NoError(t, json.Unmarshal([]byte(out), &take), out)
			for _, p := range take.Packets {
				card := p.Card + "@" + strconv.Itoa(p.Gen)
				if p.Kind == "read" {
					r.ok("read --as " + m + " --ok " + card)
					continue
				}
				head, ok := heads[p.Primary]
				require.True(t, ok, "a packet for a card the test did not add: %+v", p)
				r.ok("finish --as " + m + " " + card + " --head " + head)
			}
		}
		for _, reader := range []string{"reader-a", "reader-b"} {
			r.do("read --as " + reader + " --ok --max 100") // nothing asked of a reader is no failure
		}
		r.ok("tick")
		r.a.landOnce(context.Background(), "mem:0", more, &bytes.Buffer{})
	}
	require.True(t, done(), "every card landed with no seat action within %d rounds: %v", rounds, cols())
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "landed/merged"}, r.places("s1-1", "s1-2"))
	assert.Contains(t, r.git(r.remote, "show", "main:a.txt"), "a")
	assert.Contains(t, r.git(r.remote, "show", "main:b.txt"), "b")
	r.clean()
}
