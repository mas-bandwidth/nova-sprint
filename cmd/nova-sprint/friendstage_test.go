package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
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
	// s1-3 carries an earlier attempt's head: its NEXT head
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-3.md"), []byte(passingBrief("s1-3: a carried friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nCARRY: s1-3 attempt 1 head="+landHead+"\nWHO: friend amy")), 0o644))
	ta.ok("add --stream s1 --brief-dir " + dir)
	ta.ok("start")
	ta.ok("tick")

	var calls []struct {
		dir  string
		args []string
	}
	ta.a.stageGit = func(ctx context.Context, dir string, args ...string) (string, error) {
		_, bounded := ctx.Deadline()
		assert.True(t, bounded, "a locally typed friend stage is bounded too")
		calls = append(calls, struct {
			dir  string
			args []string
		}{dir, args})
		if args[0] == "clone" {
			require.NoError(t, os.MkdirAll(args[len(args)-1], 0o755))
		}
		return "", nil
	}

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

	// no git invocation after the clone ran before both identity writes: nothing
	// after the clone can commit under a global identity
	nameAt, emailAt := -1, -1
	for i, c := range calls {
		if c.args[0] == "config" && c.args[1] == "user.name" {
			nameAt = i
		}
		if c.args[0] == "config" && c.args[1] == "user.email" {
			emailAt = i
		}
	}
	for i, c := range calls {
		if c.args[0] != "clone" && c.args[0] != "config" {
			assert.Greater(t, i, nameAt, "%v ran before user.name was set", c.args)
			assert.Greater(t, i, emailAt, "%v ran before user.email was set", c.args)
		}
	}

	// a card carrying a NEXT head stages at that head: fetched, then the
	// delivered branch created on it, and JOB.md names it as the head
	calls = nil
	out = ta.ok(fmt.Sprintf("friend stage s1-3.w1@1 --as friend.amy --dir %s", jobs))
	assert.Contains(t, out, "FRIEND-STAGE OK friend=amy card=s1-3.w1 gen=1 branch=sprint/s1-3.w1.g1.e0")
	var sawFetch, sawNextCheckout bool
	for _, c := range calls {
		if c.args[0] == "fetch" {
			sawFetch = true
			assert.Equal(t, landHead, c.args[len(c.args)-1])
		}
		if c.args[0] == "checkout" {
			sawNextCheckout = true
			assert.Equal(t, []string{"checkout", "-q", "-B", "sprint/s1-3.w1.g1.e0", landHead}, c.args, "the branch is created at the NEXT head")
		}
	}
	assert.True(t, sawFetch && sawNextCheckout)
	jm3, err := os.ReadFile(filepath.Join(jobs, "s1-3.w1", "JOB.md"))
	require.NoError(t, err)
	assert.Contains(t, string(jm3), "HEAD: "+landHead+"\n")
	assert.Contains(t, string(jm3), "--head "+landHead+" --branch sprint/s1-3.w1.g1.e0 --base sprint/s1\n")

	// a second stage of the same card@gen is idempotent: it prints the JOB.md path
	before := len(calls)
	code, out, errs := ta.do(fmt.Sprintf("friend stage s1-1.w1@1 --as friend.amy --dir %s", jobs))
	assert.Equal(t, 0, code, "%s", errs)
	assert.Contains(t, out, filepath.Join(jobDir, "JOB.md"))
	assert.Equal(t, before, len(calls), "an idempotent stage runs no git")

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
		if len(c.args) == 3 && c.args[0] == "config" && c.args[1] == "user.email" {
			email = c.args[2]
		}
	}
	assert.Equal(t, sprintID.Email, email, "the clone commits as the sprint row's identity, never the host's global one")
}

// A served friend stage carries its caller's context into every git call. If
// the caller goes away, the bounded staging runner stops and the server releases
// its one control line instead of leaving later verbs behind a stuck clone.
func TestServedFriendStageStopsWithItsRequest(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t, "amy")
	_, err := cfg.Delete(context.Background(), config.KindFriend, "amy", "t")
	require.NoError(t, err)
	_, err = cfg.Insert(context.Background(), config.KindFriend, config.Row{
		Name: "amy", Fields: map[string]string{"tiers": "flash", "git_identity": "Amy Author <amy@example.com>"},
	}, "t")
	require.NoError(t, err)
	ta.a.tip = tipIs(t, landHead)
	root := t.TempDir()
	ta.ok("friend sync --root " + root)
	ta.beatUp("amy")
	briefs := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(briefs, "s1-1.md"), []byte(passingBrief("s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nWHO: friend amy")), 0o644))
	ta.ok("add --stream s1 --brief-dir " + briefs)
	ta.ok("start")
	ta.ok("tick")

	started := make(chan context.Context, 1)
	ta.a.stageGit = func(ctx context.Context, _ string, args ...string) (string, error) {
		if args[0] != "clone" {
			t.Fatalf("git %v ran after the clone was stopped", args)
		}
		started <- ctx
		<-ctx.Done()
		return "", ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan sprintwire.Response, 1)
	jobs := filepath.Join(root, "amy-working", "jobs")
	ta.a.serveAddr = "mem:0"
	go func() {
		done <- ta.a.serveCtx(ctx, sprintwire.Request{Verbs: [][]string{{"friend", "stage", "s1-1.w1@1", "--as", "friend.amy", "--dir", jobs}}}, true)
	}()
	var gitCtx context.Context
	select {
	case gitCtx = <-started:
	case res := <-done:
		t.Fatalf("friend stage ended before starting git: %+v", res.Results)
	case <-t.Context().Done():
		t.Fatal("friend stage did not reach git")
	}
	_, ok := gitCtx.Deadline()
	require.True(t, ok, "friend stage has its hard staging deadline")
	cancel()

	select {
	case res := <-done:
		require.Len(t, res.Results, 1)
		assert.NotEqual(t, 0, res.Results[0].Code)
		assert.Contains(t, res.Results[0].Stderr, "context canceled")
	case <-t.Context().Done():
		t.Fatal("the cancelled friend stage kept the control line")
	}
	assert.Nil(t, ta.a.runCtx, "the completed served verb keeps no request context")
	require.True(t, ta.a.serial.TryLock(), "the cancelled friend stage released the control line")
	ta.a.serial.Unlock()
}

// A jobs-root mkdir is bounded by the served request before Git starts. The live
// failure this guards was a mkdir syscall that held the sole control line for
// minutes; the Git cancellation test above could not reach it because no Git
// child existed yet.
func TestServedFriendStageStopsWhileMakingTheJobsDirectory(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t, "amy")
	_, err := cfg.Delete(context.Background(), config.KindFriend, "amy", "t")
	require.NoError(t, err)
	_, err = cfg.Insert(context.Background(), config.KindFriend, config.Row{
		Name: "amy", Fields: map[string]string{"tiers": "flash", "git_identity": "Amy Author <amy@example.com>"},
	}, "t")
	require.NoError(t, err)
	ta.a.tip = tipIs(t, landHead)
	root := t.TempDir()
	ta.ok("friend sync --root " + root)
	ta.beatUp("amy")
	briefs := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(briefs, "s1-1.md"), []byte(passingBrief("s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nWHO: friend amy")), 0o644))
	ta.ok("add --stream s1 --brief-dir " + briefs)
	ta.ok("start")
	ta.ok("tick")

	started := make(chan context.Context, 1)
	ta.a.stageMkdir = func(ctx context.Context, _ string) error {
		started <- ctx
		<-ctx.Done()
		return ctx.Err()
	}
	ta.a.stageGit = func(context.Context, string, ...string) (string, error) {
		t.Fatal("Git started before the jobs directory was made")
		return "", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan sprintwire.Response, 1)
	ta.a.serveAddr = "mem:0"
	go func() {
		done <- ta.a.serveCtx(ctx, sprintwire.Request{Verbs: [][]string{{"friend", "stage", "s1-1.w1@1", "--as", "friend.amy", "--dir", filepath.Join(root, "missing-jobs")}}}, true)
	}()
	var mkdirCtx context.Context
	select {
	case mkdirCtx = <-started:
	case res := <-done:
		t.Fatalf("friend stage ended before making its jobs directory: %+v", res.Results)
	case <-t.Context().Done():
		t.Fatal("friend stage did not reach the jobs-directory step")
	}
	_, ok := mkdirCtx.Deadline()
	require.True(t, ok, "friend stage's jobs-directory child has the hard staging deadline")
	cancel()

	select {
	case res := <-done:
		require.Len(t, res.Results, 1)
		assert.NotEqual(t, 0, res.Results[0].Code)
		assert.Contains(t, res.Results[0].Stderr, "context canceled")
	case <-t.Context().Done():
		t.Fatal("the cancelled jobs-directory step kept the control line")
	}
	assert.Nil(t, ta.a.runCtx, "the completed served verb keeps no request context")
	require.True(t, ta.a.serial.TryLock(), "the cancelled jobs-directory step released the control line")
	ta.a.serial.Unlock()
}

func TestStageMkdirCommandKeepsThePathOutOfShellText(t *testing.T) {
	t.Parallel()
	path := `C:\jobs\雪 & b 'quoted'`
	name, args, stdin := stageMkdirCommand("windows", path)
	assert.Equal(t, "powershell.exe", name)
	assert.Equal(t, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "[IO.Directory]::CreateDirectory([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadToEnd()))) > $null"}, args)
	decoded, err := base64.StdEncoding.DecodeString(stdin)
	require.NoError(t, err)
	assert.Equal(t, path, string(decoded), "Windows' code page never decodes the path")

	name, args, stdin = stageMkdirCommand("linux", path)
	assert.Equal(t, "mkdir", name)
	assert.Equal(t, []string{"-p", "--", path}, args)
	assert.Empty(t, stdin)
}

// A served friend stage takes the server's control line only for the short
// pre-stage assignment snapshot and post-stage revalidation before publishing
// JOB.md. While its clone blocks longer than a tick, a tick and representative
// hook/coordinator writes complete on the control line without waiting; when the
// clone releases, staging completes once and a rerun is idempotent while
// preserving dirty/advanced work. If the caller times out or the card's
// assignment changes during the clone, all lane and control-line locks are
// released and no stale JOB.md is published.
func TestServedFriendStageBlockedCloneLeavesTickAndWritesLive(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t, "amy", "bob")
	for _, f := range []struct{ name, id string }{
		{"amy", "Amy Author <amy@example.com>"},
		{"bob", "Bob Builder <bob@example.com>"},
	} {
		_, err := cfg.Delete(context.Background(), config.KindFriend, f.name, "t")
		require.NoError(t, err)
		_, err = cfg.Insert(context.Background(), config.KindFriend, config.Row{
			Name: f.name, Fields: map[string]string{"tiers": "flash", "git_identity": f.id},
		}, "t")
		require.NoError(t, err)
	}
	ta.a.tip = tipIs(t, landHead)
	ta.ok("init --readers reader-a,reader-b --members m1")
	root := t.TempDir()
	ta.ok("friend sync --root " + root)
	ta.beatUp("amy")
	ta.beatUp("bob")

	briefs := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(briefs, "s1-1.md"), []byte(passingBrief("s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nWHO: friend amy")), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(briefs, "s1-2.md"), []byte(passingBrief("s1-2: a second friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nWHO: friend bob")), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(briefs, "s1-3.md"), []byte(passingBrief("s1-3: a member's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1")), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(briefs, "s1-4.md"), []byte(passingBrief("s1-4: a third friend's card\nREPO: mas-bandwidth/nova-tools\nBASE: sprint/s1\nWHO: friend bob")), 0o644))
	ta.ok("add --stream s1 --brief-dir " + briefs)
	ta.ok("start")
	ta.ok("tick")

	ta.a.serveAddr = "mem:0"
	jobsAmy := filepath.Join(root, "amy-working", "jobs")
	jobsBob := filepath.Join(root, "bob-working", "jobs")

	// (1) Blocked clone longer than a tick leaves tick and hook/coordinator writes live.
	cloneStarted := make(chan context.Context, 1)
	releaseClone := make(chan struct{})
	cloneCount := 0
	ta.a.stageGit = func(ctx context.Context, _ string, args ...string) (string, error) {
		if args[0] == "clone" {
			cloneCount++
			target := args[len(args)-1]
			require.NoError(t, os.MkdirAll(target, 0o755))
			cloneStarted <- ctx
			select {
			case <-releaseClone:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return "", nil
	}

	stageDone := make(chan sprintwire.Response, 1)
	go func() {
		stageDone <- ta.a.serveCtx(context.Background(), sprintwire.Request{
			Verbs: [][]string{{"friend", "stage", "s1-1.w1@1", "--as", "friend.amy", "--dir", jobsAmy}},
		}, true)
	}()

	var gitCtx context.Context
	select {
	case gitCtx = <-cloneStarted:
	case res := <-stageDone:
		t.Fatalf("friend stage ended before clone blocked: %+v", res.Results)
	case <-t.Context().Done():
		t.Fatal("friend stage did not reach clone")
	}
	_, hasDeadline := gitCtx.Deadline()
	require.True(t, hasDeadline, "served friend stage preserves the hard staging deadline")

	// While clone is blocked, app.serial is free and JOB.md is not yet published.
	require.True(t, ta.a.serial.TryLock(), "control line must not be held while friend stage clones")
	ta.a.serial.Unlock()
	_, err := os.Lstat(filepath.Join(jobsAmy, "s1-1.w1", "JOB.md"))
	require.True(t, os.IsNotExist(err), "JOB.md must not exist before post-stage revalidation")

	// Advance past a tick and run both a tick on the control line and a served
	// hook/worker write while the clone stays blocked.
	ta.a.serial.waiting = func() {
		t.Error("a tick or write waited for the control line while friend stage was cloning")
	}
	ta.step(ta.a.quietEvery + 1)
	_, err = ta.a.serial.TickLockCtx(t.Context())
	require.NoError(t, err)
	ta.ok("tick")
	ta.a.serial.Unlock()

	beatRes := ta.a.serveCtx(t.Context(), sprintwire.Request{
		Verbs: [][]string{{"friend", "beat", "amy", "--running", "s1-1.w1"}},
	}, false)
	require.Len(t, beatRes.Results, 1)
	require.Equal(t, 0, beatRes.Results[0].Code, "%s", beatRes.Results[0].Stderr)

	takeRes := ta.a.serveCtx(t.Context(), sprintwire.Request{
		Verbs: [][]string{{"take", "--as", "m1", "--epoch", "0", "--max", "1"}},
	}, false)
	require.Len(t, takeRes.Results, 1)
	require.Equal(t, 0, takeRes.Results[0].Code, "%s", takeRes.Results[0].Stderr)
	assert.Equal(t, "accepted", takeRes.Results[0].Outcome)

	coordRes := ta.a.serveCtx(t.Context(), sprintwire.Request{
		Verbs: [][]string{{"inbox", "--read", "--actor", "coordinator"}},
	}, true)
	require.Len(t, coordRes.Results, 1)
	require.Equal(t, 0, coordRes.Results[0].Code, "%s", coordRes.Results[0].Stderr)
	ta.a.serial.waiting = nil

	// Release the blocked clone and prove staging completes once.
	close(releaseClone)
	res := <-stageDone
	require.Len(t, res.Results, 1)
	require.Equal(t, 0, res.Results[0].Code, "%s", res.Results[0].Stderr)
	assert.Equal(t, "accepted", res.Results[0].Outcome)
	assert.Equal(t, "yes", res.Results[0].Changed)
	assert.Contains(t, res.Results[0].Stdout, "FRIEND-STAGE OK friend=amy card=s1-1.w1 gen=1")
	assert.Equal(t, 1, cloneCount)

	jobPathAmy := filepath.Join(jobsAmy, "s1-1.w1", "JOB.md")
	jobBytes, err := os.ReadFile(jobPathAmy)
	require.NoError(t, err)
	assert.Contains(t, string(jobBytes), "CARD: s1-1.w1\n")

	// Same-card same-generation rerun is idempotent and preserves dirty work.
	dirtyFile := filepath.Join(jobsAmy, "s1-1.w1", "dirty.txt")
	require.NoError(t, os.WriteFile(dirtyFile, []byte("in-progress work\n"), 0o644))
	rerunRes := ta.a.serveCtx(t.Context(), sprintwire.Request{
		Verbs: [][]string{{"friend", "stage", "s1-1.w1@1", "--as", "friend.amy", "--dir", jobsAmy}},
	}, true)
	require.Len(t, rerunRes.Results, 1)
	require.Equal(t, 0, rerunRes.Results[0].Code, "%s", rerunRes.Results[0].Stderr)
	assert.Equal(t, "accepted", rerunRes.Results[0].Outcome)
	assert.Equal(t, "no", rerunRes.Results[0].Changed)
	assert.Equal(t, 1, cloneCount, "idempotent rerun must not clone again")
	dirtyAfter, err := os.ReadFile(dirtyFile)
	require.NoError(t, err)
	assert.Equal(t, "in-progress work\n", string(dirtyAfter))

	// (2) Negative control: an assignment change while the clone is blocked
	// cannot publish a stale JOB.md and reports the precise refusal.
	cloneStarted = make(chan context.Context, 1)
	releaseClone = make(chan struct{})
	ta.a.stageGit = func(ctx context.Context, _ string, args ...string) (string, error) {
		if args[0] == "clone" {
			target := args[len(args)-1]
			require.NoError(t, os.MkdirAll(target, 0o755))
			cloneStarted <- ctx
			select {
			case <-releaseClone:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return "", nil
	}

	go func() {
		stageDone <- ta.a.serveCtx(context.Background(), sprintwire.Request{
			Verbs: [][]string{{"friend", "stage", "s1-2.w1@1", "--as", "friend.bob", "--dir", jobsBob}},
		}, true)
	}()
	select {
	case <-cloneStarted:
	case res := <-stageDone:
		t.Fatalf("bob friend stage ended before clone blocked: %+v", res.Results)
	case <-t.Context().Done():
		t.Fatal("bob friend stage did not reach clone")
	}

	// While bob's clone is in flight, bob takes and finishes (--failed) the card on
	// the control line so s1-2.w1@1 is no longer ready or working on friend.bob.
	finishRes := ta.a.serveCtx(t.Context(), sprintwire.Request{
		Verbs: [][]string{
			{"take", "--as", "friend.bob", "--epoch", "0", "--max", "1"},
			{"finish", "--as", "friend.bob", "s1-2.w1@1", "--epoch", "0", "--head", "sprint/s1", "--branch", "sprint/s1-2.w1.g1.e0", "--base", "sprint/s1", "--failed", "--report", "aborted"},
		},
	}, false)
	require.Len(t, finishRes.Results, 2)
	require.Equal(t, 0, finishRes.Results[0].Code, "%s", finishRes.Results[0].Stderr)
	require.Equal(t, 0, finishRes.Results[1].Code, "%s", finishRes.Results[1].Stderr)

	close(releaseClone)
	staleRes := <-stageDone
	require.Len(t, staleRes.Results, 1)
	assert.Equal(t, 1, staleRes.Results[0].Code)
	assert.Equal(t, "refused", staleRes.Results[0].Outcome)
	assert.Contains(t, staleRes.Results[0].Stderr, "card s1-2.w1 at gen 1 is not dealt to friend bob (ready or working)")
	_, err = os.Lstat(filepath.Join(jobsBob, "s1-2.w1", "JOB.md"))
	assert.True(t, os.IsNotExist(err), "stale stage after assignment change must not publish JOB.md")

	// (3) Negative control: a timeout / cancellation during the blocked clone
	// releases all lane and control-line locks and publishes no JOB.md.
	cloneStarted = make(chan context.Context, 1)
	ta.a.stageGit = func(ctx context.Context, _ string, args ...string) (string, error) {
		if args[0] == "clone" {
			target := args[len(args)-1]
			require.NoError(t, os.MkdirAll(target, 0o755))
			cloneStarted <- ctx
			<-ctx.Done()
			return "", ctx.Err()
		}
		return "", nil
	}
	timeoutCtx, cancelTimeout := context.WithCancel(context.Background())
	go func() {
		stageDone <- ta.a.serveCtx(timeoutCtx, sprintwire.Request{
			Verbs: [][]string{{"friend", "stage", "s1-4.w1@1", "--as", "friend.bob", "--dir", jobsBob}},
		}, true)
	}()
	select {
	case <-cloneStarted:
	case res := <-stageDone:
		t.Fatalf("s1-4 friend stage ended before clone blocked: %+v", res.Results)
	case <-t.Context().Done():
		t.Fatal("s1-4 friend stage did not reach clone")
	}
	cancelTimeout()
	timedOutRes := <-stageDone
	require.Len(t, timedOutRes.Results, 1)
	assert.NotEqual(t, 0, timedOutRes.Results[0].Code)
	assert.Contains(t, timedOutRes.Results[0].Stderr, "context canceled")
	_, err = os.Lstat(filepath.Join(jobsBob, "s1-4.w1", "JOB.md"))
	assert.True(t, os.IsNotExist(err), "cancelled stage must not publish JOB.md")
	require.True(t, ta.a.serial.TryLock(), "cancelled stage released control line")
	ta.a.serial.Unlock()
	require.NotNil(t, ta.a.lanes)
	require.True(t, ta.a.lanes.stage.TryLock(), "cancelled stage released stage lane lock")
	ta.a.lanes.stage.Unlock()

	// After the cancelled clone cleaned up its unstaged directory and released all
	// locks, a subsequent stage of s1-4.w1@1 succeeds cleanly.
	ta.a.stageGit = func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "clone" {
			require.NoError(t, os.MkdirAll(args[len(args)-1], 0o755))
		}
		return "", nil
	}
	retryRes := ta.a.serveCtx(t.Context(), sprintwire.Request{
		Verbs: [][]string{{"friend", "stage", "s1-4.w1@1", "--as", "friend.bob", "--dir", jobsBob}},
	}, true)
	require.Len(t, retryRes.Results, 1)
	require.Equal(t, 0, retryRes.Results[0].Code, "%s", retryRes.Results[0].Stderr)
	assert.Equal(t, "accepted", retryRes.Results[0].Outcome)
	assert.Equal(t, "yes", retryRes.Results[0].Changed)

	// A mismatched existing JOB.md (wrong head/branch) is still refused.
	require.NoError(t, os.WriteFile(filepath.Join(jobsBob, "s1-4.w1", "JOB.md"), []byte("CARD: s1-4.w1\nGEN: 1\nBRANCH: sprint/other\nHEAD: sprint/s1\n"), 0o644))
	mismatchRes := ta.a.serveCtx(t.Context(), sprintwire.Request{
		Verbs: [][]string{{"friend", "stage", "s1-4.w1@1", "--as", "friend.bob", "--dir", jobsBob}},
	}, true)
	require.Len(t, mismatchRes.Results, 1)
	assert.Equal(t, 1, mismatchRes.Results[0].Code)
	assert.Contains(t, mismatchRes.Results[0].Stderr, "already staged as another card, branch or head; nothing was written")
}
