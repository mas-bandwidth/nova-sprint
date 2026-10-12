package main

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoveryHeadBindingBatchesExactRefsAndDistinguishesFailure(t *testing.T) {
	t.Parallel()
	requests := []sprint.RecoveryHeadRequest{{Card: "a", Gen: 1, CurrentGen: 2, Repo: "repo", Branch: "a.g1"}, {Card: "b", Gen: 3, CurrentGen: 4, Repo: "repo", Branch: "b.g3"}}
	calls := 0
	a := &app{recoveryTips: func(_ context.Context, repo string, branches []string) (map[string]string, error) {
		calls++
		assert.Equal(t, "repo", repo)
		assert.Equal(t, []string{"a.g1", "b.g3"}, branches)
		return map[string]string{"a.g1": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, nil
	}}
	out := a.recoveryHeads(context.Background(), requests)
	require.Len(t, out, 2)
	assert.Equal(t, 1, calls)
	assert.Equal(t, requests[0], out[0].RecoveryHeadRequest)
	assert.NotEmpty(t, out[0].Head)
	assert.Empty(t, out[1].Head, "only a successful listing establishes absent branch")
	assert.Empty(t, out[1].Error)
	a.recoveryTips = func(context.Context, string, []string) (map[string]string, error) {
		return nil, errors.New("unreachable")
	}
	for _, f := range a.recoveryHeads(context.Background(), requests) {
		assert.Empty(t, f.Head)
		assert.Equal(t, "unreachable", f.Error)
	}
}

func TestRecoveryHeadBindingRespectsBudgetAndBatchCap(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	a := &app{recoveryTips: func(context.Context, string, []string) (map[string]string, error) { calls++; return nil, nil }}
	requests := make([]sprint.RecoveryHeadRequest, sprint.RecoveryHeadBatch+1)
	for i := range requests {
		requests[i].Repo = "repo"
	}
	out := a.recoveryHeads(ctx, requests)
	assert.Zero(t, calls, "expired shared deadline causes no additional remote queries")
	require.Len(t, out, sprint.RecoveryHeadBatch)
	for _, f := range out {
		assert.NotEmpty(t, f.Error)
	}
}

func TestRecoveryHeadBindingReadsRealIsolatedGitWithoutChangingSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("-c", "user.name=Recovery Test", "-c", "user.email=recovery-test@example.invalid", "commit", "-q", "--allow-empty", "-m", "isolated head")
	head := git("rev-parse", "HEAD")
	git("branch", "old.g1")
	a := &app{}
	a.recoveryTips = a.recoveryBranchTips
	requests := []sprint.RecoveryHeadRequest{{Card: "a", Gen: 1, CurrentGen: 2, Repo: dir, Branch: "old.g1"}, {Card: "b", Gen: 1, CurrentGen: 2, Repo: dir, Branch: "absent.g1"}}
	out := a.recoveryHeads(context.Background(), requests)
	require.Len(t, out, 2)
	assert.Equal(t, head, out[0].Head)
	assert.Empty(t, out[0].Error)
	assert.Empty(t, out[1].Head)
	assert.Empty(t, out[1].Error)
	assert.Equal(t, head, git("rev-parse", "refs/heads/old.g1"), "recovery never deletes or mutates the source branch")
}
