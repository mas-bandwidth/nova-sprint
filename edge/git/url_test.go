package git

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

// TestCloneURL is defect 21: an <org>/<repo> with forge github is GitHub's
// https address; a clone address passes as it is; a hand stream's repo is
// its own; a form a forge cannot address is refused naming the form.
func TestCloneURL(t *testing.T) {
	t.Parallel()
	cases := []struct{ repo, forge, want, refused string }{
		{repo: "mas-bandwidth/nova-tools", forge: "github", want: "https://github.com/mas-bandwidth/nova-tools.git"},
		{repo: "mas-bandwidth/nova-tools.git", forge: "github", want: "https://github.com/mas-bandwidth/nova-tools.git"},
		{repo: " o/r ", forge: "github", want: "https://github.com/o/r.git"},
		{repo: "https://example.invalid/r.git", forge: "github", want: "https://example.invalid/r.git"},
		{repo: "git@example.com:r.git", forge: "github", want: "git@example.com:r.git"},
		{repo: "example.com:org/r.git", forge: "github", want: "example.com:org/r.git"},
		{repo: "/srv/git/r.git", forge: "github", want: "/srv/git/r.git"},
		{repo: "./r", forge: "", want: "./r"},
		{repo: "o/r", forge: "", want: "o/r"},
		{repo: "nova-tools", forge: "github", refused: "neither <org>/<repo> nor a clone URL"},
		{repo: "a/b/c", forge: "github", refused: "neither <org>/<repo> nor a clone URL"},
		{repo: "../r", forge: "github", want: "../r"},
		{repo: "o/..", forge: "github", refused: "neither <org>/<repo>"},
		{repo: "o/r", forge: "gitlab", refused: "the forges are github and none"},
		{repo: "", forge: "github", refused: "repository is empty"},
	}
	for _, c := range cases {
		got, err := CloneURL(c.repo, c.forge)
		switch {
		case c.refused != "" && (err == nil || !strings.Contains(err.Error(), c.refused)):
			t.Errorf("CloneURL(%q, %q) = %q, %v; want a refusal naming %q", c.repo, c.forge, got, err, c.refused)
		case c.refused == "" && (err != nil || got != c.want):
			t.Errorf("CloneURL(%q, %q) = %q, %v; want %q", c.repo, c.forge, got, err, c.want)
		}
	}
}

// TestTipIsOneLsRemoteOfTheAddress: no clone, refs/heads/ for a bare
// branch, exit 2 is absent, any other failure carries git's words.
func TestTipIsOneLsRemoteOfTheAddress(t *testing.T) {
	t.Parallel()
	ex := edge.NewFakeExec()
	sha := strings.Repeat("a", 40)
	ex.Enqueue("git", edge.FakeOutcome{Stdout: sha + "\trefs/heads/main\n"}, edge.FakeOutcome{ExitCode: 2},
		edge.FakeOutcome{ExitCode: 128, Stderr: "fatal: repository 'x' does not exist"})
	c := New(Config{Exec: ex})
	url := "https://github.com/o/r.git"
	got, ok, err := c.Tip(context.Background(), url, "main")
	if err != nil || !ok || got != sha {
		t.Fatalf("Tip = %q %v %v", got, ok, err)
	}
	if _, ok, err := c.Tip(context.Background(), url, "refs/heads/gone"); ok || err != nil {
		t.Fatalf("an absent branch: %v %v", ok, err)
	}
	if _, _, err := c.Tip(context.Background(), url, "main"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("a failed ls-remote carries git's words: %v", err)
	}
	calls := ex.Calls()
	want := [][]string{{"git", "ls-remote", "--exit-code", url, "refs/heads/main"}, {"git", "ls-remote", "--exit-code", url, "refs/heads/gone"}}
	for i, w := range want {
		if !slices.Equal(calls[i].Cmd.Argv, w) || calls[i].Cmd.Dir != "" {
			t.Fatalf("call %d: %v in %q, want %v and no directory", i, calls[i].Cmd.Argv, calls[i].Cmd.Dir, w)
		}
	}
}

// TestGitHubCredentialIsGhAndNamesNoSecret: with gh set, git's only
// github.com helper is gh as the host's login (the host's list cleared
// first; every GitHub credential of the process unset for gh, directive
// 59); the environment names no token; unset, git's own configuration
// answers.
func TestGitHubCredentialIsGhAndNamesNoSecret(t *testing.T) {
	t.Parallel()
	ex := edge.NewFakeExec()
	ex.Enqueue("git", edge.FakeOutcome{ExitCode: 2}, edge.FakeOutcome{ExitCode: 2})
	_, _, _ = New(Config{Exec: ex, GitHubCredential: "gh"}).Tip(context.Background(), "https://github.com/o/r.git", "main")
	_, _, _ = New(Config{Exec: ex}).Tip(context.Background(), "https://github.com/o/r.git", "main")
	calls := ex.Calls()
	env := calls[0].Cmd.Env
	for _, kv := range []string{"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=credential.https://github.com.helper", "GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=credential.https://github.com.helper", "GIT_CONFIG_VALUE_1=!env -u GH_TOKEN -u GITHUB_TOKEN -u GH_ENTERPRISE_TOKEN -u GITHUB_ENTERPRISE_TOKEN -u GH_CONFIG_DIR gh auth git-credential"} {
		if !slices.Contains(env, kv) {
			t.Fatalf("env lacks %q: %v", kv, env)
		}
	}
	for _, kv := range append(env, calls[1].Cmd.Env...) {
		if strings.Contains(kv, "TOKEN=") { // a value; the helper only unsets them
			t.Fatalf("the git environment names a secret: %q", kv)
		}
	}
	if slices.ContainsFunc(calls[1].Cmd.Env, func(kv string) bool { return strings.HasPrefix(kv, "GIT_CONFIG_") }) {
		t.Fatalf("no identity, yet the helper was set: %v", calls[1].Cmd.Env)
	}
}
