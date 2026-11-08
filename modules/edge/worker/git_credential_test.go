package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/git"
)

const gitTokenFixture = "gh-gate-token-fixture"

// workerGitCalls drives every worker git call (staging, the collect into
// the worker's clone, and the calls that reach the remote) through g on a
// fake exec, and returns those that reach the remote.
func workerGitCalls_internal(t *testing.T, g *OSGitOps, fx *edge.FakeExec) []edge.FakeCall {
	t.Helper()
	sha := strings.Repeat("a", 40)
	sealedFake_internal(fx, sha)
	ctx := context.Background()
	jobDir := filepath.Join(t.TempDir(), "job")
	j := Job{Repo: "o/r", Forge: "github", Kind: "work", Branch: "sprint/s/p1", BaseSHA: strings.Repeat("b", 40), Consumer: "p1.w1",
		JobDir: jobDir, WorktreeDir: filepath.Join(jobDir, "repo"), CommitName: testCommitName, CommitEmail: testCommitEmail}
	if _, err := g.StageWorktree(ctx, j); err != nil {
		t.Fatal(err)
	}
	if err := g.Fetch(ctx, j); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, SealBundle), []byte("bundle"), 0o600); err != nil { // the seal step's
		t.Fatal(err)
	}
	const url = "https://github.com/o/r.git"
	clone := filepath.Join(t.TempDir(), "clone")
	if _, err := g.Collect(ctx, Sealed{JobDir: jobDir, Clone: clone, Remote: url, Base: j.BaseSHA, Name: j.CommitName, Email: j.CommitEmail}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.OriginHasHead(ctx, clone, url, j.Branch, sha); err != nil {
		t.Fatal(err)
	}
	if _, err := g.RebaseOntoStream(ctx, clone, url, "sprint/s/group"); err != nil {
		t.Fatal(err)
	}
	if err := g.Push(ctx, clone, url, j.Branch, sha, j.BaseSHA); err != nil {
		t.Fatal(err)
	}
	var remote []edge.FakeCall
	verbs := map[string]bool{}
	for _, c := range fx.Calls() {
		if args := c.Cmd.Argv[gitArg0_internal:]; remoteCall_internal(args) {
			remote, verbs[args[0]] = append(remote, c), true
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

// The token is the worker's git's alone: the harness's wall (RunHarness),
// the gates' (RunTest) and the seal step's (RunStep), which start from the
// worker's own environment, never carry it, nor git's credential env, in
// their environment or argv; and each run's leftovers are killed.
func TestWorkerGitTokenNeverReachesTheHarness(t *testing.T) { // unitlint: serial, it sets the process environment (t.Setenv): RunTest starts from os.Environ
	t.Setenv(forge.TokenEnv, gitTokenFixture)
	dir := t.TempDir()
	fx := cr2OKExec_internal()
	var groups []int
	w := &OSReserveWall{Exec: fx, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", NetDeny: true},
		LookPath: func(string) (string, error) { return "/usr/bin/claude", nil }, KillGroup: func(pgid int) (int, error) {
			groups = append(groups, pgid)
			if len(groups) == 3 {
				return 2, errors.New("kill -KILL: operation not permitted")
			}
			return 2, nil
		}}
	j := Job{Consumer: "c1", JobDir: dir, WorktreeDir: dir}
	leaked := append([]string{"PATH=/usr/bin", forge.TokenEnv + "=" + gitTokenFixture}, git.CredentialEnv(gitTokenFixture, "gh")...)
	var results []WallRunResult
	res, err := w.RunHarness(context.Background(), j, []string{"claude"}, leaked, time.Minute)
	results = append(results, res)
	if err == nil {
		res, err = w.RunTest(context.Background(), j, "go test ./...", time.Minute)
		results = append(results, res)
	}
	if err == nil {
		res, err = w.RunStep(context.Background(), j, sealArgv_internal(dir, strings.Repeat("b", 40), "n", "e", "m"), time.Minute)
		results = append(results, res)
	}
	if err != nil {
		t.Fatal(err)
	}
	calls := fx.Calls()
	if len(calls) != 3 {
		t.Fatalf("want three wall runs, got %d", len(calls))
	}
	for i, c := range calls {
		all := strings.Join(append(slices.Clone(c.Cmd.Argv), c.Cmd.Env...), "\n")
		if !c.Cmd.CleanEnv || strings.Contains(all, gitTokenFixture) || strings.Contains(all, "GIT_CONFIG_") || strings.Contains(all, forge.TokenEnv) {
			t.Fatalf("the wall's run carries the git credential (clean env %t):\n%s", c.Cmd.CleanEnv, all)
		}
		// Each run's process group (its leader's pid) is killed after it, and the count kept.
		if groups[i] != c.PID || results[i].Leftovers != 2 {
			t.Fatalf("run %d: group killed %v, leftovers %d, want pid %d and 2", i, groups, results[i].Leftovers, c.PID)
		}
	}
	// A kill that failed is said on the step's line, never dropped.
	if note := leftoversNote_internal(results[2]); results[0].LeftoverErr != nil || !strings.Contains(note, "operation not permitted") || !strings.Contains(note, "leftovers-killed=2") {
		t.Fatalf("the seal step's leftovers note %q (harness's error %v)", note, results[0].LeftoverErr)
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
	for name, tampered := range map[string]JobRecord{
		"another repo": {Consumer: rec.Consumer, Kind: "work", Repo: "other/r", Forge: "github", Branch: rec.Branch, Gen: 1},
		"another host": {Consumer: rec.Consumer, Kind: "work", Repo: "https://evil.example/o/r.git", Forge: "github", Branch: rec.Branch, Gen: 1},
		"no repo":      {Consumer: rec.Consumer, Kind: "work", Forge: "github", Branch: rec.Branch, Gen: 1},
	} {
		if _, err := r.jobRemote_internal(tampered); err == nil || !strings.Contains(err.Error(), "is not the take receipt's clone URL") {
			t.Fatalf("%s: %v", name, err)
		}
		if err := r.pushBranch_internal(context.Background(), &tampered, strings.Repeat("a", 40)); err == nil || !strings.Contains(err.Error(), "is not the take receipt's clone URL") {
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

// sealedFake_internal answers a worker's git calls on a fake exec as a
// clone would: clone makes its directory, bundle create its file,
// rev-parse and ls-remote a sha.
func sealedFake_internal(fx *edge.FakeExec, sha string) {
	fx.On("git", func(c edge.FakeCall) edge.FakeOutcome {
		args := c.Cmd.Argv[gitArg0_internal:]
		switch {
		case args[0] == "clone":
			_ = os.MkdirAll(filepath.Join(args[len(args)-1], ".git"), 0o755)
		case args[0] == "bundle" && args[1] == "create":
			_ = os.WriteFile(args[2], []byte("bundle"), 0o600)
		case args[0] == "remote" && args[1] == "get-url":
			return edge.FakeOutcome{Stdout: testCloneURL + "\n"}
		case args[0] == "rev-parse", args[0] == "ls-remote":
			return edge.FakeOutcome{Stdout: sha + "\n"}
		}
		return edge.FakeOutcome{}
	})
}

// The token is on the calls that reach a remote alone (the work clone's
// clone and fetch, the rebase's fetch, ls-remote, push), never on a local
// clone, the bundle fetch, rebase, rev-parse or config; every call carries
// the no-hooks, no-fsmonitor, no-submodule flags, reads no global or
// system config, and fetch and push pass --no-recurse-submodules.
func TestWorkerGitTokenOnlyOnNetworkCalls(t *testing.T) {
	t.Parallel()
	fx := edge.NewFakeExec()
	g := &OSGitOps{WorkRoot: filepath.Join(t.TempDir(), "clones"), MirrorRoot: filepath.Join(t.TempDir(), "mirror"), Exec: fx, Token: gitTokenFixture}
	workerGitCalls_internal(t, g, fx)
	seen := map[string]bool{}
	for _, c := range fx.Calls() {
		args := c.Cmd.Argv[gitArg0_internal:]
		seen[args[0]] = true
		if !slices.Equal(c.Cmd.Argv[:gitArg0_internal], gitPrefix_internal) {
			t.Fatalf("%q lacks the worker's git flags", c.Cmd.Argv)
		}
		network := remoteCall_internal(args)
		token := slices.Contains(c.Cmd.Env, "NOVA_SPRINT_GIT_TOKEN="+gitTokenFixture)
		helper := slices.ContainsFunc(c.Cmd.Env, func(kv string) bool { return strings.HasPrefix(kv, "GIT_CONFIG_COUNT=") })
		if network != token || network != helper {
			t.Fatalf("%q: network %t, token %t, helper %t", args, network, token, helper)
		}
		if (args[0] == "fetch" || args[0] == "push") && !slices.Contains(args, "--no-recurse-submodules") {
			t.Fatalf("%q recurses into submodules", args)
		}
		if !slices.Contains(c.Cmd.Env, "GIT_CONFIG_GLOBAL=/dev/null") || !slices.Contains(c.Cmd.Env, "GIT_CONFIG_NOSYSTEM=1") {
			t.Fatalf("%q reads the host's global or system git config: %q", args, c.Cmd.Env)
		}
	}
	for _, v := range []string{"clone", "fetch", "ls-remote", "push", "rebase", "rev-parse", "config", "bundle", "merge-base", "checkout", "-c"} {
		if !seen[v] {
			t.Fatalf("git %s was not reached: %v", v, seen)
		}
	}
}

// #680 redesign: no worker git runs in the job directory after the
// harness. The seal step runs in the wall; the push, the rebase, the
// done-when apply's export and the clean's check all run in the worker's
// own clone under <root>/clones. A job with no clone URL is refused.
func TestWorkerGitNeverRunsInTheJobDirAfterTheHarness(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sha := strings.Repeat("a", 40)
	fx := edge.NewFakeExec()
	sealedFake_internal(fx, sha)
	g := &OSGitOps{WorkRoot: filepath.Join(root, "work"), Exec: fx}
	var steps [][]string
	wall := &stepWall_internal{fakeWall: &fakeWall{}, steps: &steps}
	r, err := NewRunner(Runner{Host: "b1", RootDir: root, Git: g, Wall: wall, Now: testClock_internal(), Sleep: noSleep_internal})
	if err != nil {
		t.Fatal(err)
	}
	jobDir := filepath.Join(root, "jobs", "p1.w1")
	rec := JobRecord{Epoch: 1, Consumer: "p1.w1", Kind: "work", Repo: "o/r", Forge: "github", Branch: "sprint/s/p1", BaseSHA: strings.Repeat("b", 40),
		Gen: 1, StreamRef: "sprint/s/group", DoneWhen: []string{"go test ./..."}, Outcome: Outcome{Result: "ok"}}
	writeStagedRecord_internal(t, jobDir, rec)
	if err := os.WriteFile(filepath.Join(jobDir, SealBundle), []byte("bundle"), 0o600); err != nil { // the seal step's
		t.Fatal(err)
	}
	ctx := context.Background()
	job := Job{Consumer: rec.Consumer, JobDir: jobDir, WorktreeDir: filepath.Join(jobDir, "repo"), BaseSHA: rec.BaseSHA, CommitName: testCommitName, CommitEmail: testCommitEmail}
	if out := r.harvestWorkHead_internal(ctx, job, &rec, rec.Outcome); out.Result != "ok" || out.Head != sha {
		t.Fatalf("harvest: %+v", out)
	}
	rec.Outcome.Head = sha
	if err := r.rebaseAndTestStep_internal(ctx, jobDir, &rec); err != nil || rec.Outcome.Result != "ok" {
		t.Fatalf("rebase and test: %v %+v", err, rec.Outcome)
	}
	if err := r.cleanJobDir_internal(ctx, jobDir, rec); err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0][0] != "sh" || !strings.Contains(steps[0][2], "git bundle create") || !strings.Contains(steps[1][2], "git checkout") {
		t.Fatalf("the wall ran %q, want the seal step then the apply", steps)
	}
	verbs := map[string]bool{}
	for _, c := range fx.Calls() {
		args := c.Cmd.Argv[gitArg0_internal:]
		verbs[args[0]] = true
		if c.Cmd.Dir == jobDir || strings.HasPrefix(c.Cmd.Dir, jobDir+string(filepath.Separator)) {
			t.Fatalf("worker git %q ran in the job directory (%s)", args, c.Cmd.Dir)
		}
	}
	for _, v := range []string{"clone", "bundle", "merge-base", "push", "fetch", "rebase", "ls-remote"} {
		if !verbs[v] {
			t.Fatalf("git %s was not reached: %v", v, verbs)
		}
	}
	if fileExists_internal(r.clonePath_internal(rec.Consumer)) || fileExists_internal(jobDir) {
		t.Fatal("clean left the job directory or the worker's clone")
	}
	if err := g.Push(ctx, root, "", "sprint/s/p1", sha, ""); err == nil || !strings.Contains(err.Error(), "names no clone URL") {
		t.Fatalf("a job with no clone URL: %v", err)
	}
}

// stepWall_internal records the argv of every RunStep.
type stepWall_internal struct {
	*fakeWall
	steps *[][]string
}

func (w *stepWall_internal) RunStep(_ context.Context, _ Job, argv []string, _ time.Duration) (WallRunResult, error) {
	*w.steps = append(*w.steps, argv)
	return WallRunResult{}, nil
}

// A sealed head that does not descend from the job's base, or whose
// commits add a gitlink, is refused before anything is pushed.
func TestCollectRefusesAForeignHeadOrAGitlink(t *testing.T) {
	t.Parallel()
	for name, fail := range map[string]func(args []string) (edge.FakeOutcome, bool){
		"not a descendant": func(args []string) (edge.FakeOutcome, bool) {
			return edge.FakeOutcome{ExitCode: 1}, args[0] == "merge-base"
		},
		"a gitlink": func(args []string) (edge.FakeOutcome, bool) {
			switch {
			case args[0] == "rev-list":
				return edge.FakeOutcome{Stdout: "c1\n"}, true
			case args[0] == "ls-tree" && args[3] == "c1":
				return edge.FakeOutcome{Stdout: "160000 commit " + strings.Repeat("d", 40) + "\tsub\x00"}, true
			}
			return edge.FakeOutcome{}, false
		},
	} {
		fx := edge.NewFakeExec()
		fx.On("git", func(c edge.FakeCall) edge.FakeOutcome {
			args := c.Cmd.Argv[gitArg0_internal:]
			if out, ok := fail(args); ok {
				return out
			}
			if args[0] == "clone" {
				_ = os.MkdirAll(filepath.Join(args[len(args)-1], ".git"), 0o755)
			}
			return edge.FakeOutcome{Stdout: strings.Repeat("a", 40) + "\n"}
		})
		jobDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(jobDir, SealBundle), []byte("bundle"), 0o600); err != nil {
			t.Fatal(err)
		}
		g := &OSGitOps{WorkRoot: t.TempDir(), Exec: fx}
		_, err := g.Collect(context.Background(), Sealed{JobDir: jobDir, Clone: filepath.Join(t.TempDir(), "c"), Remote: testCloneURL, Base: strings.Repeat("b", 40)})
		if err == nil || !strings.Contains(err.Error(), "collect refused") {
			t.Fatalf("%s: %v", name, err)
		}
		for _, c := range fx.Calls() {
			if c.Cmd.Argv[gitArg0_internal] == "checkout" {
				t.Fatalf("%s: the refused head was checked out", name)
			}
		}
	}
}

// remoteCall_internal: a worker git call that reaches a remote.
func remoteCall_internal(args []string) bool {
	switch args[0] {
	case "fetch", "ls-remote", "push":
		return true
	case "clone":
		return slices.Contains(args, "https://github.com/o/r.git")
	}
	return false
}

// The wall's read set for a job's tree is worked out from the job's repo
// and the worker's own clone roots, never from the tree: a .git file
// naming another gitdir or alternates naming another object store widen
// nothing.
func TestWallReadsOnlyWorkerOwnedClones(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wt := filepath.Join(dir, "repo")
	info := filepath.Join(wt, ".git", "objects", "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(info, "alternates"), []byte("/secret/store/objects\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &OSReserveWall{Config: WallConfig{ReadPaths: []string{"/usr"}, WorkRoot: "/w", MirrorRoot: "/m"}}
	cfg := w.effectiveConfig_internal(Job{Repo: "o/r", Forge: "github", JobDir: dir, WorktreeDir: wt})
	if want := []string{"/usr", "/w/r/.git", "/m/r.git"}; !slices.Equal(cfg.ReadPaths, want) {
		t.Fatalf("read set %q, want %q", cfg.ReadPaths, want)
	}
	if err := os.RemoveAll(filepath.Join(wt, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /secret/repo/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg := w.effectiveConfig_internal(Job{Repo: "o/r", Forge: "github", JobDir: dir, WorktreeDir: wt}); slices.ContainsFunc(cfg.ReadPaths, func(p string) bool { return strings.HasPrefix(p, "/secret") }) {
		t.Fatalf("a .git file widened the read set: %q", cfg.ReadPaths)
	}
}

// A job directory whose job.json or receipt is not the worker's kept copy
// is refused, and a pass says so and acts on nothing in it.
func TestJobRecordIsReadOnlyFromTheKeptCopy(t *testing.T) {
	t.Parallel()
	// Each changed job directory is set aside to quarantine: its harness
	// killed, its job directory moved, the worker's state for it removed.
	for _, change := range []string{"job.json", ReceiptName, "no kept copy", "torn write"} {
		root := t.TempDir()
		jobDir := filepath.Join(root, "jobs", "p1.w1")
		rec := JobRecord{Epoch: 1, Consumer: "p1.w1", Kind: "work", Row: "vision.ds", Branch: "sprint/s/p1", Gen: 3, PID: 4242, Nonce: "n1"}
		if err := WriteJobRecord(jobDir, rec); err != nil {
			t.Fatal(err)
		}
		if err := writeReceipt_internal(jobDir, TakeInfo{Consumer: "p1.w1", Branch: "sprint/s/p1"}); err != nil {
			t.Fatal(err)
		}
		if got, err := ReadJobRecord(jobDir); err != nil || got.Branch != rec.Branch || !fileExists_internal(filepath.Join(root, RecordsDir, "p1.w1", "job.json")) {
			t.Fatalf("the kept record: %+v %v", got, err)
		}
		if err := os.MkdirAll(filepath.Join(root, SealedDir, "p1.w1", ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		evil := []byte(`{"consumer":"p1.w1","branch":"main","row":"vision.ds","kind":"work","epoch":1,"gen":3,"pid":4242,"nonce":"n1"}` + "\n")
		var err error
		switch change {
		case "job.json", ReceiptName:
			err = os.WriteFile(filepath.Join(jobDir, change), evil, 0o600)
		case "no kept copy": // a job taken before kept copies
			err = os.RemoveAll(filepath.Join(root, RecordsDir))
		case "torn write": // a crash between the job directory's write and the kept copy's
			rec.State = JobReporting
			data, _ := json.Marshal(rec)
			err = os.WriteFile(filepath.Join(jobDir, "job.json"), append(data, '\n'), 0o600)
		}
		if err != nil {
			t.Fatal(err)
		}
		if change == ReceiptName {
			if _, err := readReceipt_internal(jobDir); !errors.Is(err, ErrJobDirChanged) {
				t.Fatalf("a changed receipt: %v", err)
			}
		} else if _, err := ReadJobRecord(jobDir); !errors.Is(err, ErrJobDirChanged) {
			t.Fatalf("%s: %v", change, err)
		}
		var warns []string
		procs := &fakeProcTable{alive: map[string]bool{"4242:n1": true}}
		r, err := NewRunner(Runner{Host: "b1", RootDir: root, Git: newFakeGit_internal(), Wall: &fakeWall{}, Procs: procs, Now: testClock_internal(), Sleep: noSleep_internal,
			Warn: func(s string) { warns = append(warns, s) }})
		if err != nil {
			t.Fatal(err)
		}
		jobs, err := r.loadRowJobs_internal("vision.ds")
		said := strings.Join(warns, "\n")
		if err != nil || len(jobs) != 0 || !strings.Contains(said, "quarantine") || !strings.Contains(said, "not the one the worker kept") {
			t.Fatalf("%s: a pass over a changed job directory: jobs %+v err %v warnings %q", change, jobs, err, warns)
		}
		if fileExists_internal(jobDir) || !fileExists_internal(filepath.Join(r.quarantineDir_internal("p1.w1", 3), "job.json")) || !r.quarantined_internal(ConsumerSummary{ID: "p1.w1", Gen: 3}) {
			t.Fatalf("%s: the job directory is not in quarantine (warnings %q)", change, warns)
		}
		if len(procs.killed) != 1 || procs.killed[0] != 4242 {
			t.Fatalf("%s: its harness is not killed: %v", change, procs.killed)
		}
		for _, d := range []string{filepath.Join(root, RecordsDir, "p1.w1"), filepath.Join(root, SealedDir, "p1.w1")} {
			if fileExists_internal(d) {
				t.Fatalf("%s: %s is left", change, d)
			}
		}
	}
}

// TestJobStateRemovesOnlyTheWorkersOwnClones: a clone under <root>/clones
// (a work clone, whatever its alternates) is never removed with a job.
func TestJobStateRemovesOnlyTheWorkersOwnClones(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	clones := filepath.Join(root, "clones")
	alternates := func(name, line string) string {
		dir := filepath.Join(clones, name)
		if err := os.MkdirAll(filepath.Join(dir, ".git", "objects", "info"), 0o755); err != nil {
			t.Fatal(err)
		}
		if line != "" {
			if err := os.WriteFile(filepath.Join(dir, ".git", "objects", "info", "alternates"), []byte(line+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	work := alternates("work", filepath.Join(root, "mirrors", "x.git", "objects"))
	bare := alternates("bare", "")
	self := alternates("self", filepath.Join(clones, "self", ".git", "objects"))
	outside := alternates("outside", filepath.Join(root, "elsewhere", "w", ".git", "objects"))
	shared := alternates("p1.w1", filepath.Join(clones, "work", ".git", "objects"))
	r := &Runner{RootDir: root, Warn: func(string) {}}
	for _, d := range []string{work, bare, self, outside, shared} {
		r.removeJobState_internal(filepath.Base(d))
		if !fileExists_internal(d) {
			t.Fatalf("%s, not the worker's own, was removed", d)
		}
	}
}

// quietRunner_internal is a runner over root whose warnings are collected.
func quietRunner_internal(t *testing.T, root string, procs ProcessTable) (*Runner, *[]string) {
	t.Helper()
	var warns []string
	r, err := NewRunner(Runner{Host: "b1", RootDir: root, Git: newFakeGit_internal(), Wall: &fakeWall{}, Procs: procs, Now: testClock_internal(), Sleep: noSleep_internal,
		Warn: func(s string) { warns = append(warns, s) }})
	if err != nil {
		t.Fatal(err)
	}
	return r, &warns
}

// TestQuarantineSaysWhenItCannotKill: with no job.json to read, the
// quarantine says it could not look for the harness, naming the job dir.
func TestQuarantineSaysWhenItCannotKill(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	jobDir := filepath.Join(root, "jobs", "p1.w1")
	if err := os.MkdirAll(filepath.Join(jobDir, "job.json"), 0o755); err != nil { // unreadable as a file
		t.Fatal(err)
	}
	r, warns := quietRunner_internal(t, root, &fakeProcTable{alive: map[string]bool{}})
	r.quarantineChanged_internal(jobDir, "p1.w1", ErrJobDirChanged)
	said := strings.Join(*warns, "\n")
	if !strings.Contains(said, "harness not stopped") || !strings.Contains(said, jobDir) {
		t.Fatalf("the skipped kill is not said: %q", said)
	}
}

// TestQuarantineSaysWhenNoProcessMatched: a recorded harness that is not
// running (a worker restart on macOS) is said on the line, never silent.
func TestQuarantineSaysWhenNoProcessMatched(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	jobDir := filepath.Join(root, "jobs", "p1.w1")
	if err := WriteJobRecord(jobDir, JobRecord{Consumer: "p1.w1", Row: "vision.ds", Gen: 3, PID: 4242, Nonce: "n1"}); err != nil {
		t.Fatal(err)
	}
	procs := &fakeProcTable{alive: map[string]bool{}}
	r, warns := quietRunner_internal(t, root, procs)
	r.quarantineChanged_internal(jobDir, "p1.w1", ErrJobDirChanged)
	r.withdrawRunningJob_internal(JobRecord{Consumer: "p1.w2", PID: 4343, Nonce: "n2"})
	said := strings.Join(*warns, "\n")
	for _, want := range []string{"no live process matched pid 4242 with nonce n1", "no live process matched pid 4343 with nonce n2"} {
		if !strings.Contains(said, want) {
			t.Fatalf("%q is not said: %q", want, said)
		}
	}
}

// fails the kept-copy check is reported and left as it is.
func TestFileUnderJobsIsReported(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := WriteJobRecord(filepath.Join(root, "jobs", "p1.w1"), JobRecord{Consumer: "p1.w1", Row: "vision.ds", Gen: 1}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "jobs", "p1.w1")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "jobs", "p1.w1"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, warns := quietRunner_internal(t, root, nil)
	jobs, err := r.loadRowJobs_internal("vision.ds")
	said := strings.Join(*warns, "\n")
	if err != nil || len(jobs) != 0 || !strings.Contains(said, "p1.w1") || !strings.Contains(said, "not the one the worker kept") || !fileExists_internal(filepath.Join(root, "jobs", "p1.w1")) {
		t.Fatalf("jobs %+v err %v said %q", jobs, err, said)
	}
}

// TestUnreadableJobRecordIsReported: a job directory whose record cannot be
// read (not a changed one) is reported, naming its path and the error.
func TestUnreadableJobRecordIsReported(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	jobDir := filepath.Join(root, "jobs", "p1.w1")
	if err := WriteJobRecord(jobDir, JobRecord{Consumer: "p1.w1", Row: "vision.ds", Gen: 1}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{jobDir, filepath.Join(root, RecordsDir, "p1.w1")} {
		if err := os.WriteFile(filepath.Join(d, "job.json"), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r, warns := quietRunner_internal(t, root, nil)
	jobs, err := r.loadRowJobs_internal("vision.ds")
	said := strings.Join(*warns, "\n")
	if err != nil || len(jobs) != 0 || !strings.Contains(said, "p1.w1") || !strings.Contains(said, "invalid character") {
		t.Fatalf("jobs %+v err %v said %q", jobs, err, said)
	}
}
