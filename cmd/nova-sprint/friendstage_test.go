package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A friend's dealt card is staged by a verb (docs/FRIENDS.md), never by hand: a
// friend stage <card>@<gen> --as friend.<name> clones the card's REPO at its
// delivered branch, sets the friend's own git identity in the clone's local
// config (never the machine's global one), writes JOB.md naming the finish
// command, and refuses a card not on the friend's row or a friend with no
// git_identity on record. The fake git records every argv and every config
// write, so the test asserts the clone's local user.name and user.email are the
// friend's.
func TestAFriendCardIsStagedByAVerb(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t, "amy", "bob")

	// amy is on record with a git identity; bob has none
	_, err := cfg.Delete(context.Background(), config.KindFriend, "amy", "t")
	require.NoError(t, err)
	_, err = cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: "amy", Fields: map[string]string{"tiers": "flash", "git_identity": "Amy Author <amy@example.com>"}}, "t")
	require.NoError(t, err)

	ta.a.tip = tipIs(t, landHead)
	root := t.TempDir()
	ta.ok("friend sync --root " + root)
	ta.beatUp("amy")
	ta.beatUp("bob")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-1.md"), []byte(passingBrief("s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nWHO: friend amy")), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-2.md"), []byte(passingBrief("s1-2: a second friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nWHO: friend bob")), 0o644))
	ta.ok("add --stream s1 --brief-dir " + dir)
	ta.ok("start")
	ta.ok("tick")

	var calls []struct {
		dir  string
		args []string
	}
	prev := friendStageGit
	friendStageGit = func(_ context.Context, dir string, args ...string) (string, error) {
		calls = append(calls, struct {
			dir  string
			args []string
		}{dir, args})
		if args[0] == "clone" {
			require.NoError(t, os.MkdirAll(args[len(args)-1], 0o755))
		}
		return "", nil
	}
	defer func() { friendStageGit = prev }()

	jobs := filepath.Join(root, "amy-working", "jobs")
	out := ta.ok(fmt.Sprintf("friend stage s1-1.w1@1 --as friend.amy --dir %s", jobs))
	assert.Contains(t, out, "FRIEND-STAGE OK friend=amy card=s1-1.w1 gen=1 branch=sprint/s1-1.w1.g1.e0")

	jobDir := filepath.Join(jobs, "s1-1.w1")
	jobmd, err := os.ReadFile(filepath.Join(jobDir, "JOB.md"))
	require.NoError(t, err)
	assert.Contains(t, string(jobmd), "CARD: s1-1.w1\n")
	assert.Contains(t, string(jobmd), "GEN: 1\n")
	assert.Contains(t, string(jobmd), "BRANCH: sprint/s1-1.w1.g1.e0\n")
	assert.Contains(t, string(jobmd), "FINISH: nova-sprint finish --as friend.amy s1-1.w1@1 --epoch 0 --head sprint/s1 --branch sprint/s1-1.w1.g1.e0 --base sprint/s1\n")
	assert.Contains(t, string(jobmd), "FINISH-FAILED: nova-sprint finish --as friend.amy s1-1.w1@1 --epoch 0 --head sprint/s1 --branch sprint/s1-1.w1.g1.e0 --base sprint/s1 --failed\n")

	// the friend's identity was set in the clone's local config, and the clone
	// was made at the delivered branch
	var sawName, sawEmail, sawClone, sawCheckout bool
	for _, c := range calls {
		switch {
		case c.args[0] == "clone":
			sawClone = true
			assert.Equal(t, jobs, c.dir, "the clone is made under the jobs directory")
		case c.args[0] == "config" && len(c.args) >= 3 && c.args[1] == "user.name":
			sawName = true
			assert.Equal(t, "Amy Author", c.args[2])
		case c.args[0] == "config" && len(c.args) >= 3 && c.args[1] == "user.email":
			sawEmail = true
			assert.Equal(t, "amy@example.com", c.args[2])
		case c.args[0] == "checkout":
			sawCheckout = true
			assert.Equal(t, "sprint/s1-1.w1.g1.e0", c.args[3], "the delivered branch is created")
		}
	}
	assert.True(t, sawClone, "the card's repo is cloned")
	assert.True(t, sawName && sawEmail, "the clone's local identity is the friend's")
	assert.True(t, sawCheckout, "the delivered branch is checked out")

	// a second stage of the same card@gen is idempotent: it prints the JOB.md path
	before := len(calls)
	code, out, errs := ta.do(fmt.Sprintf("friend stage s1-1.w1@1 --as friend.amy --dir %s", jobs))
	assert.Equal(t, 0, code, "%s", errs)
	assert.Contains(t, out, filepath.Join(jobDir, "JOB.md"))
	assert.Equal(t, before, len(calls), "an idempotent stage runs no git")

	// a card not dealt to the named friend is refused
	code, _, errs = ta.do(fmt.Sprintf("friend stage s1-1.w1@1 --as friend.bob --dir %s", jobs))
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "not dealt to friend bob")

	// a friend with no git_identity on record is refused and no clone is made
	before = len(calls)
	code, _, errs = ta.do(fmt.Sprintf("friend stage s1-2.w1@1 --as friend.bob --dir %s", filepath.Join(root, "bob-working", "jobs")))
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "no git_identity field")
	assert.Equal(t, before, len(calls), "no clone is made for a friend with no identity")
	_, statErr := os.Lstat(filepath.Join(root, "bob-working", "jobs", "s1-2.w1"))
	assert.True(t, os.IsNotExist(statErr), "the job directory was not created")
}
