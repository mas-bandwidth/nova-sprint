package main

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// TestHostForge: CanAct reads gh's viewerPermission and refuses below
// write, and a gh that cannot read, naming the fix; PR reads head and state.
// gh is a fake (forge.ExecFunc): no network.
func TestHostForge(t *testing.T) {
	t.Parallel()
	out, code := "WRITE\n", 0
	var argv []string
	f := hostForge{exec: forge.ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		argv = cmd.Argv
		return edge.Result{Stdout: out, ExitCode: code, Stderr: "HTTP 404"}, nil
	})}
	ctx := context.Background()
	if err := f.CanAct(ctx, "o/r"); err != nil || strings.Join(argv, " ") != "gh repo view o/r --json viewerPermission --jq .viewerPermission" {
		t.Fatalf("write: %v %v", err, argv)
	}
	out = "READ\n"
	if err := f.CanAct(ctx, "o/r"); err == nil || !strings.Contains(err.Error(), "READ permission on o/r") {
		t.Fatalf("read: %v", err)
	}
	out, code = "", 1
	if err := f.CanAct(ctx, "o/r"); err == nil || !strings.Contains(err.Error(), "cannot read o/r") {
		t.Fatalf("no read: %v", err)
	}
	out, code = `{"headRefOid":"ABC","state":"MERGED"}`, 0
	head, merged, err := f.PR(ctx, "o/r", 7)
	if err != nil || head != "abc" || !merged || strings.Join(argv, " ") != "gh pr view 7 -R o/r --json headRefOid,state" {
		t.Fatalf("pr: %q %v %v %v", head, merged, err, argv)
	}
	out = "not json"
	if _, _, err := f.PR(ctx, "o/r", 7); err == nil {
		t.Fatal("a body that is not json read")
	}
}

// TestHostForgeRequiredChecks: the required checks are the branch
// protection's contexts and the rulesets' contexts, joined without repeats;
// either read failing is an error. gh is a fake: no network.
func TestHostForgeRequiredChecks(t *testing.T) {
	t.Parallel()
	var argvs []string
	fail := ""
	f := hostForge{exec: forge.ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		line := strings.Join(cmd.Argv, " ")
		argvs = append(argvs, line)
		switch {
		case fail != "" && strings.Contains(line, fail):
			return edge.Result{ExitCode: 1, Stderr: "HTTP 403"}, nil
		case strings.Contains(line, "/rules/branches/"):
			return edge.Result{Stdout: "Gates\nContributor Assignment Agreement\n"}, nil
		}
		return edge.Result{Stdout: "Gates\n\nLinux Debug\n"}, nil
	})}
	got, err := f.RequiredChecks(context.Background(), "o/r", "main")
	if err != nil || strings.Join(got, "|") != "Gates|Linux Debug|Contributor Assignment Agreement" {
		t.Fatalf("got %q, %v", got, err)
	}
	if len(argvs) != 2 || !strings.HasPrefix(argvs[0], "gh api repos/o/r/branches/main --jq") || !strings.HasPrefix(argvs[1], "gh api repos/o/r/rules/branches/main --jq") {
		t.Fatalf("argvs %q", argvs)
	}
	for _, which := range []string{"/rules/branches/", "/branches/main --jq"} {
		fail = which
		if _, err := f.RequiredChecks(context.Background(), "o/r", "main"); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
			t.Fatalf("%s: %v", which, err)
		}
	}
}

// TestRequiredChecksFromAURL: a stream stored as a clone URL reads
// repos/<owner>/<repo>, never the URL pasted into the path.
func TestRequiredChecksFromAURL(t *testing.T) {
	t.Parallel()
	var argvs []string
	f := hostForge{exec: forge.ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		argvs = append(argvs, strings.Join(cmd.Argv, " "))
		return edge.Result{Stdout: "Gates\n"}, nil
	})}
	if _, err := f.RequiredChecks(context.Background(), "https://github.com/mas-bandwidth/nova-tools", "main"); err != nil {
		t.Fatal(err)
	}
	if len(argvs) != 2 || !strings.HasPrefix(argvs[0], "gh api repos/mas-bandwidth/nova-tools/branches/main ") || !strings.HasPrefix(argvs[1], "gh api repos/mas-bandwidth/nova-tools/rules/branches/main ") {
		t.Fatalf("argv %q", argvs)
	}
}

// TestOriginFetchSerialized: the clone and fetch of one address's clone are
// held under one lock per clone dir (concurrent git fetch in one repository
// fails "cannot lock ref", and admission read that as a repository that
// cannot be cloned). While one read of the address is fetching, a second
// read (Tree, Commits, Contains) does not fetch, and one whose ctx ends while
// it waits returns the ctx's error; the lock is a file lock, so it holds
// across processes too. git is a fake (no process); the lock file is in a
// temp dir.
func TestOriginFetchSerialized(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	var fetches atomic.Int32
	o := &origin{dir: t.TempDir(), git: git.New(git.Config{Exec: git.ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		if slices.Contains(cmd.Argv, "fetch") && fetches.Add(1) == 1 {
			entered <- struct{}{}
			<-release
		}
		return edge.Result{}, nil
	})})}
	const url = "https://github.com/o/r"
	first := make(chan error, 1)
	go func() {
		_, err := o.Tree(context.Background(), url, "abc")
		first <- err
	}()
	<-entered
	done, cancel := context.WithCancel(context.Background())
	cancel()
	_, errTree := o.Tree(done, url, "abc")
	_, errCommits := o.Commits(done, url, "abc", "main", "")
	_, errContains := o.Contains(done, url, "abc", "def")
	for _, err := range []error{errTree, errCommits, errContains} {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("a read that waited on a fetch in flight: %v (want context.Canceled)", err)
		}
	}
	if n := fetches.Load(); n != 1 {
		t.Fatalf("%d fetches in one clone at once, want 1", n)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if _, err := o.Tree(context.Background(), url, "abc"); err != nil || fetches.Load() != 2 {
		t.Fatalf("after the first fetch: %v, %d fetches", err, fetches.Load())
	}
}

// TestLanderPushTakesTheClonesLock: git push updates refs/remotes/origin/<ref>
// in the clone, so the lander's push holds the clone's lock as its fetch
// does. git is a fake (no process).
func TestLanderPushTakesTheClonesLock(t *testing.T) {
	t.Parallel()
	var pushLocked []bool
	l := &lander{clones: &origin{dir: t.TempDir(), git: git.New(git.Config{Exec: git.ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		switch {
		case slices.Contains(cmd.Argv, "push"):
			f, err := os.OpenFile(cmd.Dir+".lock", os.O_CREATE|os.O_RDWR, 0o600)
			if err != nil {
				return edge.Result{}, err
			}
			pushLocked = append(pushLocked, syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil)
			if err := f.Close(); err != nil {
				return edge.Result{}, err
			}
		}
		return edge.Result{}, nil
	})})}}
	s := store.Stream{Name: "s1", Repo: "https://example.invalid/r.git", Base: "main"}
	if err := l.CreateGroup(context.Background(), s, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	if len(pushLocked) != 1 || !pushLocked[0] {
		t.Fatalf("pushes with the lock held: %v, want [true]", pushLocked)
	}
}

// TestLanderLandHoldsTheClonesLock: a hand landing with no forge merges in
// the clone's work tree (MergeGroup's checkout and merge) and pushes, so one
// lock is held from the fetch through the push. git is a fake (no process):
// the checkout records whether the lock is held, then fails the merge.
func TestLanderLandHoldsTheClonesLock(t *testing.T) {
	t.Parallel()
	tip, head := strings.Repeat("a", 40), strings.Repeat("b", 40)
	var checkoutLocked []bool
	l := &lander{clones: &origin{dir: t.TempDir(), git: git.New(git.Config{Exec: git.ExecFunc(func(_ context.Context, cmd edge.Cmd) (edge.Result, error) {
		switch {
		case slices.Contains(cmd.Argv, "ls-remote"):
			return edge.Result{Stdout: tip + "\trefs/heads/main\n"}, nil
		case slices.Contains(cmd.Argv, "merge-base"):
			return edge.Result{ExitCode: 1}, nil
		case slices.Contains(cmd.Argv, "checkout"):
			f, err := os.OpenFile(cmd.Dir+".lock", os.O_CREATE|os.O_RDWR, 0o600)
			if err != nil {
				return edge.Result{}, err
			}
			checkoutLocked = append(checkoutLocked, syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil)
			if err := f.Close(); err != nil {
				return edge.Result{}, err
			}
			return edge.Result{ExitCode: 1, Stderr: "stop here"}, nil
		}
		return edge.Result{}, nil
	})})}}
	ctx := git.WithIdentity(context.Background(), "Test", "test@example.invalid")
	s := store.Stream{Name: "s1", Repo: "https://example.invalid/r.git", Base: "main"}
	if _, err := l.Land(ctx, s, "card/x", head); err == nil || !strings.Contains(err.Error(), "stop here") {
		t.Fatalf("land: %v", err)
	}
	if len(checkoutLocked) != 1 || !checkoutLocked[0] {
		t.Fatalf("checkouts with the lock held: %v, want [true]", checkoutLocked)
	}
}

// A landing whose merge succeeded is landed even when the clone's lock then
// fails to release: the merge sha is returned, the lock error said apart.
func TestLandedEvenWhenTheLockFailsToRelease(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("e", 40)
	var said strings.Builder
	got, err := landResult(&said, sha, nil, errors.New("close: bad file descriptor"))
	if got != sha || err != nil || !strings.Contains(said.String(), "landed "+sha) || !strings.Contains(said.String(), "bad file descriptor") {
		t.Fatalf("landResult = %q, %v; said %q", got, err, said.String())
	}
	if _, err := landResult(&said, "", errors.New("merge refused"), errors.New("close")); err == nil {
		t.Fatal("a failed merge must stay a failure")
	}
}
