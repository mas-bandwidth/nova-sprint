package forge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestFunctionalScratchForge exercises Client against a real GitHub repository using
// non-the owner credentials (e.g. friendF-claude via NOVA_FORGE_TEST_TOKEN).
// When NOVA_FORGE_SCRATCH_WRITE=1 is also set (for a scratch repo with a write-scoped token),
// it pushes a throwaway branch scratch/forge-<id>, opens a PR, reads its CI status, and closes
// the PR and deletes the branch in cleanup.
func TestFunctionalScratchForge(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_FORGE_SCRATCH_TEST") != "1" {
		t.Skip("skipping live GitHub scratch test: set NOVA_FORGE_SCRATCH_TEST=1 to run")
	}
	client, repo, base, login := newScratchClient_internal()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stdout, _, err := client.runGH_internal(ctx, "api", fmt.Sprintf("repos/%s/git/ref/heads/%s", repo, base), "--jq", ".object.sha")
	if err != nil {
		t.Fatalf("lookup base %s sha: %v", base, err)
	}
	baseSHA := strings.TrimSpace(stdout)

	if _, _, err := client.PR(ctx, repo, "scratch/nonexistent-probe", base); err != nil {
		t.Fatalf("PR query: %v", err)
	}
	rollups, err := client.Checks(ctx, repo, []string{baseSHA})
	if err != nil {
		t.Fatalf("Checks(%s): %v", baseSHA, err)
	}
	r, ok := rollups[baseSHA]
	if !ok {
		t.Fatalf("missing rollup for %s", baseSHA)
	}
	t.Logf("live GitHub Checks(%s@%s) as %s: state=%s reason=%q", repo, baseSHA, login, r.State, r.Reason)

	if os.Getenv("NOVA_FORGE_SCRATCH_WRITE") == "1" {
		verifyScratchPRRoundTrip_internal(t, ctx, client, repo, base, baseSHA)
	}
}

func newScratchClient_internal() (*Client, string, string, string) {
	login := os.Getenv("NOVA_FORGE_TEST_LOGIN")
	if login == "" {
		login = "friendF-claude"
	}
	repo := os.Getenv("NOVA_FORGE_SCRATCH_REPO")
	if repo == "" {
		repo = "mas-bandwidth/nova-sprint"
	}
	base := os.Getenv("NOVA_FORGE_SCRATCH_BASE")
	if base == "" {
		base = "v2"
	}
	ghBin := os.Getenv("NOVA_FORGE_GH_BIN")
	if ghBin == "" {
		ghBin = "gh"
	}
	c := New(Config{
		Identity: Identity{
			Name:  "scratch-test",
			Token: os.Getenv("NOVA_FORGE_TEST_TOKEN"),
		},
		Binary:  ghBin,
		Timeout: 30 * time.Second,
	})
	return c, repo, base, login
}

func verifyScratchPRRoundTrip_internal(t *testing.T, ctx context.Context, client *Client, repo, base, baseSHA string) {
	t.Helper()
	branch, sha := pushScratchBranch_internal(t, ctx, client, repo, base, baseSHA)
	defer cleanupScratchBranch_internal(t, client, repo, branch)

	pr, err := client.OpenPR(ctx, repo, branch, base, "scratch: forge functional probe "+branch, "Automated throwaway PR.")
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	defer func() {
		_, _, _ = client.runGH_internal(context.Background(), "pr", "close", fmt.Sprintf("%d", pr.Number), "--repo", repo)
	}()

	found, exists, err := client.PR(ctx, repo, branch, base)
	if err != nil || !exists || found.Number != pr.Number {
		t.Fatalf("PR lookup = (%+v, %v, %v), want PR #%d", found, exists, err, pr.Number)
	}
	prRollups, err := client.Checks(ctx, repo, []string{sha})
	if err != nil {
		t.Fatalf("Checks(%s): %v", sha, err)
	}
	t.Logf("live scratch PR #%d (%s) check state at %s: %s", pr.Number, pr.URL, sha, prRollups[sha].State)
}

func pushScratchBranch_internal(t *testing.T, ctx context.Context, c *Client, repo, _, baseSHA string) (string, string) {
	t.Helper()
	branch := fmt.Sprintf("scratch/forge-%d", time.Now().UnixNano())
	tmp := t.TempDir()
	runGit := func(args ...string) string {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = tmp
		cmd.Env = append(c.env_internal(),
			"GIT_AUTHOR_NAME=Rowan Scratch",
			"GIT_AUTHOR_EMAIL=friendF@mas-bandwidth.com",
			"GIT_COMMITTER_NAME=Rowan Scratch",
			"GIT_COMMITTER_EMAIL=friendF@mas-bandwidth.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
		return strings.TrimSpace(string(out))
	}
	remoteURL := fmt.Sprintf("https://github.com/%s.git", repo)
	if c.cfg.Identity.Token != "" {
		remoteURL = fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", c.cfg.Identity.Token, repo)
	}
	runGit("init")
	runGit("remote", "add", "origin", remoteURL)
	runGit("fetch", "--depth=1", "origin", baseSHA)
	runGit("checkout", "-b", branch, "FETCH_HEAD")
	runGit("commit", "--allow-empty", "-m", "scratch: forge functional probe")
	headSHA := runGit("rev-parse", "HEAD")
	runGit("push", "origin", "HEAD:refs/heads/"+branch)
	return branch, headSHA
}

func cleanupScratchBranch_internal(t *testing.T, c *Client, repo, branch string) {
	t.Helper()
	_, _, err := c.runGH_internal(context.Background(), "api", "-X", "DELETE", fmt.Sprintf("repos/%s/git/refs/heads/%s", repo, branch))
	if err != nil {
		t.Logf("cleanup branch %s warning: %v", branch, err)
	}
}
