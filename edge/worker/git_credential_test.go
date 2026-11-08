package worker

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
)

const gitTokenFixture = "gh-gate-token-fixture"

// workerGitCalls drives every worker git call that reaches the remote
// (clone, fetch, ls-remote, push) through g on a fake exec, and returns
// those calls.
func workerGitCalls_internal(t *testing.T, g *OSGitOps, fx *edge.FakeExec) []edge.FakeCall {
	t.Helper()
	fx.On("git", func(c edge.FakeCall) edge.FakeOutcome {
		args := c.Cmd.Argv[5:] // after git -c hooksPath -c fsmonitor
		switch {
		case args[0] == "clone":
			_ = os.MkdirAll(args[len(args)-1], 0o755)
		case args[0] == "remote" && args[1] == "get-url":
			return edge.FakeOutcome{Stdout: "https://github.com/o/r.git\n"}
		case args[0] == "rev-parse":
			return edge.FakeOutcome{Stdout: strings.Repeat("a", 40) + "\n"}
		}
		return edge.FakeOutcome{}
	})
	ctx := context.Background()
	j := Job{Repo: "o/r", Forge: "github", Kind: "work", Branch: "sprint/s/p1", BaseSHA: strings.Repeat("b", 40), Consumer: "p1.w1",
		WorktreeDir: filepath.Join(t.TempDir(), "job", "repo"), CommitName: testCommitName, CommitEmail: testCommitEmail}
	if _, err := g.StageWorktree(ctx, j); err != nil {
		t.Fatal(err)
	}
	if err := g.Fetch(ctx, j); err != nil {
		t.Fatal(err)
	}
	if _, err := g.HasHead(ctx, "https://github.com/o/r.git", j.Branch, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	if err := g.Push(ctx, j.WorktreeDir, j.Branch, strings.Repeat("a", 40), strings.Repeat("b", 40)); err != nil {
		t.Fatal(err)
	}
	var remote []edge.FakeCall
	verbs := map[string]bool{}
	for _, c := range fx.Calls() {
		if v := c.Cmd.Argv[5]; v == "clone" || v == "fetch" || v == "ls-remote" || v == "push" {
			remote, verbs[v] = append(remote, c), true
		}
	}
	if len(verbs) != 4 {
		t.Fatalf("the worker's remote git verbs reached are %v, want clone, fetch, ls-remote and push", verbs)
	}
	return remote
}

// The v1 credential helper (~/.local/bin/nova-push-credential, named by
// ~/.gitconfig's credential.https://github.com.helper) is gone from the
// fleet: every git call the worker makes carries edge/git's credential
// environment (git.CredentialEnv), which resets credential.helper before it
// names the one credential, so the host's config is never asked. With
// GH_GATE_TOKEN the token answers, in git's environment only, never argv.
func TestWorkerGitCarriesTheOneCredential(t *testing.T) {
	t.Parallel()
	fx := edge.NewFakeExec()
	g := &OSGitOps{WorkRoot: filepath.Join(t.TempDir(), "clones"), MirrorRoot: filepath.Join(t.TempDir(), "mirror"), Exec: fx,
		Token: gitTokenFixture, GitHubCredential: "gh"}
	for _, c := range workerGitCalls_internal(t, g, fx) {
		env := c.Cmd.Env
		if !c.Cmd.CleanEnv {
			t.Fatalf("%q runs with the worker's whole environment, ~/.gitconfig's helper and GH_GATE_TOKEN included", c.Cmd.Argv)
		}
		cred := git.CredentialEnv(gitTokenFixture, "gh")
		if len(env) < len(cred) || !slices.Equal(env[len(env)-len(cred):], cred) {
			t.Fatalf("%q env %q does not end with edge/git's credential env %q", c.Cmd.Argv, env, cred)
		}
		reset := slices.Index(env, "GIT_CONFIG_KEY_0=credential.helper")
		if reset < 0 || !slices.Contains(env, "GIT_CONFIG_VALUE_0=") || !slices.Contains(env, "GIT_CONFIG_COUNT=2") {
			t.Fatalf("%q env %q does not reset credential.helper first", c.Cmd.Argv, env)
		}
		if !slices.Contains(env, "NOVA_SPRINT_GIT_TOKEN="+gitTokenFixture) {
			t.Fatalf("%q env %q does not answer with the token", c.Cmd.Argv, env)
		}
		for _, kv := range env {
			if strings.HasPrefix(kv, forge.TokenEnv+"=") {
				t.Fatalf("%q env carries %s itself: %q", c.Cmd.Argv, forge.TokenEnv, env)
			}
		}
		if strings.Contains(strings.Join(c.Cmd.Argv, " "), gitTokenFixture) {
			t.Fatalf("the token is on git's argv: %q", c.Cmd.Argv)
		}
	}
}

// No token: gh's login answers for https://github.com, the github-scoped
// helper reset first; an OSGitOps that names no credential still never
// falls back to the host's config (gh is the default).
func TestWorkerGitWithoutTokenUsesGh(t *testing.T) {
	t.Parallel()
	for _, named := range []string{"gh", ""} {
		fx := edge.NewFakeExec()
		g := &OSGitOps{WorkRoot: filepath.Join(t.TempDir(), "clones"), MirrorRoot: filepath.Join(t.TempDir(), "mirror"), Exec: fx, GitHubCredential: named}
		for _, c := range workerGitCalls_internal(t, g, fx) {
			env := c.Cmd.Env
			cred := git.CredentialEnv("", "gh")
			if len(cred) == 0 || len(env) < len(cred) || !slices.Equal(env[len(env)-len(cred):], cred) {
				t.Fatalf("GitHubCredential %q: %q env %q does not end with gh's credential env %q", named, c.Cmd.Argv, env, cred)
			}
			if !slices.Contains(env, "GIT_CONFIG_KEY_0=credential.https://github.com.helper") || !slices.Contains(env, "GIT_CONFIG_VALUE_0=") {
				t.Fatalf("%q env %q does not reset the github helper first", c.Cmd.Argv, env)
			}
			if slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "NOVA_SPRINT_GIT_TOKEN=") }) {
				t.Fatalf("%q env %q carries a token when there is none", c.Cmd.Argv, env)
			}
		}
	}
}

// The token is the worker's git's alone: the harness's wall (RunHarness)
// and the gates' (RunTest, which starts from the worker's own environment)
// never carry it, nor git's credential env, in their environment or argv.
func TestWorkerGitTokenNeverReachesTheHarness(t *testing.T) { // unitlint: serial, it sets the process environment (t.Setenv): RunTest starts from os.Environ
	t.Setenv(forge.TokenEnv, gitTokenFixture)
	dir := t.TempDir()
	fx := cr2OKExec_internal()
	w := &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", NetDeny: true},
		LookPath: func(string) (string, error) { return "/usr/bin/claude", nil }}
	j := Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}
	leaked := append([]string{"PATH=/usr/bin", forge.TokenEnv + "=" + gitTokenFixture}, git.CredentialEnv(gitTokenFixture, "gh")...)
	if _, err := w.RunHarness(context.Background(), j, []string{"claude"}, leaked, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := w.RunTest(context.Background(), j, "go test ./...", time.Minute); err != nil {
		t.Fatal(err)
	}
	calls := fx.Calls()
	if len(calls) != 2 {
		t.Fatalf("want two wall runs, got %d", len(calls))
	}
	for _, c := range calls {
		all := strings.Join(append(slices.Clone(c.Cmd.Argv), c.Cmd.Env...), "\n")
		if !c.Cmd.CleanEnv || strings.Contains(all, gitTokenFixture) || strings.Contains(all, "GIT_CONFIG_") || strings.Contains(all, forge.TokenEnv) {
			t.Fatalf("the wall's run carries the git credential (clean env %t):\n%s", c.Cmd.CleanEnv, all)
		}
	}
}

// A worker started without GH_GATE_TOKEN refuses in one line naming the
// grant that gives its seat the key (nova-secrets grant NAME --to <seat>),
// the line `nova-sprint check` quotes from the unit's log (#672).
func TestWorkerRefusesWithoutGitToken(t *testing.T) {
	t.Parallel()
	none := func(string) (string, bool) { return "", false }
	err := (Config{Row: "amd-1", Host: "amd-1", Harness: "script", LookupEnv: none}).Check()
	want := "nova-secrets grant GH_GATE_TOKEN --to amd-1 --pins <dir> --store <dir> --key <file>"
	if err == nil || !strings.HasPrefix(err.Error(), "worker refused: GH_GATE_TOKEN is not in the worker's environment") || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "\n") {
		t.Fatalf("a worker with no git token: %v", err)
	}
	blank := func(string) (string, bool) { return "  ", true }
	if err := (Config{Row: "amd-1", Harness: "script", LookupEnv: blank}).Check(); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("a worker with a blank git token: %v", err)
	}
	token := func(k string) (string, bool) { return gitTokenFixture, k == forge.TokenEnv }
	if err := (Config{Row: "amd-1", Harness: "script", LookupEnv: token}).Check(); err != nil {
		t.Fatalf("a worker with its git token refused: %v", err)
	}
}
