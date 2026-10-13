//go:build functional

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// --- the refused tick step that poisons the drain (2026-10-11 04:17Z and 16:35Z) ---

// TestStallAnOverBoundMergeNoteNeverPoisonsTheDrain is the 16:35Z freeze on a real store
// (PR #91): a lander's note over the card text bound was written into the card's why, the
// store refused the tick's all-or-none drain step every tick, and no finish moved for twenty
// minutes. A note over the bound is refused at the merge verb, nothing written; a note of the
// size that froze the sprint (40 KiB, under the 64 KiB bound) is taken; and every tick after
// both drains another member's finish.
func TestStallAnOverBoundMergeNoteNeverPoisonsTheDrain(t *testing.T) {
	t.Parallel()
	r := newStallRig(t)
	r.do("init", "--readers", "reader-a,reader-b", "--members", "m1:4")
	declareStallFlashRoute(t, r)
	r.do("set", "--read-cards", "off") // this stall fixture consumes standalone legacy readers
	r.do("add", "--stream", "s1", "--count", "3")
	r.beat("m1")
	r.do("start")
	r.tick() // presence: the members up
	r.tick() // the deal
	packets := r.take("m1")
	require.Len(t, packets, 3, "the three cards are dealt and taken")
	byID := map[string]sprint.Packet{}
	for _, p := range packets {
		byID[p.Primary] = p
	}
	finishStallFunded(r, "m1", byID["s1-1"], stallHead("s1-1"))
	r.tick()
	readStallAll(r, "reader-a", "reader-b")
	for i := 0; i < 3 && r.col("s1-1") != "merging"; i++ {
		r.tick()
		readStallAll(r, "reader-a", "reader-b")
	}
	require.Equal(t, "merging", r.col("s1-1"), "s1-1 is queued to merge")

	line := "tla/RUNS.tsv:3 has no current run record for the case MCX.cfg (E4); "
	over := "the head of s1-1 fails the lander's checks: " + strings.Repeat(line, 1200) // ~85 KiB
	code, out, errs := r.run("merge", "--stream", "s1", "--conflict", "s1-1", "--note", over)
	assert.NotEqual(t, 0, code, "a note over the bound is refused at the verb: %s%s", out, errs)
	assert.Contains(t, out+errs, "over the bound", "the refusal says why")
	r.tick()
	assert.Equal(t, "merging", r.col("s1-1"), "a refused note writes nothing")

	froze := "the head of s1-1 fails the lander's checks: " + strings.Repeat(line, 560) // ~40 KiB, the note that froze the sprint
	r.do("merge", "--stream", "s1", "--conflict", "s1-1", "--note", froze)

	// another finish: every tick after drains it
	finishStallFunded(r, "m1", byID["s1-2"], stallHead("s1-2"))
	r.tick()
	r.tick()
	assert.Equal(t, "review", r.col("s1-2"), "the finish drained: the sprint is not frozen")
	finishStallFunded(r, "m1", byID["s1-3"], stallHead("s1-3"))
	r.tick()
	assert.Equal(t, "review", r.col("s1-3"), "and the next one")
}

// TestStallAnOverBoundBriefNeverPoisonsTheDrain is the 04:17Z freeze on a real store
// (brief-file-over-the-bound-is-refused): a brief of 17,408 bytes, over the 16 KiB bound,
// printed BRIEF OK on a running machine, its change was queued, and the store refused every
// tick's drain for 37 minutes. Now the verb refuses it, exit 2, nothing queued, and the next
// tick drains a member's finish.
func TestStallAnOverBoundBriefNeverPoisonsTheDrain(t *testing.T) {
	t.Parallel()
	r := newStallRig(t)
	r.do("init", "--readers", "reader-a,reader-b", "--members", "m1:4")
	declareStallFlashRoute(t, r)
	r.do("set", "--read-cards", "off") // this stall fixture consumes standalone legacy readers
	r.do("add", "--stream", "s1", "--count", "3")
	r.beat("m1")
	r.do("start")
	r.tick() // presence: the members up
	r.tick() // the deal
	packets := r.take("m1")
	require.Len(t, packets, 3)
	byID := map[string]sprint.Packet{}
	for _, p := range packets {
		byID[p.Primary] = p
	}
	finishStallFunded(r, "m1", byID["s1-1"], stallHead("s1-1"))
	r.tick()
	require.Equal(t, "review", r.col("s1-1"))

	// a brief that passes the card lint, 17,408 bytes long: the card's rules whole, then its prose
	brief := passingBrief("s1-1: the card, briefed again\n")
	pad := strings.Repeat("The case is the one the owner named; the finding is in the record.\n", 300)
	brief += "\n\n" + pad[:17408-len(brief)-2]
	require.Len(t, brief, 17408)
	path := filepath.Join(t.TempDir(), "s1-1.md")
	require.NoError(t, os.WriteFile(path, []byte(brief), 0o600))
	code, out, errs := r.run("brief", "s1-1", "--brief-file", path)
	assert.Equal(t, 2, code, "a brief over the bound is refused at the verb, exit 2: %s%s", out, errs)
	assert.Contains(t, out+errs, "over the", "the refusal names the bound")

	// the machine goes on: a finish drains at the next tick
	finishStallFunded(r, "m1", byID["s1-2"], stallHead("s1-2"))
	r.tick()
	r.tick()
	assert.Equal(t, "review", r.col("s1-2"), "the finish drained: the sprint is not frozen")
	finishStallFunded(r, "m1", byID["s1-3"], stallHead("s1-3"))
	r.tick()
	assert.Equal(t, "review", r.col("s1-3"))
}

// These successful finishes incur the fixture's declared flash route. Keep
// their usage explicit so the fund check remains real.
func finishStallFunded(r *stallRig, who string, p sprint.Packet, head string) {
	r.t.Helper()
	r.cliOK(who, "finish", "--as", who, p.Card+"@"+strconv.Itoa(p.Gen), "--epoch", "0",
		"--head", head, "--report", "done", "--usage",
		"input=100 cache_read=0 cache_write=0 output=10 model=functional-fixture/fixture")
}

func readStallAll(r *stallRig, readers ...string) {
	r.t.Helper()
	for _, rd := range readers {
		r.cli(rd, "queue", "--as", rd)
		r.cli(rd, "read", "--as", rd, "--ok", "--max", "100", "--epoch", "0", "--usage",
			"input=100 cache_read=0 cache_write=0 output=10 model=functional-fixture/fixture")
	}
}
