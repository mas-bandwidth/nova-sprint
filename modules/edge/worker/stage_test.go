package worker

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// stageFake is a fake exec answering staging's git as a repo would: init
// makes its directory, remote get-url names the repo, ls-remote lists
// tips, for-each-ref lists a ref in a deep clone, merge-base fails
// mergeFails times first.
type stageFake struct {
	fx         *edge.FakeExec
	tips       string
	deep       map[string]bool // dir: has refs
	mergeFails int
}

func newStageFake_internal(tips string, deep map[string]bool, mergeFails int) *stageFake {
	f := &stageFake{fx: edge.NewFakeExec(), tips: tips, deep: deep, mergeFails: mergeFails}
	f.fx.On("git", func(c edge.FakeCall) edge.FakeOutcome {
		args := c.Cmd.Argv[gitArg0_internal:]
		switch args[0] {
		case "init":
			_ = os.MkdirAll(args[len(args)-1], 0o755)
		case "remote":
			if args[1] == "get-url" {
				return edge.FakeOutcome{Stdout: testCloneURL + "\n"}
			}
		case "ls-remote":
			return edge.FakeOutcome{Stdout: f.tips}
		case "for-each-ref":
			if f.deep[c.Cmd.Dir] {
				return edge.FakeOutcome{Stdout: "refs/heads/main\n"}
			}
		case "merge-base":
			if f.mergeFails > 0 {
				f.mergeFails--
				return edge.FakeOutcome{ExitCode: 1}
			}
		}
		return edge.FakeOutcome{}
	})
	return f
}

// stageCalls are the calls' argv after the worker's git flags, with the
// directory each ran in and whether it carried the credential.
func stageCalls_internal(fx *edge.FakeExec) (argv []string, dirs []string, remote []bool) {
	for _, c := range fx.Calls() {
		argv = append(argv, strings.Join(c.Cmd.Argv[gitArg0_internal:], " "))
		dirs = append(dirs, c.Cmd.Dir)
		remote = append(remote, slices.ContainsFunc(c.Cmd.Env, func(kv string) bool { return strings.HasPrefix(kv, "GIT_CONFIG_COUNT=") }))
	}
	return argv, dirs, remote
}

const (
	shaBase, shaPrior, shaHead, shaTip = "b1b1", "p1p1", "h1h1", "t1t1"
	fetchFlags                         = "fetch --quiet --no-tags --no-recurse-submodules --no-auto-gc"
)

func stageJobs_internal() []Job {
	return []Job{
		{Repo: "o/r", Forge: "github", Kind: "work", Base: "main", Branch: "sprint/s/p1", BaseSHA: shaBase, PriorHead: shaPrior},
		{Repo: "o/r", Forge: "github", Kind: "read", Base: "main", Branch: "sprint/s/p2", BaseSHA: shaBase, Head: shaHead},
	}
}

// A fresh bench: the mirror and the work clone are made empty, then one
// ls-remote of the named branches and one fetch per repo of only the
// commits the pass's jobs check out (their base, prior head and head, and
// the tips of the branches that exist), at depth 1, never every branch;
// the mirror's from the clone URL with the credential, the work clone's
// from the mirror without it. A branch absent from the remote is removed
// from both, so BranchTip reads "". Every head shares a commit with its
// base, so nothing is deepened.
func TestFetchIsShallowAndTargeted(t *testing.T) {
	t.Parallel()
	roots := t.TempDir()
	f := newStageFake_internal(shaTip+"\trefs/heads/main\n"+"ffff\trefs/heads/x/refs/heads/main\n", nil, 0)
	g := &OSGitOps{WorkRoot: filepath.Join(roots, "clones"), MirrorRoot: filepath.Join(roots, "mirror"), Exec: f.fx}
	if err := g.Fetch(context.Background(), stageJobs_internal()...); err != nil {
		t.Fatal(err)
	}
	mirror, work := filepath.Join(roots, "mirror", "r.git"), filepath.Join(roots, "clones", "r")
	wants := shaBase + " " + shaPrior + " " + shaHead + " " + shaTip
	want := []string{
		"init --quiet --bare " + mirror,
		"init --quiet " + work,
		"remote add origin " + testCloneURL,
		"ls-remote --quiet " + testCloneURL + " refs/heads/main refs/heads/sprint/s/p1 refs/heads/sprint/s/p2",
		"for-each-ref --count=1 --format=%(refname)",
		fetchFlags + " --depth=1 " + testCloneURL + " " + wants,
		"update-ref refs/heads/main " + shaTip,
		"update-ref -d refs/heads/sprint/s/p1",
		"update-ref -d refs/heads/sprint/s/p2",
		fetchFlags + " --depth=1 " + mirror + " " + wants,
		"update-ref refs/remotes/origin/main " + shaTip,
		"update-ref -d refs/remotes/origin/sprint/s/p1",
		"update-ref -d refs/remotes/origin/sprint/s/p2",
		"merge-base " + shaBase + " " + shaPrior,
		"merge-base " + shaBase + " " + shaTip,
		"merge-base " + shaBase + " " + shaHead,
	}
	argv, dirs, remote := stageCalls_internal(f.fx)
	if strings.Join(argv, "\n") != strings.Join(want, "\n") {
		t.Fatalf("staging ran\n%s\nwant\n%s", strings.Join(argv, "\n"), strings.Join(want, "\n"))
	}
	for i, a := range argv {
		if strings.Contains(a, "refs/heads/*") || strings.HasPrefix(a, "clone") {
			t.Fatalf("staging fetched every branch or cloned the repo: %q", a)
		}
		network := strings.Contains(a, testCloneURL) && !strings.HasPrefix(a, "remote add")
		if remote[i] != network {
			t.Fatalf("%q: credential %t, reaches the clone URL %t", a, remote[i], network)
		}
	}
	if dirs[5] != mirror || dirs[9] != work || dirs[len(dirs)-1] != work {
		t.Fatalf("the fetches ran in %q and %q, the merge-base checks in %q", dirs[5], dirs[9], dirs[len(dirs)-1])
	}
	alt, err := os.ReadFile(filepath.Join(work, ".git", "objects", "info", "alternates"))
	if err != nil || string(alt) != filepath.Join(mirror, "objects")+"\n" {
		t.Fatalf("the work clone borrows %q (%v), want the mirror's objects", alt, err)
	}
}

// fetchArgv_internal is the fetches the fake saw, by directory.
func fetchArgv_internal(fx *edge.FakeExec) map[string][]string {
	out := map[string][]string{}
	argv, dirs, _ := stageCalls_internal(fx)
	for i, a := range argv {
		if strings.HasPrefix(a, "fetch") {
			out[dirs[i]] = append(out[dirs[i]], a)
		}
	}
	return out
}

// A bench's mirror and work clone from before shallow staging are deep:
// they stay deep (the fetches carry no depth, only the commits wanted), and
// nothing is cloned again. A deep work clone behind a shallow mirror
// fetches shallow too: a shallow source feeds only a shallow fetch.
func TestFetchKeepsADeepCloneDeep(t *testing.T) {
	t.Parallel()
	for _, kase := range []string{"both deep", "shallow mirror"} {
		t.Run(kase, func(t *testing.T) {
			t.Parallel()
			roots := t.TempDir()
			mirror, work := filepath.Join(roots, "mirror", "r.git"), filepath.Join(roots, "clones", "r")
			for _, d := range []string{mirror, filepath.Join(work, ".git")} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			deep := map[string]bool{mirror: true, work: true}
			if kase == "shallow mirror" {
				if err := os.WriteFile(filepath.Join(mirror, "shallow"), []byte(shaBase+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			f := newStageFake_internal(shaTip+"\trefs/heads/main\n", deep, 0)
			g := &OSGitOps{WorkRoot: filepath.Join(roots, "clones"), MirrorRoot: filepath.Join(roots, "mirror"), Exec: f.fx}
			if err := g.Fetch(context.Background(), stageJobs_internal()[0]); err != nil {
				t.Fatal(err)
			}
			argv, _, _ := stageCalls_internal(f.fx)
			if slices.ContainsFunc(argv, func(a string) bool { return strings.HasPrefix(a, "init") || strings.HasPrefix(a, "clone") }) {
				t.Fatalf("an existing clone was made again: %q", argv)
			}
			fetches := fetchArgv_internal(f.fx)
			wantMirror, wantWork := fetchFlags+" "+testCloneURL+" "+shaBase+" "+shaPrior+" "+shaTip, fetchFlags+" "+mirror+" "+shaBase+" "+shaPrior+" "+shaTip
			if kase == "shallow mirror" {
				wantMirror, wantWork = fetchFlags+" --depth=1 "+testCloneURL+" "+shaBase+" "+shaPrior+" "+shaTip, fetchFlags+" --depth=1 "+mirror+" "+shaBase+" "+shaPrior+" "+shaTip
			}
			if !slices.Equal(fetches[mirror], []string{wantMirror}) || !slices.Equal(fetches[work], []string{wantWork}) {
				t.Fatalf("fetches %q, want mirror %q and work %q", fetches, wantMirror, wantWork)
			}
		})
	}
}

// A card branch forked below the shallow boundary shares no fetched commit
// with its base: each shallow hop is deepened, mirror first, by 64 then
// 1024 commits, then unshallowed, until it does; the deepening stops at the
// first check that passes. Every step names the commits, the unshallow
// too: a source's HEAD may be unborn (a bare mirror). A pair that shares
// none once nothing is shallow is left to the step that needs it.
func TestFetchDeepensUntilTheMergeBaseIsFetched(t *testing.T) {
	t.Parallel()
	for _, kase := range []struct {
		fails int
		steps []string
	}{
		{1, []string{"--deepen=64"}},
		{2, []string{"--deepen=64", "--deepen=1024"}},
		{99, []string{"--deepen=64", "--deepen=1024", "--unshallow"}},
	} {
		roots := t.TempDir()
		mirror, work := filepath.Join(roots, "mirror", "r.git"), filepath.Join(roots, "clones", "r")
		for _, p := range []string{filepath.Join(mirror, "shallow"), filepath.Join(work, ".git", "shallow")} {
			if err := writeGitFile_internal(p, []byte(shaBase+"\n")); err != nil {
				t.Fatal(err)
			}
		}
		f := newStageFake_internal("", nil, kase.fails)
		g := &OSGitOps{WorkRoot: filepath.Join(roots, "clones"), MirrorRoot: filepath.Join(roots, "mirror"), Exec: f.fx}
		j := stageJobs_internal()[0]
		if err := g.Fetch(context.Background(), j); err != nil {
			t.Fatal(err)
		}
		var wantMirror, wantWork []string
		wantMirror = append(wantMirror, fetchFlags+" --depth=1 "+testCloneURL+" "+shaBase+" "+shaPrior)
		wantWork = append(wantWork, fetchFlags+" --depth=1 "+mirror+" "+shaBase+" "+shaPrior)
		for _, s := range kase.steps {
			wants := " " + shaBase + " " + shaPrior
			wantMirror = append(wantMirror, fetchFlags+" "+s+" "+testCloneURL+wants)
			wantWork = append(wantWork, fetchFlags+" "+s+" "+mirror+wants)
		}
		fetches := fetchArgv_internal(f.fx)
		if !slices.Equal(fetches[mirror], wantMirror) || !slices.Equal(fetches[work], wantWork) {
			t.Fatalf("%d failed checks: fetches\n%q\nwant mirror\n%q\nwork\n%q", kase.fails, fetches, wantMirror, wantWork)
		}
	}
}

// A read job carries no base sha: its head is paired with its stream's tip,
// so the reader's diff finds the merge base in the work clone.
func TestMergePairsOfAReadJob(t *testing.T) {
	t.Parallel()
	got := mergePairs_internal([]Job{{Kind: "read", Base: "main", Head: shaPrior}}, map[string]string{"main": shaTip})
	if want := [][2]string{{shaTip, shaPrior}}; !slices.Equal(got, want) {
		t.Fatalf("pairs %q, want %q", got, want)
	}
}

// With no mirror the work clone is the one repo staging fetches into, from
// the clone URL, with the credential.
func TestFetchWithoutAMirror(t *testing.T) {
	t.Parallel()
	roots := t.TempDir()
	f := newStageFake_internal("", nil, 0)
	g := &OSGitOps{WorkRoot: filepath.Join(roots, "clones"), Exec: f.fx}
	if err := g.Fetch(context.Background(), stageJobs_internal()[0]); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(roots, "clones", "r")
	if got := fetchArgv_internal(f.fx); len(got) != 1 || !slices.Equal(got[work], []string{fetchFlags + " --depth=1 " + testCloneURL + " " + shaBase + " " + shaPrior}) {
		t.Fatalf("fetches %q", got)
	}
	if fileExists_internal(filepath.Join(work, ".git", "objects", "info", "alternates")) {
		t.Fatal("a work clone with no mirror borrows objects")
	}
}

// The job's tree shares the work clone's objects and its shallow boundary:
// git clone --shared of a shallow repo copies, instead, only the objects
// its branches reach.
func TestSharedCloneCarriesTheShallowBoundary(t *testing.T) {
	t.Parallel()
	roots := t.TempDir()
	work, dest := filepath.Join(roots, "work"), filepath.Join(roots, "dest")
	if err := writeGitFile_internal(filepath.Join(work, ".git", "shallow"), []byte(shaBase+"\n")); err != nil {
		t.Fatal(err)
	}
	if err := writeGitFile_internal(filepath.Join(work, ".git", "objects", "info", "alternates"), []byte("/m/r.git/objects\n")); err != nil {
		t.Fatal(err)
	}
	f := newStageFake_internal("", nil, 0)
	g := &OSGitOps{Exec: f.fx}
	if err := g.sharedClone_internal(context.Background(), 0, work, dest); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, ".git", "shallow")); err != nil || string(b) != shaBase+"\n" {
		t.Fatalf("shallow %q, %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, ".git", "objects", "info", "alternates")); err != nil || string(b) != filepath.Join(work, ".git", "objects")+"\n/m/r.git/objects\n" {
		t.Fatalf("alternates %q, %v", b, err)
	}
	if argv, _, _ := stageCalls_internal(f.fx); !slices.Equal(argv, []string{"init --quiet " + dest, "remote add origin " + work}) {
		t.Fatalf("git ran %q", argv)
	}
}
