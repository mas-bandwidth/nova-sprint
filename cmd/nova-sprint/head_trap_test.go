package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The head trap: a finish without --head records the card's id as its head,
// which land cannot merge. The worker's packet names --head <commit> on its
// report line, so the command it pastes names the commit.
func TestThePacketsReportLineNamesTheHead(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
	out := ta.ok("take --as m1 s1-1.w1@1")
	assert.Contains(t, out, "  report it: nova-sprint finish --as m1 s1-1.w1@1 --epoch 0 --branch sprint/s1-1.w1.g1.e0 --head <commit> --report '<what you did>' [--failed]")
	ta.clean()
}

// landResult is land's --json, whatever its exit.
type landResult struct {
	Status string      `json:"status"`
	Items  []landBatch `json:"items"`
}

// landJSON runs a land line with --json and returns its exit and its result.
func (r *landRig) landJSON(line string) (int, landResult) {
	r.t.Helper()
	code, out, errs := r.do(line + " --json")
	var v landResult
	require.NoError(r.t, json.Unmarshal([]byte(out), &v), "%s: %s%s", line, out, errs)
	return code, v
}

// queuedOneWithoutHead takes s1-1 to the merge queue at a pushed commit and
// s1-2 behind it finished with no --head, its head the card's id.
func (r *landRig) queuedOneWithoutHead() {
	r.t.Helper()
	r.ok("add --stream s1 --count 2")
	head := r.head("s1-1", "main", "a.txt", "a\n")
	r.git(r.worker, "push", "-q", "origin", "refs/heads/sprint/*:refs/heads/sprint/*")
	r.deal(2)
	r.ok("take --as m1 --limit 100")
	r.ok("finish --as m1 s1-1.w1@1 --head " + head)
	r.ok("finish --as m1 s1-2.w1@1")
	r.ok("ask")
	r.ok("read --as reader-a --ok --limit 100")
	r.ok("read --as reader-b --ok --limit 100")
	r.ok("accept --read-ok")
	r.markProtected()
}

// land --dry-run ejects a head that is not a commit id exactly as land does: the
// cards before it a batch that lands, that card named as ejected with the same words,
// and nothing recorded by the dry run; then land itself ejects it in those words, and
// the stream goes on.
func TestLandDryRunEjectsAHeadThatIsNotACommitAsLandDoes(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.queuedOneWithoutHead()
	applies := r.applies()
	land := "land --repo-dir " + r.clone + " --base main"

	code, dry := r.landJSON(land + " --dry-run")
	assert.Equal(t, 0, code)
	assert.Equal(t, "ok", dry.Status)
	require.Len(t, dry.Items, 1)
	assert.Equal(t, "ok", dry.Items[0].Status)
	assert.Equal(t, []string{"s1-1"}, dry.Items[0].IDs)
	require.Len(t, dry.Items[0].Ejected, 1)
	assert.Contains(t, dry.Items[0].Ejected[0], "s1-2: the head s1-2.w1 of s1-2 is not a commit id")
	assert.Contains(t, dry.Items[0].Ejected[0], "--head <commit>")
	assert.Empty(t, dry.Items[0].Fact, "a dry run records no fact")

	code, out, _ := r.do(land + " --dry-run")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "LAND OK stream=s1 cards=1 base=main tip=- ids=s1-1")
	assert.Contains(t, out, "ejected=s1-2 dry_run=yes")
	assert.Equal(t, applies, r.applies(), "a dry run wrote")
	assert.Equal(t, map[string]string{"s1-1": "merging/queued", "s1-2": "merging/queued"}, r.places("s1-1", "s1-2"))

	code, real := r.landJSON(land)
	assert.Equal(t, 0, code)
	require.Len(t, real.Items, 1)
	assert.Equal(t, "ok", real.Items[0].Status)
	assert.Equal(t, dry.Items[0].Ejected, real.Items[0].Ejected, "the dry run's words are land's")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "review/returned"}, r.places("s1-1", "s1-2"))
	assert.NotContains(t, r.streamState("s1"), "stopped")
}

// A batch land cannot place is refused naming every problem at once, the dry
// run and land alike: no BASE:, no REPO:, and the head that is not a commit
// id it would meet next, with one land line that supplies both flags.
func TestLandNamesEveryProblemOfABatchAtOnce(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.queuedOneWithoutHead()
	applies := r.applies()
	for _, line := range []string{"land --dry-run", "land"} {
		code, _, errs := r.do(line)
		assert.Equal(t, 1, code, line)
		require.Contains(t, errs, "reason=", line)
		reason := errs[strings.Index(errs, "reason="):]
		for _, want := range []string{
			"card s1-1 names no BASE: line and no --base was given, and the card names no REPO: line and no --repo-dir was given; run: nova-sprint land --stream s1 --base <branch> --repo-dir <clone>",
			"the head s1-2.w1 of s1-2 is not a commit id",
		} {
			assert.Contains(t, reason, want, line)
		}
	}
	assert.Equal(t, applies, r.applies(), "a refusal before any git wrote")
}
