package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/friend"
	"github.com/mas-bandwidth/nova-tools/pkg/testbin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// benchRec is a fake friendStageRun that records every step's host and argv, and
// keeps the JOB.md writes it sees so the idempotent re-run and the BENCH line
// can be asserted. No test opens a socket.
type benchRec struct {
	t     *testing.T
	calls []struct {
		host string
		op   friendOp
	}
	files map[string]string
}

func (r *benchRec) install() {
	r.t.Helper()
	prev := friendStageRun
	friendStageRun = func(_ context.Context, op friendOp) (string, error) {
		r.calls = append(r.calls, struct {
			host string
			op   friendOp
		}{op.Host, op})
		switch {
		case len(op.Git) > 0:
			if op.Git[0] == "clone" {
				require.NoError(r.t, os.MkdirAll(op.Git[len(op.Git)-1], 0o755))
			}
		case op.Job != "":
			r.files[filepath.Join(op.Dir, friend.JobFile)] = op.Job
		case op.Read:
			s, ok := r.files[filepath.Join(op.Dir, friend.JobFile)]
			if !ok {
				return "", &os.PathError{Path: filepath.Join(op.Dir, friend.JobFile), Op: "open", Err: os.ErrNotExist}
			}
			return s, nil
		}
		return "", nil
	}
	r.t.Cleanup(func() { friendStageRun = prev })
}

// dealToAmy is a sprint with one card s1-1.w1@1 dealt to friend amy, ready, and
// returns the temp root friend sync used.
func dealToAmy(t *testing.T, ta *testApp) string {
	t.Helper()
	ta.a.tip = tipIs(t, landHead)
	root := t.TempDir()
	ta.ok("friend sync --root " + root)
	ta.beatUp("amy")
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-1.md"), []byte(passingBrief("s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nWHO: friend amy")), 0o644))
	ta.ok("add --stream s1 --brief-dir " + dir)
	ta.ok("start")
	ta.ok("tick")
	return root
}

// amyWithIdentity is a friendApp with amy on record naming a git identity.
func amyWithIdentity(t *testing.T) (*testApp, *config.Mem) {
	t.Helper()
	ta, cfg := friendApp(t, "amy")
	_, err := cfg.Delete(context.Background(), config.KindFriend, "amy", "t")
	require.NoError(t, err)
	_, err = cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: "amy", Fields: map[string]string{"tiers": "flash", "git_identity": "Amy Author <amy@example.com>"}}, "t")
	require.NoError(t, err)
	return ta, cfg
}

// friend stage --bench <host> runs every git step and the JOB.md write on that
// host (a fleet member) over ssh, through the one runner seam: the fake records
// the host and each git argv, and no test opens a socket.
func TestFriendStageClonesOnTheBenchItNames(t *testing.T) {
	ta, _ := amyWithIdentity(t)
	dealToAmy(t, ta)

	rec := &benchRec{t: t, files: map[string]string{}}
	rec.install()

	jobs := filepath.Join(t.TempDir(), "jobs")
	out := ta.ok(fmt.Sprintf("friend stage s1-1.w1@1 --as friend.amy --bench m1 --dir %s", jobs))

	var sawClone, sawName, sawEmail, sawCheckout, sawWrite bool
	for _, c := range rec.calls {
		if len(c.op.Git) > 0 {
			assert.Equal(t, "m1", c.host, "every git step runs on the named bench")
			switch c.op.Git[0] {
			case "clone":
				sawClone = true
				assert.Equal(t, jobs, c.op.Dir)
			case "config":
				if len(c.op.Git) >= 2 && c.op.Git[1] == "user.name" {
					sawName = true
				}
				if len(c.op.Git) >= 2 && c.op.Git[1] == "user.email" {
					sawEmail = true
				}
			case "checkout":
				sawCheckout = true
			}
		}
		if c.op.Job != "" {
			sawWrite = true
			assert.Equal(t, "m1", c.host, "the JOB.md write runs on the named bench")
			assert.Contains(t, c.op.Job, "BENCH: m1\n")
		}
	}
	assert.True(t, sawClone, "the clone ran on the bench")
	assert.True(t, sawName && sawEmail, "the identity was set on the bench")
	assert.True(t, sawCheckout, "the delivered branch was checked out on the bench")
	assert.True(t, sawWrite, "JOB.md was written on the bench")
	assert.Contains(t, out, "FRIEND-STAGE OK")
	assert.Contains(t, out, "bench=m1")
}

// JOB.md names the bench it staged on: BENCH: <host> with --bench, BENCH:
// local without, and the idempotent re-run compares the bench too, so staging
// the same card@gen on another bench is refused as a different stage. With no
// --dir on a bench the jobs directory is ~/<friend>-jobs on that host, left
// for the remote shell to expand, never this machine's HOME.
func TestFriendStageWritesTheBenchIntoJobMd(t *testing.T) {
	ta, _ := amyWithIdentity(t)
	dealToAmy(t, ta)

	rec := &benchRec{t: t, files: map[string]string{}}
	rec.install()

	jobs := filepath.Join("$HOME", "amy-jobs")
	jobDir := filepath.Join(jobs, "s1-1.w1")

	ta.ok("friend stage s1-1.w1@1 --as friend.amy --bench m1")
	md := rec.files[filepath.Join(jobDir, friend.JobFile)]
	assert.Contains(t, md, "BENCH: m1\n")
	assert.Contains(t, string(md), "CARD: s1-1.w1\n")

	var sawDefaultClone bool
	for _, c := range rec.calls {
		if len(c.op.Git) > 0 && c.op.Git[0] == "clone" {
			sawDefaultClone = true
			assert.Equal(t, jobs, c.op.Dir, "the default jobs dir on a bench is ~/amy-jobs")
			assert.Equal(t, jobDir, c.op.Git[len(c.op.Git)-1])
		}
	}
	assert.True(t, sawDefaultClone, "the default-dir clone ran on the bench")
	assert.Equal(t, `"$HOME/"'amy-jobs/s1-1.w1'`, friendPath(jobDir), "the remote shell expands $HOME, not this machine")

	// the same stage on the same bench is idempotent: no new git steps (only
	// the staged JOB.md read)
	gitSteps := func() int {
		n := 0
		for _, c := range rec.calls {
			if len(c.op.Git) > 0 {
				n++
			}
		}
		return n
	}
	before := gitSteps()
	code, _, errs := ta.do("friend stage s1-1.w1@1 --as friend.amy --bench m1")
	assert.Equal(t, 0, code, "%s", errs)
	assert.Equal(t, before, gitSteps(), "an idempotent bench stage runs no git steps")

	// the same card@gen on another bench is a different stage: refused
	code, _, errs = ta.do("friend stage s1-1.w1@1 --as friend.amy --bench m2")
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "already staged as another card, branch, head or bench")
}

// TestFriendRunSSHReportsAMissingJobFileAsNotExist pins the first-time bench
// stage: cat of a JOB.md that it has not written yet exits non-zero, which must
// read as os.ErrNotExist so stage goes on to mkdir and clone instead of
// refusing "the staged JOB.md cannot be read". The fake ssh is a local script:
// no socket, no network.
func TestFriendRunSSHReportsAMissingJobFileAsNotExist(t *testing.T) {
	bin := t.TempDir()
	require.NoError(t, testbin.WriteExecutable(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\nexit 44\n"), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := friendRunSSH(context.Background(), friendOp{Host: "m1", Dir: "$HOME/amy-jobs/s1-1.w1", Read: true})
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err), "a missing JOB.md on the bench reads as not exist, not an ssh exit: %v", err)
}

// A host that is no fleet member is refused before anything is staged: nothing
// written, no git step.
func TestFriendStageRefusesABenchThatIsNoMember(t *testing.T) {
	ta, _ := amyWithIdentity(t)
	dealToAmy(t, ta)

	rec := &benchRec{t: t, files: map[string]string{}}
	rec.install()

	code, out, errs := ta.do(fmt.Sprintf("friend stage s1-1.w1@1 --as friend.amy --bench hulk --dir %s", filepath.Join(t.TempDir(), "jobs")))
	assert.Equal(t, 1, code)
	assert.Empty(t, out)
	assert.Contains(t, errs, "is no fleet member")
	assert.Empty(t, rec.calls, "a refused bench stages nothing")
}

// A friend row read fine that names no git_identity still falls back to the
// sprint row's identity (v1.4.5): the stage succeeds, saying identity=sprint.
func TestFriendStageNeedsNoPostgresForTheSprintIdentity(t *testing.T) {
	ta, cfg := amyWithIdentity(t)
	dealToAmy(t, ta)
	_, err := cfg.Delete(context.Background(), config.KindFriend, "amy", "t")
	require.NoError(t, err)
	_, err = cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: "amy", Fields: map[string]string{"tiers": "flash"}}, "t")
	require.NoError(t, err)

	rec := &benchRec{t: t, files: map[string]string{}}
	rec.install()

	jobs := filepath.Join(t.TempDir(), "jobs")
	out := ta.ok(fmt.Sprintf("friend stage s1-1.w1@1 --as friend.amy --bench m1 --dir %s", jobs))
	assert.Contains(t, out, "identity=sprint", "with no nova-config, the sprint row's identity is used")
	assert.Contains(t, out, "FRIEND-STAGE OK")
	assert.NotContains(t, out, "the config cannot be read")
}

// The sprint's server admits `friend stage <card>@<gen> --as friend.<name>
// --bench <host> [--dir <d>]` as a worker's verb, so a friend needs only the
// server address: the server runs it as the friend.
func TestTheServerRunsAFriendStage(t *testing.T) {
	r := newServerRig(t, "nova-sprint init --readers reader-a,reader-b --members m1:2")
	r.a.friends = friendRows("amy")
	root := t.TempDir()
	r.boss("nova-sprint friend sync --root " + root)
	r.pong("amy")
	res := r.one("friend", "beat", "amy")
	require.Equal(t, 0, res.Code, res.Stderr)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-1.md"), []byte(passingBrief("s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nWHO: friend amy")), 0o644))
	r.boss("nova-sprint add --stream s1 --brief-dir " + dir)
	r.boss("nova-sprint start")
	r.boss("nova-sprint tick")

	rec := &benchRec{t: t, files: map[string]string{}}
	rec.install()

	jobs := filepath.Join(t.TempDir(), "jobs")
	res = r.one("friend", "stage", "s1-1.w1@1", "--as", "friend.amy", "--bench", "m1", "--dir", jobs)
	require.Equal(t, 0, res.Code, res.Stderr)
	assert.Contains(t, res.Stdout, "FRIEND-STAGE OK friend=amy")
	var sawClone bool
	for _, c := range rec.calls {
		if len(c.op.Git) > 0 && c.op.Git[0] == "clone" {
			sawClone = true
			assert.Equal(t, "m1", c.host)
		}
	}
	assert.True(t, sawClone, "the server ran the clone on the named bench")
}
