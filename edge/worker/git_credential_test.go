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
		args := c.Cmd.Argv[gitArg0_internal:] // after gitPrefix_internal
		switch {
		case args[0] == "clone":
			_ = os.MkdirAll(args[len(args)-1], 0o755)
		case args[0] == "remote" && args[1] == "get-url":
			return edge.FakeOutcome{Stdout: "https://github.com/o/r.git\n"}
		case args[0] == "config" && args[1] == "--local":
			return edge.FakeOutcome{Stdout: "remote.origin.url\nhttps://github.com/o/r.git\x00"}
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
	writeStagedAlternates_internal(t, g, j.WorktreeDir, "r") // what the real clone --shared writes
	if err := g.Fetch(ctx, j); err != nil {
		t.Fatal(err)
	}
	const url = "https://github.com/o/r.git"
	if _, err := g.OriginHasHead(ctx, j.WorktreeDir, url, j.Branch, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	if _, err := g.RebaseOntoStream(ctx, j.WorktreeDir, url, "sprint/s/group"); err != nil {
		t.Fatal(err)
	}
	if err := g.Push(ctx, j.WorktreeDir, url, j.Branch, strings.Repeat("a", 40), strings.Repeat("b", 40)); err != nil {
		t.Fatal(err)
	}
	var remote []edge.FakeCall
	verbs := map[string]bool{}
	for _, c := range fx.Calls() {
		if v := c.Cmd.Argv[gitArg0_internal]; v == "clone" || v == "fetch" || v == "ls-remote" || v == "push" {
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
		if !slices.Contains(env, "GIT_CONFIG_KEY_0=credential.helper") || !slices.Contains(env, "GIT_CONFIG_VALUE_0=") || !slices.Contains(env, "GIT_CONFIG_COUNT=2") ||
			!slices.Contains(env, "GIT_CONFIG_KEY_1=credential.https://github.com.helper") {
			t.Fatalf("%q env %q does not reset credential.helper first and scope the token to github.com", c.Cmd.Argv, env)
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
			if !slices.Contains(env, "GIT_CONFIG_KEY_0=credential.helper") || !slices.Contains(env, "GIT_CONFIG_VALUE_0=") || !slices.Contains(env, "GIT_CONFIG_KEY_1=credential.https://github.com.helper") {
				t.Fatalf("%q env %q does not reset credential.helper first and scope gh to github.com", c.Cmd.Argv, env)
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
	want := "nova-secrets grant GH_GATE_TOKEN --to amd-1 --pins <dir> --store <dir> --key <file>, push the secrets store (grant does not)"
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

// guardFake_internal is a fake git whose worktree reads origin as origin
// and its push URL as pushURL, whose local config (.git/config) lists
// local and whose whole config lists local and global ("key\nvalue"
// entries); it records every git argv after the -c pairs.
func guardFake_internal(origin, pushURL string, local, global []string) (*edge.FakeExec, *[]string) {
	fx := edge.NewFakeExec()
	var argvs []string
	fx.On("git", func(c edge.FakeCall) edge.FakeOutcome {
		args := c.Cmd.Argv[gitArg0_internal:]
		argvs = append(argvs, strings.Join(args, " "))
		switch {
		case len(args) > 3 && args[0] == "remote" && args[2] == "--push":
			return edge.FakeOutcome{Stdout: pushURL + "\n"}
		case args[0] == "remote":
			return edge.FakeOutcome{Stdout: origin + "\n"}
		case args[0] == "config" && args[1] == "--local":
			return edge.FakeOutcome{Stdout: strings.Join(local, "\x00")}
		case args[0] == "config":
			return edge.FakeOutcome{Stdout: strings.Join(append(slices.Clone(local), global...), "\x00")}
		case args[0] == "rev-parse":
			return edge.FakeOutcome{Stdout: strings.Repeat("a", 40) + "\n"}
		}
		return edge.FakeOutcome{}
	})
	return fx, &argvs
}

// stagedLocal_internal is a work worktree's .git/config as staging leaves
// it (git 2.56: clone --shared, remote set-url, checkout -B, the identity).
var stagedLocal_internal = []string{"core.repositoryformatversion\n0", "core.filemode\ntrue", "core.bare\nfalse", "core.logallrefupdates\ntrue",
	"core.ignorecase\ntrue", "core.precomposeunicode\ntrue", "remote.origin.url\nhttps://github.com/o/r.git",
	"remote.origin.fetch\n+refs/heads/*:refs/remotes/origin/*", "branch.main.remote\norigin", "branch.main.merge\nrefs/heads/main",
	"user.name\nSprint Bot", "user.email\n1+sprint-bot@users.noreply.example.test"}

// #680 cold reads: the worktree is under the job directory, which the
// harness writes. Its .git/config may hold a remote named after the clone
// URL (git push <url> then goes to that remote's url), http.* that sends
// the token to another server (sslVerify off and a proxy; a scoped
// http.https://github.com.* beats any -c), a filter or core.sshCommand that
// runs a command in the worker's git, or an origin, push URL or insteadOf
// pointed elsewhere. Every worker git call after the harness (push, the
// rebase's fetch, OriginHasHead, IsDirty, CommitAll, HeadSHA) refuses it,
// naming the key, before git runs anything else there; an untouched
// worktree is reached by its clone URL, never origin.
func TestWorkerGitRefusesATamperedWorktree(t *testing.T) {
	t.Parallel()
	const url = "https://github.com/o/r.git"
	sha, lease := strings.Repeat("a", 40), strings.Repeat("b", 40)
	ctx := context.Background()
	with := func(extra ...string) []string { return append(slices.Clone(stagedLocal_internal), extra...) }
	workRoot := t.TempDir()
	wt := filepath.Join(t.TempDir(), "repo")
	writeStagedAlternates_internal(t, &OSGitOps{WorkRoot: workRoot}, wt, "r")
	type tamper struct {
		origin, push  string
		local, global []string
		names         string // in the refusal
	}
	cases := map[string]tamper{
		"a remote named after the clone URL": {url, url, with("remote." + url + ".url\n/elsewhere"), nil, "remote." + url + ".url"},
		"http.sslVerify and http.proxy":      {url, url, with("http.https://github.com.sslverify\nfalse", "http.proxy\nhttp://evil.example:8080"), nil, "http.https://github.com.sslverify"},
		"http.curloptResolve":                {url, url, with("http.curloptresolve\ngithub.com:443:203.0.113.9"), nil, "http.curloptresolve"},
		"a filter":                           {url, url, with("filter.x.clean\ntouch /tmp/pwned"), nil, "filter.x.clean"},
		"core.sshCommand":                    {url, url, with("core.sshcommand\ntouch /tmp/pwned"), nil, "core.sshcommand"},
		"core.fsmonitor":                     {url, url, with("core.fsmonitor\ntouch /tmp/pwned"), nil, "core.fsmonitor"},
		"an include":                         {url, url, with("include.path\n/tmp/evil.config"), nil, "include.path"},
		"a branch remote elsewhere":          {url, url, with("branch.x.remote\nhttps://evil.example/o/r.git"), nil, "branch.x.remote"},
		"a local insteadOf":                  {url, url, with("url.https://evil.example/.insteadof\nhttps://github.com/"), nil, "url.https://evil.example/.insteadof"},
		"a global insteadOf rewriting it":    {url, url, stagedLocal_internal, []string{"url.https://github.com/other/.pushinsteadof\nhttps://github.com/o/"}, "rewrites the job's clone URL"},
		"origin elsewhere":                   {"https://evil.example/o/r.git", url, stagedLocal_internal, nil, "not the job's clone URL"},
		"origin another repo":                {"https://github.com/other/r.git", url, stagedLocal_internal, nil, "not the job's clone URL"},
		"push URL elsewhere":                 {url, "https://evil.example/o/r.git", stagedLocal_internal, nil, "not the job's clone URL"},
	}
	calls := map[string]func(g *OSGitOps) error{
		"push":  func(g *OSGitOps) error { return g.Push(ctx, wt, url, "sprint/s/p1", sha, lease) },
		"fetch": func(g *OSGitOps) error { _, err := g.RebaseOntoStream(ctx, wt, url, "sprint/s/group"); return err },
		"has head": func(g *OSGitOps) error {
			_, err := g.OriginHasHead(ctx, wt, url, "sprint/s/p1", sha)
			return err
		},
		"dirty":  func(g *OSGitOps) error { _, err := g.IsDirty(ctx, wt); return err },
		"commit": func(g *OSGitOps) error { _, err := g.CommitAll(ctx, wt, "m"); return err },
		"head":   func(g *OSGitOps) error { _, err := g.HeadSHA(ctx, wt); return err },
	}
	remoteOnly := map[string]bool{"push": true, "fetch": true, "has head": true}
	for name, c := range cases {
		configOnly := c.origin == url && c.push == url && c.global == nil
		for verb, call := range calls {
			if !configOnly && !remoteOnly[verb] {
				continue // origin and insteadOf matter only to a call that reaches the remote
			}
			fx, argvs := guardFake_internal(c.origin, c.push, c.local, c.global)
			err := call(&OSGitOps{Exec: fx, Token: gitTokenFixture, WorkRoot: workRoot})
			ran := slices.ContainsFunc(*argvs, func(a string) bool {
				return !strings.HasPrefix(a, "config") && !strings.HasPrefix(a, "remote get-url") && !strings.HasPrefix(a, "ls-files")
			})
			if err == nil || !strings.Contains(err.Error(), "git refused:") || !strings.Contains(err.Error(), c.names) || ran {
				t.Fatalf("%s, %s: err %v, git ran %q", name, verb, err, *argvs)
			}
		}
	}
	fx, argvs := guardFake_internal(url, url, stagedLocal_internal, []string{"url.git@github.com:.insteadof\nhttps://gitlab.example/"})
	g := &OSGitOps{Exec: fx, Token: gitTokenFixture, WorkRoot: workRoot}
	for verb, call := range calls {
		if err := call(g); err != nil {
			t.Fatalf("an untouched worktree, %s: %v", verb, err)
		}
	}
	for _, want := range []string{"push --no-recurse-submodules --force-with-lease=refs/heads/sprint/s/p1:" + lease + " " + url + " " + sha + ":refs/heads/sprint/s/p1",
		"fetch --quiet --no-recurse-submodules " + url + " sprint/s/group", "ls-remote " + url + " refs/heads/sprint/s/p1", "status --porcelain", "add -A"} {
		if !slices.Contains(*argvs, want) {
			t.Fatalf("an untouched worktree: no %q in %q", want, *argvs)
		}
	}
	if slices.ContainsFunc(*argvs, func(a string) bool { return strings.Contains(a, " origin ") && !strings.HasPrefix(a, "remote") }) {
		t.Fatalf("a remote call named origin: %q", *argvs)
	}
	for _, c := range fx.Calls() {
		if !slices.Equal(c.Cmd.Argv[:gitArg0_internal], []string{"git", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "submodule.recurse=false", "-c", "diff.ignoreSubmodules=all"}) {
			t.Fatalf("a worker git call runs the worktree's hooks or fsmonitor, or recurses into submodules: %q", c.Cmd.Argv)
		}
	}
	if err := (&OSGitOps{Exec: fx, WorkRoot: workRoot}).Push(ctx, wt, "", "sprint/s/p1", sha, lease); err == nil || !strings.Contains(err.Error(), "names no clone URL") {
		t.Fatalf("a job with no clone URL: %v", err)
	}
}

// The clone URL a job pushes to is its take receipt's, kept outside the
// job directory: a job.json the harness rewrote to another repo is refused
// before any push, and a job with no kept URL is too.
func TestWorkerGitRefusesATamperedJobRecord(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	git := newFakeGit_internal()
	r, err := NewRunner(Runner{Host: "b1", RootDir: root, Git: git, Now: testClock_internal(), Sleep: noSleep_internal})
	if err != nil {
		t.Fatal(err)
	}
	rec := JobRecord{Consumer: "p1.w1", Kind: "work", Repo: "o/r", Forge: "github", Branch: "sprint/s/p1", Gen: 1}
	if err := r.keepRemote_internal(TakeInfo{Consumer: rec.Consumer, Repo: rec.Repo, Forge: rec.Forge}); err != nil {
		t.Fatal(err)
	}
	if got, err := r.jobRemote_internal(rec); err != nil || got != "https://github.com/o/r.git" {
		t.Fatalf("the receipt's clone URL: %q, %v", got, err)
	}
	wt := filepath.Join(root, "jobs", rec.Consumer, "repo")
	for name, tampered := range map[string]JobRecord{
		"another repo": {Consumer: rec.Consumer, Kind: "work", Repo: "other/r", Forge: "github", Branch: rec.Branch, Gen: 1},
		"another host": {Consumer: rec.Consumer, Kind: "work", Repo: "https://evil.example/o/r.git", Forge: "github", Branch: rec.Branch, Gen: 1},
		"no repo":      {Consumer: rec.Consumer, Kind: "work", Forge: "github", Branch: rec.Branch, Gen: 1},
	} {
		if _, err := r.jobRemote_internal(tampered); err == nil || !strings.Contains(err.Error(), "is not the take receipt's clone URL") {
			t.Fatalf("%s: %v", name, err)
		}
		if err := r.pushBranch_internal(context.Background(), wt, &tampered, strings.Repeat("a", 40)); err == nil || !strings.Contains(err.Error(), "is not the take receipt's clone URL") {
			t.Fatalf("%s: the push was not refused: %v", name, err)
		}
	}
	if pushes := git.pushes(); len(pushes) != 0 {
		t.Fatalf("a tampered record was pushed: %+v", pushes)
	}
	if _, err := r.jobRemote_internal(JobRecord{Consumer: "p2.w1", Repo: "o/r", Forge: "github"}); err == nil || !strings.Contains(err.Error(), "no clone URL kept") {
		t.Fatalf("a job with no kept clone URL: %v", err)
	}
}

// writeStagedRecord_internal is a job a runner before this one took and
// staged: its record in jobDir (<root>/jobs/<consumer>) and the clone URL
// its take kept outside it (keepRemote_internal).
func writeStagedRecord_internal(t *testing.T, jobDir string, rec JobRecord) {
	t.Helper()
	if err := WriteJobRecord(jobDir, rec); err != nil {
		t.Fatal(err)
	}
	r := &Runner{RootDir: filepath.Dir(filepath.Dir(jobDir))}
	if err := r.keepRemote_internal(TakeInfo{Consumer: rec.Consumer, Repo: rec.Repo, Forge: rec.Forge}); err != nil {
		t.Fatal(err)
	}
}

// gitArg0_internal is the index of a worker git call's own arguments, after
// gitPrefix_internal.
var gitArg0_internal = len(gitPrefix_internal)

// writeStagedAlternates_internal makes wt's .git/objects/info/alternates
// as staging writes it for the work clone <g.WorkRoot>/<name>.
func writeStagedAlternates_internal(t *testing.T, g *OSGitOps, wt, name string) {
	t.Helper()
	info := filepath.Join(wt, ".git", "objects", "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(info, "alternates"), []byte(filepath.Join(g.WorkRoot, name, ".git", "objects")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// #680 round 3: a repository nested in the worktree (a .git below its root,
// or a gitlink in its index) makes the worker's git run that repository's
// config, its filters and http settings included, outside the wall; and
// alternates the harness rewrote feed git objects staging never fetched.
// Each is refused before git runs anything but the guard's reads (config,
// ls-files), for every call after the harness.
func TestWorkerGitRefusesANestedRepoOrNewAlternates(t *testing.T) {
	t.Parallel()
	const url = "https://github.com/o/r.git"
	ctx := context.Background()
	sha := strings.Repeat("a", 40)
	calls := map[string]func(g *OSGitOps, wt string) error{
		"push": func(g *OSGitOps, wt string) error { return g.Push(ctx, wt, url, "sprint/s/p1", sha, "") },
		"fetch": func(g *OSGitOps, wt string) error {
			_, err := g.RebaseOntoStream(ctx, wt, url, "sprint/s/group")
			return err
		},
		"has head": func(g *OSGitOps, wt string) error {
			_, err := g.OriginHasHead(ctx, wt, url, "sprint/s/p1", sha)
			return err
		},
		"dirty":  func(g *OSGitOps, wt string) error { _, err := g.IsDirty(ctx, wt); return err },
		"commit": func(g *OSGitOps, wt string) error { _, err := g.CommitAll(ctx, wt, "m"); return err },
		"head":   func(g *OSGitOps, wt string) error { _, err := g.HeadSHA(ctx, wt); return err },
	}
	type tamper struct {
		prep    func(t *testing.T, wt, workRoot string)
		index   string // ls-files --stage -z output
		names   string
		gitRuns bool // the guard reads config and the index before it refuses
	}
	write := func(t *testing.T, path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]tamper{
		"a nested .git directory": {prep: func(t *testing.T, wt, _ string) {
			write(t, filepath.Join(wt, "sub", ".git", "config"), "[filter \"x\"]\n\tclean = touch /tmp/pwned\n")
			write(t, filepath.Join(wt, "sub", ".gitattributes"), "* filter=x\n")
		}, names: "nested repository at " + "SUB"},
		"a nested .git file": {prep: func(t *testing.T, wt, _ string) {
			write(t, filepath.Join(wt, "deep", "er", ".git"), "gitdir: /elsewhere\n")
		}, names: "nested repository at"},
		"a gitlink in the index": {index: "100644 " + sha + " 0\tREADME.md\x00160000 " + sha + " 0\tsub\x00", names: "gitlink (a submodule) at sub", gitRuns: true},
		"an alternates line added": {prep: func(t *testing.T, wt, workRoot string) {
			write(t, filepath.Join(wt, ".git", "objects", "info", "alternates"), filepath.Join(workRoot, "r", ".git", "objects")+"\n/tmp/evil/objects\n")
		}, names: "objects/info/alternates", gitRuns: true},
		"alternates elsewhere": {prep: func(t *testing.T, wt, _ string) {
			write(t, filepath.Join(wt, ".git", "objects", "info", "alternates"), "/tmp/evil/objects\n")
		}, names: "objects/info/alternates", gitRuns: true},
		"alternates removed": {prep: func(t *testing.T, wt, _ string) {
			if err := os.Remove(filepath.Join(wt, ".git", "objects", "info", "alternates")); err != nil {
				t.Fatal(err)
			}
		}, names: "objects/info/alternates", gitRuns: true},
	}
	for name, c := range cases {
		for verb, call := range calls {
			workRoot := t.TempDir()
			wt := filepath.Join(t.TempDir(), "repo")
			writeStagedAlternates_internal(t, &OSGitOps{WorkRoot: workRoot}, wt, "r")
			if c.prep != nil {
				c.prep(t, wt, workRoot)
			}
			fx, argvs := guardFake_internal(url, url, stagedLocal_internal, nil)
			fx.On("git", func(fc edge.FakeCall) edge.FakeOutcome { // the guard fake, with an index
				if fc.Cmd.Argv[gitArg0_internal] == "ls-files" {
					*argvs = append(*argvs, "ls-files")
					return edge.FakeOutcome{Stdout: c.index}
				}
				*argvs = append(*argvs, strings.Join(fc.Cmd.Argv[gitArg0_internal:], " "))
				if fc.Cmd.Argv[gitArg0_internal] == "config" {
					return edge.FakeOutcome{Stdout: strings.Join(stagedLocal_internal, "\x00")}
				}
				return edge.FakeOutcome{Stdout: url + "\n"}
			})
			err := call(&OSGitOps{Exec: fx, WorkRoot: workRoot, Token: gitTokenFixture}, wt)
			want := strings.Replace(c.names, "SUB", filepath.Join(wt, "sub", ".git"), 1)
			ranOther := slices.ContainsFunc(*argvs, func(a string) bool { return !strings.HasPrefix(a, "config --local") && a != "ls-files" })
			if err == nil || !strings.Contains(err.Error(), "git refused:") || !strings.Contains(err.Error(), want) || ranOther || !c.gitRuns && len(*argvs) != 0 {
				t.Fatalf("%s, %s: err %v, git ran %q", name, verb, err, *argvs)
			}
		}
	}
	// A gitlink that git add -A records (the tree about to be committed) is
	// refused before the commit.
	workRoot := t.TempDir()
	wt := filepath.Join(t.TempDir(), "repo")
	writeStagedAlternates_internal(t, &OSGitOps{WorkRoot: workRoot}, wt, "r")
	fx := edge.NewFakeExec()
	added := false
	var argvs []string
	fx.On("git", func(fc edge.FakeCall) edge.FakeOutcome {
		args := fc.Cmd.Argv[gitArg0_internal:]
		argvs = append(argvs, args[0])
		switch args[0] {
		case "config":
			return edge.FakeOutcome{Stdout: strings.Join(stagedLocal_internal, "\x00")}
		case "add":
			added = true
		case "ls-files":
			if added {
				return edge.FakeOutcome{Stdout: "160000 " + sha + " 0\tsub\x00"}
			}
		}
		return edge.FakeOutcome{}
	})
	if _, err := (&OSGitOps{Exec: fx, WorkRoot: workRoot}).CommitAll(ctx, wt, "m"); err == nil || !strings.Contains(err.Error(), "gitlink") || slices.Contains(argvs, "commit") {
		t.Fatalf("a gitlink added by add -A: err %v, git ran %q", err, argvs)
	}
}

// The token is on the network calls alone (clone, fetch, ls-remote,
// push), never on status, add, commit, rebase, rev-parse, config or
// ls-files; every call carries the no-hooks, no-fsmonitor, no-submodule
// flags, and fetch and push --no-recurse-submodules.
func TestWorkerGitTokenOnlyOnNetworkCalls(t *testing.T) {
	t.Parallel()
	fx := edge.NewFakeExec()
	g := &OSGitOps{WorkRoot: filepath.Join(t.TempDir(), "clones"), MirrorRoot: filepath.Join(t.TempDir(), "mirror"), Exec: fx, Token: gitTokenFixture}
	workerGitCalls_internal(t, g, fx)
	wt := ""
	for _, c := range fx.Calls() {
		if args := c.Cmd.Argv[gitArg0_internal:]; args[0] == "push" {
			wt = c.Cmd.Dir
		}
	}
	if _, err := g.IsDirty(context.Background(), wt); err != nil {
		t.Fatal(err)
	}
	if _, err := g.CommitAll(context.Background(), wt, "m"); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range fx.Calls() {
		args := c.Cmd.Argv[gitArg0_internal:]
		seen[args[0]] = true
		if !slices.Equal(c.Cmd.Argv[:gitArg0_internal], gitPrefix_internal) {
			t.Fatalf("%q lacks the worker's git flags", c.Cmd.Argv)
		}
		network := slices.Contains(networkVerbs_internal, args[0])
		token := slices.Contains(c.Cmd.Env, "NOVA_SPRINT_GIT_TOKEN="+gitTokenFixture)
		helper := slices.ContainsFunc(c.Cmd.Env, func(kv string) bool { return strings.HasPrefix(kv, "GIT_CONFIG_") })
		if network != token || network != helper {
			t.Fatalf("%q: network %t, token %t, helper %t", args, network, token, helper)
		}
		if (args[0] == "fetch" || args[0] == "push") && !slices.Contains(args, "--no-recurse-submodules") {
			t.Fatalf("%q recurses into submodules", args)
		}
	}
	for _, v := range []string{"clone", "fetch", "ls-remote", "push", "rebase", "rev-parse", "status", "add", "commit", "config", "ls-files"} {
		if !seen[v] {
			t.Fatalf("git %s was not reached: %v", v, seen)
		}
	}
}
