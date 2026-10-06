package sprint_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The dev sync on the twin store and a twin repository (docs/SPEC-SPRINT.md, "Dev sync
// every cycle"): a bare origin with main, dev and the base; a developer's clone that
// commits to dev and the base; and the land round's own clone, where the sync runs.

func TestTheBaseTakesTheDevelopmentBranchEveryCycle(t *testing.T) {
	t.Parallel()
	r := newSyncRig(t)
	defer r.Close()
	repo := r.repo

	// dev moves ahead of the base by two commits; the first cycle (no sync recorded) merges
	// it through the tree gate and pushes the merge onto the base, like a batch
	repo.commit("dev", "feature.go", "package f // one\n", "dev commit 1")
	repo.commit("dev", "other.go", "package f\n", "dev commit 2")
	devTip := repo.tip("dev")
	f, due, err := r.cycle()
	require.NoError(t, err)
	require.True(t, due, "no dev sync recorded: the cycle syncs")
	assert.True(t, f.Synced)
	assert.Equal(t, 2, f.Drift.BaseLacks)
	assert.Equal(t, []string{"package f // one\n"}, r.gate, "the merged tree went through the tree gate")
	assert.True(t, repo.has(syncBase, devTip), "the base on origin holds dev")
	assert.Equal(t, f.MergeSha, repo.tip(syncBase))
	s := r.load()
	d := sprint.DevDriftOf(s)
	assert.Equal(t, 0, d.BaseLacks)
	assert.Equal(t, f.MergeSha, d.LastSha)
	assert.Equal(t, "0", s.StreamCtl("s1").F(sprint.FieldDevSyncBaseLacks), "the drift is on the merge row")
	assert.True(t, sprint.CanLand(s, "s1"))

	// the next cycle at the same clock, nothing landed: not due, no git
	repo.commit("dev", "feature.go", "package f // two\n", "dev commit 3")
	_, due, err = r.cycle()
	require.NoError(t, err)
	assert.False(t, due)
	assert.Equal(t, f.MergeSha, repo.tip(syncBase))

	// a red tree gate pushes nothing and records nothing
	r.tick(sprint.DevSyncAge)
	r.red = errors.New("vet: red")
	_, due, err = r.cycle()
	require.ErrorContains(t, err, "vet: red")
	assert.True(t, due)
	assert.Equal(t, f.MergeSha, repo.tip(syncBase), "a red gate pushes nothing")
	assert.Empty(t, repo.git(repo.land, "status", "--porcelain"), "the clone is clean")
	r.red = nil

	// the base and dev both change one file: a conflict stops every stream with ONE
	// judgment naming the file, and pushes nothing
	repo.commit(syncBase, "feature.go", "package f // base\n", "base edit")
	baseTip := repo.tip(syncBase)
	f, due, err = r.cycle()
	require.NoError(t, err)
	require.True(t, due)
	assert.True(t, f.Conflict)
	assert.Equal(t, []string{"feature.go"}, f.Files)
	assert.Equal(t, baseTip, repo.tip(syncBase))
	s = r.load()
	for _, st := range []string{"s1", "s2"} {
		assert.Equal(t, sprint.StreamStopped, s.StreamCtl(st).F("state"), st)
		assert.Equal(t, sprint.DevSyncCause, s.StreamCtl(st).F("cause"), st)
		assert.False(t, sprint.CanLand(s, st), st)
	}
	js := r.open()
	require.Len(t, js, 1, "ONE judgment")
	assert.Contains(t, js[0].Note.What, "feature.go")

	// every cycle tries again while it is open: still one judgment
	r.tick(time.Minute)
	_, due, err = r.cycle()
	require.NoError(t, err)
	require.True(t, due, "an open conflict makes every cycle due")
	assert.Len(t, r.open(), 1)

	// merged by hand: the next cycle finds the base holds dev, closes the judgment and
	// resumes both streams
	repo.git(repo.dev, "fetch", "-q", "origin")
	repo.git(repo.dev, "checkout", "-q", "-B", syncBase, "origin/"+syncBase)
	cmd := repo.cmd(repo.dev, "merge", "-q", "origin/dev")
	require.Error(t, cmd.Run(), "the hand merge conflicts as the cycle's did")
	require.NoError(t, os.WriteFile(filepath.Join(repo.dev, "feature.go"), []byte("package f // both\n"), 0o644))
	repo.git(repo.dev, "commit", "-q", "-am", "merge dev by hand")
	repo.git(repo.dev, "push", "-q", "origin", "HEAD:refs/heads/"+syncBase)
	r.tick(time.Minute)
	f, due, err = r.cycle()
	require.NoError(t, err)
	require.True(t, due)
	assert.False(t, f.Conflict)
	assert.Empty(t, r.open(), "the judgment is closed")
	s = r.load()
	for _, st := range []string{"s1", "s2"} {
		assert.Equal(t, sprint.StreamMerging, s.StreamCtl(st).F("state"), st)
		assert.True(t, sprint.CanLand(s, st), st)
	}
	assert.Equal(t, repo.tip(syncBase), sprint.DevDriftOf(s).LastSha)
}
