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

// TestReaderAddPrefixesABareMemberName: reader add, reader set and reader
// up given a bare fleet member name add the reader- prefix themselves and
// say so; READER-ADD prints member-reader=<yes|no> for each row, and
// reader add writes the row up (no reader up needed after it).
func TestReaderAddPrefixesABareMemberName(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --members m1,m2")
	// init already made the member readers; take one off and add it back
	// by its bare member name: the verb prefixes it itself.
	ta.ok("reader remove reader-m1")
	out := ta.ok("reader add m1")
	assert.Contains(t, out, "reader-m1", "a bare member name is prefixed: %s", out)
	assert.Contains(t, out, "member-reader=yes", "the row is the member's reader: %s", out)
	assert.NotContains(t, out, "readers=m1", "the bare name is not stored: %s", out)

	out = ta.ok("reader add reader-x")
	assert.Contains(t, out, "member-reader=no", "a plain reader is no member reader: %s", out)

	// reader set and reader up take the bare name too.
	ta.ok("reader set m1 --tiers pro")
	out = ta.ok("reader up m1")
	assert.Contains(t, out, "reader-m1", "reader up prefixes: %s", out)

	// the row reader add wrote is up with no reader up after it: its state
	// reads up at the store.
	st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(t, err)
	rows, err := st.ReaderRows(context.Background())
	require.NoError(t, err)
	assert.Contains(t, rows, "reader-m1")
	assert.NotContains(t, rows, "m1")
	states, err := st.ReaderStates(context.Background(), []string{"reader-m1"}, ta.now)
	require.NoError(t, err)
	assert.Equal(t, "up", states["reader-m1"], "reader add writes the row up")
}

// TestReleaseAcceptAClearedSprintReadsWithMembersOnly is a case of the
// release acceptance build gate (docs/TESTS.md, "Release acceptance"), on
// the rig of TestReleaseAcceptAClearedSprintLandsWithNoSeatAction: init
// with members only (no --readers), clear, add, start; a card reaches
// review and its reads are dealt to member readers with no seat verb, and
// tick --shadow's read units list every up member.
func TestReleaseAcceptAClearedSprintReadsWithMembersOnly(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.ok("reader remove reader-a")
	r.ok("reader remove reader-b")
	r.ok("init --members m1,m2")
	r.ok("clear --confirm sprint")
	r.ok("add --stream s1 --count 2")
	heads := map[string]string{
		"s1-1": r.head("s1-1", "main", "a.txt", "a\n"),
		"s1-2": r.head("s1-2", "main", "b.txt", "b\n"),
	}
	r.git(r.worker, "push", "-q", "origin", "refs/heads/sprint/*:refs/heads/sprint/*")
	r.markProtected()
	r.ok("start")

	// the shadow tick's read units list every up member.
	shadow := r.ok("tick --shadow")
	assert.Contains(t, shadow, "units:")
	for _, m := range []string{"m1", "m2"} {
		assert.Contains(t, shadow, m, "shadow units list %s", m)
	}

	more := []string{"--repo-dir", r.clone, "--base", "main"}
	landed := func() bool {
		for _, id := range []string{"s1-1", "s1-2"} {
			var v cardView
			r.json("card "+id, &v)
			if v.Primary.Col != "landed" {
				return false
			}
		}
		return true
	}
	rounds := 0
	for ; rounds < 12 && !landed(); rounds++ {
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
				r.ok("finish --as " + m + " " + card + " --head " + heads[p.Primary])
			}
		}
		r.ok("tick")
		r.a.landOnce(context.Background(), "mem:0", more, &bytes.Buffer{})
	}
	require.True(t, landed(), "the card landed with members only within %d rounds", rounds)
	r.clean()
}
