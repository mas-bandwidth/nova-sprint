package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

func TestFunctionalGitMergeConflictAndPush(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, workDir, baseSHA, runGit := setupBareAndWorkRepo_internal(t)
	client := New(Config{AuthorName: "Test Author", AuthorEmail: "test@mas-bandwidth.com"})

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

	mConflict, err := client.MergeGroup(ctx, workDir, baseSHA, []string{sha1, sha2})
	if err != nil || mConflict.Kind != MergeConflict || len(mConflict.Conflicts) != 1 || mConflict.Conflicts[0] != "tla/RUNS.tsv" {
		t.Fatalf("expected conflict on tla/RUNS.tsv, got %+v err=%v", mConflict, err)
	}
	if err := client.Push(ctx, workDir, sha1, GroupRef("s1"), ""); err != nil {
		t.Fatalf("initial group push: %v", err)
	}
	if err := client.Push(ctx, workDir, sha2, GroupRef("s1"), baseSHA); !errors.Is(err, ErrPushRefused) {
		t.Fatalf("expected ErrPushRefused on stale stream lease, got %v", err)
	}
	if err := client.Push(ctx, workDir, sha1, "refs/heads/v2", ""); err != nil {
		t.Fatalf("fast-forward push to base v2: %v", err)
	}
	if err := client.Push(ctx, workDir, sha2, "refs/heads/v2", ""); !errors.Is(err, ErrPushRefused) {
		t.Fatalf("expected ErrPushRefused on non-fast-forward base push, got %v", err)
	}
}

// Rung 2 on real git: two cards merged onto the base tip in the group
// branch; base moves after the gate, so the first pass merges nothing and
// the second rebuilds the group on the new tip, gates it and merges it (the
// fake forge's merge fast-forwards base to the gated sha, as a merge of a PR
// whose head contains the base would leave the same tree).
func TestFunctionalLandPassEndToEndAndBaseMoved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	originDir, workDir, baseSHA, runGit := setupBareAndWorkRepo_internal(t)
	lander := filepath.Join(t.TempDir(), "lander")
	runGit(filepath.Dir(lander), "clone", originDir, lander)
	c1 := commitBranch_internal(t, runGit, workDir, "c1", baseSHA, "c1.txt", "card 1\n", "refs/heads/sprint/s1/c1")
	c2 := commitBranch_internal(t, runGit, workDir, "c2", baseSHA, "c2.txt", "card 2\n", "refs/heads/sprint/s1/c2")
	other := commitBranch_internal(t, runGit, workDir, "other", baseSHA, "other.txt", "other stream\n", "")

	client := New(Config{AuthorName: "Test Author", AuthorEmail: "test@mas-bandwidth.com"})
	ff := newFakeForge_internal()
	ff.allGreen = true
	moved := false
	ff.OnCall = func(op string) {
		if op == "Checks" && !moved {
			moved = true
			runGit(workDir, "push", "origin", other+":refs/heads/v2")
		}
	}
	ff.OnMerge = func(sha string) (string, error) {
		runGit(lander, "push", "origin", sha+":refs/heads/v2")
		return sha, nil
	}
	req := LandRequest{RepoDir: lander, Repo: "o/r", Stream: "s1", Base: "v2", Heads: []string{c1, c2}, Branches: []string{"sprint/s1/c1", "sprint/s1/c2"}}
	first, err := LandPass(ctx, client, ff, req)
	if err != nil || first.Outcome != OutcomeBaseMoved {
		t.Fatalf("pass 1 = %+v err=%v, want base_moved", first, err)
	}
	second, err := LandPass(ctx, client, ff, req)
	if err != nil || second.Outcome != OutcomeLanded || second.BaseTip != other || second.Rung != 2 {
		t.Fatalf("pass 2 = %+v err=%v, want landed on %s at rung 2", second, err, other)
	}
	for _, h := range []string{c1, c2, other} {
		if ok, err := client.IsAncestor(ctx, lander, h, second.MergeSHA); err != nil || !ok {
			t.Fatalf("%s is not in the landed %s (err %v)", h, second.MergeSHA, err)
		}
	}
	again, err := LandPass(ctx, New(Config{AuthorName: "Test Author", AuthorEmail: "test@mas-bandwidth.com"}), ff, req)
	if err != nil || again.Outcome != OutcomeLanded || len(again.LandedHeads) != 2 {
		t.Fatalf("a pass after landing = %+v err=%v, want landed (every head on base)", again, err)
	}
}

// TestFunctionalCredentialEnvAnswersGitHubOnly is the same against the git
// on PATH (`git credential fill`), with a ~/.gitconfig whose github.com and
// plain helpers would answer: github.com gets the token, no other host
// gets anything.
func TestFunctionalCredentialEnvAnswersGitHubOnly(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	cfg := "[credential \"https://github.com\"]\n\thelper = \"!f() { echo password=V1; }; f\"\n[credential]\n\thelper = \"!f() { echo password=PLAIN; }; f\"\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	fill := func(host string) string {
		cmd := exec.Command("git", "credential", "fill")
		cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1"}, CredentialEnv("tok-fixture", "gh")...)
		cmd.Stdin = strings.NewReader("protocol=https\nhost=" + host + "\n\n")
		out, _ := cmd.CombinedOutput() // a host with no helper fails to fill: its output is what is checked
		return string(out)
	}
	if out := fill("github.com"); !strings.Contains(out, "password=tok-fixture\n") {
		t.Fatalf("github.com: %q", out)
	}
	for _, host := range []string{"evil.example", "github.com.evil.example"} {
		if out := fill(host); strings.Contains(out, "password=") {
			t.Fatalf("%s was answered: %q", host, out)
		}
	}
}
