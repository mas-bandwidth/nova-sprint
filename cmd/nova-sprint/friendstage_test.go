package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/friend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A friend's dealt card is staged by a verb (docs/FRIENDS.md), never by hand: a
// friend stage <card>@<gen> --as friend.<name> clones the card's REPO at its
// delivered branch, sets the friend's own git identity in the clone's local
// config (never the machine's global one), writes JOB.md naming the finish
// command, and refuses a card not on the friend's row or a friend with no
// git_identity on record. The fake runner records every argv and every config
// write, so the test asserts the clone's local user.name and user.email are the
// friend's.
func TestAFriendCardIsStagedByAVerb(t *testing.T) {
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
	// s1-3 carries an earlier attempt's head: its NEXT head
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-3.md"), []byte(passingBrief("s1-3: a carried friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nCARRY: s1-3 attempt 1 head="+landHead+"\nWHO: friend amy")), 0o644))
	ta.ok("add --stream s1 --brief-dir " + dir)
	ta.ok("start")
	ta.ok("tick")

	var calls []struct {
		host string
		op   friendOp
	}
	files := map[string]string{}
	prev := friendStageRun
	friendStageRun = func(_ context.Context, op friendOp) (string, error) {
		calls = append(calls, struct {
			host string
			op   friendOp
		}{op.Host, op})
		switch {
		case len(op.Git) > 0:
			if op.Git[0] == "clone" {
				require.NoError(t, os.MkdirAll(op.Git[len(op.Git)-1], 0o755))
			}
		case op.Job != "":
			files[filepath.Join(op.Dir, friend.JobFile)] = op.Job
		case op.Read:
			s, ok := files[filepath.Join(op.Dir, friend.JobFile)]
			if !ok {
				return "", &os.PathError{Path: filepath.Join(op.Dir, friend.JobFile), Op: "open", Err: os.ErrNotExist}
			}
			return s, nil
		}
		return "", nil
	}
	defer func() { friendStageRun = prev }()

	jobs := filepath.Join(root, "amy-working", "jobs")
	out := ta.ok(fmt.Sprintf("friend stage s1-1.w1@1 --as friend.amy --dir %s", jobs))
	assert.Contains(t, out, "FRIEND-STAGE OK friend=amy card=s1-1.w1 gen=1 branch=sprint/s1-1.w1.g1.e0")

	jobDir := filepath.Join(jobs, "s1-1.w1")
	jobmd, ok := files[filepath.Join(jobDir, "JOB.md")]
	require.True(t, ok, "JOB.md was written")
	assert.Contains(t, jobmd, "CARD: s1-1.w1\n")
	assert.Contains(t, jobmd, "GEN: 1\n")
	assert.Contains(t, jobmd, "BRANCH: sprint/s1-1.w1.g1.e0\n")
	assert.Contains(t, jobmd, "BENCH: local\n")
	assert.Contains(t, jobmd, "FINISH: nova-sprint finish --as friend.amy s1-1.w1@1 --epoch 0 --head sprint/s1 --branch sprint/s1-1.w1.g1.e0 --base sprint/s1\n")
	assert.Contains(t, jobmd, "FINISH-FAILED: nova-sprint finish --as friend.amy s1-1.w1@1 --epoch 0 --head sprint/s1 --branch sprint/s1-1.w1.g1.e0 --base sprint/s1 --failed\n")

	// the friend's identity was set in the clone's local config, and the clone
	// was made at the delivered branch
	var sawName, sawEmail, sawClone, sawCheckout bool
	for _, c := range calls {
		if len(c.op.Git) == 0 {
			continue
		}
		switch c.op.Git[0] {
		case "clone":
			sawClone = true
			assert.Equal(t, jobs, c.op.Dir, "the clone is made under the jobs directory")
		case "config":
			switch {
			case len(c.op.Git) >= 3 && c.op.Git[1] == "user.name":
				sawName = true
				assert.Equal(t, "Amy Author", c.op.Git[2])
			case len(c.op.Git) >= 3 && c.op.Git[1] == "user.email":
				sawEmail = true
				assert.Equal(t, "amy@example.com", c.op.Git[2])
			}
		case "checkout":
			sawCheckout = true
			assert.Equal(t, "sprint/s1-1.w1.g1.e0", c.op.Git[3], "the delivered branch is created")
		}
	}
	assert.True(t, sawClone, "the card's repo is cloned")
	assert.True(t, sawName && sawEmail, "the clone's local identity is the friend's")
	assert.True(t, sawCheckout, "the delivered branch is checked out")

	// no git invocation after the clone ran before both identity writes: nothing
	// after the clone can commit under a global identity
	nameAt, emailAt := -1, -1
	for i, c := range calls {
		if len(c.op.Git) >= 2 && c.op.Git[0] == "config" && c.op.Git[1] == "user.name" {
			nameAt = i
		}
		if len(c.op.Git) >= 2 && c.op.Git[0] == "config" && c.op.Git[1] == "user.email" {
			emailAt = i
		}
	}
	for i, c := range calls {
		if len(c.op.Git) == 0 || c.op.Git[0] == "clone" || c.op.Git[0] == "config" {
			continue
		}
		assert.Greater(t, i, nameAt, "%v ran before user.name was set", c.op.Git)
		assert.Greater(t, i, emailAt, "%v ran before user.email was set", c.op.Git)
	}

	// a card carrying a NEXT head stages at that head: fetched, then the
	// delivered branch created on it, and JOB.md names it as the head
	calls = nil
	out = ta.ok(fmt.Sprintf("friend stage s1-3.w1@1 --as friend.amy --dir %s", jobs))
	assert.Contains(t, out, "FRIEND-STAGE OK friend=amy card=s1-3.w1 gen=1 branch=sprint/s1-3.w1.g1.e0")
	var sawFetch, sawNextCheckout bool
	for _, c := range calls {
		if len(c.op.Git) == 0 {
			continue
		}
		if c.op.Git[0] == "fetch" {
			sawFetch = true
			assert.Equal(t, landHead, c.op.Git[len(c.op.Git)-1])
		}
		if c.op.Git[0] == "checkout" {
			sawNextCheckout = true
			assert.Equal(t, []string{"checkout", "-q", "-B", "sprint/s1-3.w1.g1.e0", landHead}, c.op.Git, "the branch is created at the NEXT head")
		}
	}
	assert.True(t, sawFetch && sawNextCheckout)
	jm3, ok := files[filepath.Join(jobs, "s1-3.w1", "JOB.md")]
	require.True(t, ok, "JOB.md was written")
	assert.Contains(t, jm3, "HEAD: "+landHead+"\n")
	assert.Contains(t, jm3, "--head "+landHead+" --branch sprint/s1-3.w1.g1.e0 --base sprint/s1\n")

	// a second stage of the same card@gen is idempotent: it prints the JOB.md
	// path and runs no git (only the staged JOB.md read)
	gitSteps := func() int {
		n := 0
		for _, c := range calls {
			if len(c.op.Git) > 0 {
				n++
			}
		}
		return n
	}
	before := gitSteps()
	code, out, errs := ta.do(fmt.Sprintf("friend stage s1-1.w1@1 --as friend.amy --dir %s", jobs))
	assert.Equal(t, 0, code, "%s", errs)
	assert.Contains(t, out, filepath.Join(jobDir, "JOB.md"))
	assert.Equal(t, before, gitSteps(), "an idempotent stage runs no git")

	// a card not dealt to the named friend is refused
	code, _, errs = ta.do(fmt.Sprintf("friend stage s1-1.w1@1 --as friend.bob --dir %s", jobs))
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "not dealt to friend bob")

	// a friend with no git_identity on her row is staged with the sprint row's (v1.4.5: the
	// friend kind has no such field, so every friend delivery was refused), and says so
	before = len(calls)
	code, out, errs = ta.do(fmt.Sprintf("friend stage s1-2.w1@1 --as friend.bob --dir %s", filepath.Join(root, "bob-working", "jobs")))
	assert.Equal(t, 0, code, "%s", errs)
	assert.Contains(t, out, "identity=sprint", "the OK line names the identity it set")
	sprintID, ok := config.ParseGitIdentity(config.DefaultGitIdentity)
	require.True(t, ok)
	var email string
	for _, c := range calls[before:] {
		if len(c.op.Git) == 3 && c.op.Git[0] == "config" && c.op.Git[1] == "user.email" {
			email = c.op.Git[2]
		}
	}
	assert.Equal(t, sprintID.Email, email, "the clone commits as the sprint row's identity, never the host's global one")
}
