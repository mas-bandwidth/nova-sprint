package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge/forge"
)

func setupBareAndWorkRepo_internal(t *testing.T) (originDir, workDir, baseSHA string, runGit func(dir string, args ...string) string) {
	t.Helper()
	tmp := t.TempDir()
	originDir = filepath.Join(tmp, "origin.git")
	workDir = filepath.Join(tmp, "work")

	runGit = func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_AUTHOR_NAME=Test Author",
			"GIT_AUTHOR_EMAIL=test@mas-bandwidth.com",
			"GIT_COMMITTER_NAME=Test Author",
			"GIT_COMMITTER_EMAIL=test@mas-bandwidth.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v in %s failed: %v\n%s", args, dir, err, string(out))
		}
		return strings.TrimSpace(string(out))
	}

	if err := os.MkdirAll(originDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(originDir, "init", "--bare")
	runGit(tmp, "clone", originDir, workDir)
	runGit(workDir, "checkout", "-b", "v2")
	writeFile_internal(t, filepath.Join(workDir, "README.md"), "nova-sprint v2\n")
	writeFile_internal(t, filepath.Join(workDir, "tla", "RUNS.tsv"), "model\tstates\n")
	runGit(workDir, "add", ".")
	runGit(workDir, "commit", "-m", "initial v2")
	runGit(workDir, "push", "origin", "v2:refs/heads/v2")
	baseSHA = runGit(workDir, "rev-parse", "HEAD")
	return originDir, workDir, baseSHA, runGit
}

func writeFile_internal(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitBranch_internal(t *testing.T, runGit func(string, ...string) string, workDir, branch, baseSHA, relPath, content, remoteRef string) string {
	t.Helper()
	runGit(workDir, "checkout", "-b", branch, baseSHA)
	writeFile_internal(t, filepath.Join(workDir, relPath), content)
	runGit(workDir, "add", ".")
	runGit(workDir, "commit", "-m", branch)
	sha := runGit(workDir, "rev-parse", "HEAD")
	if remoteRef != "" {
		runGit(workDir, "push", "origin", sha+":"+remoteRef)
	}
	return sha
}

func TestFunctionalGitMergeLedgerRebaseAndPush(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, workDir, baseSHA, runGit := setupBareAndWorkRepo_internal(t)
	client := New(Config{})

	runGit(workDir, "checkout", "-b", "card-1", baseSHA)
	writeFile_internal(t, filepath.Join(workDir, "a.go"), "package a\n")
	writeFile_internal(t, filepath.Join(workDir, "tla", "RUNS.tsv"), "model\tstates\nStore\t100\n")
	runGit(workDir, "add", ".")
	runGit(workDir, "commit", "-m", "card 1")
	sha1 := runGit(workDir, "rev-parse", "HEAD")
	runGit(workDir, "push", "origin", sha1+":refs/heads/sprint/s1/card-1")

	runGit(workDir, "checkout", "-b", "card-2", baseSHA)
	writeFile_internal(t, filepath.Join(workDir, "b.go"), "package b\n")
	writeFile_internal(t, filepath.Join(workDir, "tla", "RUNS.tsv"), "model\tstates\nCardMachine\t200\n")
	runGit(workDir, "add", ".")
	runGit(workDir, "commit", "-m", "card 2")
	sha2 := runGit(workDir, "rev-parse", "HEAD")
	runGit(workDir, "push", "origin", sha2+":refs/heads/sprint/s1/card-2")

	mConflict, err := client.MergeGroup(ctx, workDir, baseSHA, []string{sha1, sha2}, nil)
	if err != nil || mConflict.Kind != MergeConflict || len(mConflict.Conflicts) != 1 || mConflict.Conflicts[0] != "tla/RUNS.tsv" {
		t.Fatalf("expected conflict on tla/RUNS.tsv, got %+v err=%v", mConflict, err)
	}

	mOK, err := client.MergeGroup(ctx, workDir, baseSHA, []string{sha1, sha2}, []LedgerRule{{Path: "tla/RUNS.tsv"}})
	if err != nil || mOK.Kind != MergeOK {
		t.Fatalf("MergeGroup with ledger: %+v err=%v", mOK, err)
	}
	runsBytes, err := os.ReadFile(filepath.Join(workDir, "tla", "RUNS.tsv"))
	if err != nil || !strings.Contains(string(runsBytes), "Store\t100") || !strings.Contains(string(runsBytes), "CardMachine\t200") {
		t.Fatalf("ledger union missing rows: %q err=%v", string(runsBytes), err)
	}
	verifyRebaseAndPush_internal(t, ctx, client, runGit, workDir, baseSHA, sha1, sha2)
}

func verifyRebaseAndPush_internal(t *testing.T, ctx context.Context, client *Client, runGit func(string, ...string) string, workDir, baseSHA, sha1, sha2 string) {
	t.Helper()
	_ = commitBranch_internal(t, runGit, workDir, "card-conflict", baseSHA, "a.go", "package conflict\n", "")
	rebConflict, err := client.Rebase(ctx, workDir, sha1)
	if err != nil || rebConflict.OK || len(rebConflict.Conflicts) != 1 || rebConflict.Conflicts[0] != "a.go" {
		t.Fatalf("expected Rebase conflict on a.go, got %+v err=%v", rebConflict, err)
	}

	if err := client.Push(ctx, workDir, sha1, "refs/heads/sprint/s1", ""); err == nil {
		t.Fatalf("expected bare refs/heads/sprint/s1 push to be refused")
	}
	if err := client.Push(ctx, workDir, sha1, "refs/heads/sprint/s1/group", ""); err != nil {
		t.Fatalf("initial stream push: %v", err)
	}
	if err := client.Push(ctx, workDir, sha2, "refs/heads/sprint/s1/group", baseSHA); !errors.Is(err, ErrPushRefused) {
		t.Fatalf("expected ErrPushRefused on stale stream lease, got %v", err)
	}
	if err := client.Push(ctx, workDir, sha1, "refs/heads/v2", ""); err != nil {
		t.Fatalf("fast-forward push to base v2: %v", err)
	}
	if err := client.Push(ctx, workDir, sha2, "refs/heads/v2", ""); !errors.Is(err, ErrPushRefused) {
		t.Fatalf("expected ErrPushRefused on non-fast-forward base push, got %v", err)
	}
}

func TestFunctionalLandPassEndToEndAndBaseMoved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	originDir, workDir, baseSHA, runGit := setupBareAndWorkRepo_internal(t)
	landerClone := filepath.Join(t.TempDir(), "lander")
	runGit(filepath.Dir(landerClone), "clone", originDir, landerClone)

	c1SHA := commitBranch_internal(t, runGit, workDir, "c1", baseSHA, "c1.txt", "card 1\n", "refs/heads/sprint/s1/c1")
	c2SHA := commitBranch_internal(t, runGit, workDir, "c2", baseSHA, "c2.txt", "card 2\n", "refs/heads/sprint/s1/c2")
	otherBaseSHA := commitBranch_internal(t, runGit, workDir, "other-stream", baseSHA, "other.txt", "other stream\n", "")

	client := New(Config{})
	now := int64(1000)
	ff := forge.NewFake(forge.Identity{Name: "sprint", ExpectedLogin: "rowan-claude", Token: "tok"}, func() int64 { return now })

	movedBase := false
	ff.OnCall = func(op string) {
		if op == "Checks" && !movedBase {
			movedBase = true
			runGit(workDir, "push", "origin", otherBaseSHA+":refs/heads/v2")
		}
		curSHA := runGit(landerClone, "rev-parse", "HEAD")
		ff.SetCheck("mas-bandwidth/nova-sprint", curSHA, forge.CheckRollup{State: forge.CheckGreen})
	}

	req := LandRequest{RepoDir: landerClone, Repo: "mas-bandwidth/nova-sprint", Stream: "s1", Base: "v2", Heads: []string{c1SHA, c2SHA}}
	res, err := LandPass(ctx, client, ff, req)
	if err != nil || res.Outcome != OutcomeLanded || !res.Remerged {
		t.Fatalf("expected OutcomeLanded with Remerged=true, got %+v err=%v", res, err)
	}
	assertLandedStateAndRestart_internal(t, ctx, client, ff, landerClone, req, res, []string{c1SHA, c2SHA, otherBaseSHA})
}

func assertLandedStateAndRestart_internal(t *testing.T, ctx context.Context, client *Client, ff *forge.Fake, landerClone string, req LandRequest, res LandResult, heads []string) {
	t.Helper()
	v2Remote, ok, err := client.LsRemote(ctx, landerClone, "refs/heads/v2")
	if err != nil || !ok || v2Remote != res.BaseTip {
		t.Fatalf("origin/v2 = %s (ok=%v err=%v), want %s", v2Remote, ok, err, res.BaseTip)
	}
	streamRemote, ok, err := client.LsRemote(ctx, landerClone, "refs/heads/sprint/s1/group")
	if err != nil || !ok || streamRemote != res.BaseTip {
		t.Fatalf("origin/sprint/s1/group = %s, want fast-forwarded to %s", streamRemote, res.BaseTip)
	}
	for _, h := range heads {
		if anc, err := client.IsAncestor(ctx, landerClone, h, res.BaseTip); err != nil || !anc {
			t.Fatalf("expected %s to be ancestor of %s", h, res.BaseTip)
		}
	}
	freshClient := New(Config{})
	resAgain, err := LandPass(ctx, freshClient, ff, req)
	if err != nil || resAgain.Outcome != OutcomeLanded || resAgain.BaseTip != res.BaseTip {
		t.Fatalf("restart LandPass = %+v err=%v, want Landed at %s", resAgain, err, res.BaseTip)
	}
}
