package worker

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

const (
	sibSHA   = "be2591509c68a1b2c3d4e5f60718293a4b5c6d7e"
	sibURL   = "https://github.com/mas-bandwidth/serialize.rs.git"
	sibLine  = "../serialize.rs " + sibURL + " v5.0.0 " + sibSHA
	sibBlob  = "100644 blob 5ib5ib5ib5ib5ib5ib5ib5ib5ib5ib5ib5ib5ib5\tSIBLINGS\n"
	jobSHA40 = "0123456789abcdef0123456789abcdef01234567"
)

// Every line of a SIBLINGS file is checked before anything is fetched: a
// path that is not ../<name>, a name the job directory or the job's own
// repo already uses, a clone URL on another host than the stream's (the
// worker's credential goes to that host alone), a tag git would not take as
// a ref, a sha that is not 40 lowercase hex, a line of other than four
// fields, or a name given twice; each refusal names the line and its text.
func TestParseSiblingsRefusesEachMalformedLine(t *testing.T) {
	t.Parallel()
	const repoURL, repoName = "https://github.com/o/r.git", "r"
	ok := "# siblings this repo builds against\n\n" + sibLine + "\n  ../serialize.go https://GitHub.com/mas-bandwidth/serialize.go release/v1.2.3 " + jobSHA40 + "  \n"
	got, err := parseSiblings_internal(ok, repoURL, repoName)
	if err != nil {
		t.Fatal(err)
	}
	want := []sibling_internal{
		{Line: 3, Name: "serialize.rs", URL: sibURL, Tag: "v5.0.0", SHA: sibSHA},
		{Line: 4, Name: "serialize.go", URL: "https://GitHub.com/mas-bandwidth/serialize.go", Tag: "release/v1.2.3", SHA: jobSHA40},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("parsed %+v, want %+v", got, want)
	}
	if got, err := parseSiblings_internal("", repoURL, repoName); err != nil || len(got) != 0 {
		t.Fatalf("an empty file: %v %v", got, err)
	}
	u, tag, sha := " "+sibURL+" ", " v1 ", " "+sibSHA
	for _, kase := range []struct{ line, why string }{
		{"serialize.rs" + u + tag + sha, "../<name>"},
		{"../a/b" + u + tag + sha, "../<name>"},
		{"../.." + u + tag + sha, "../<name>"},
		{"../." + u + tag + sha, "../<name>"},
		{"../a..b" + u + tag + sha, "../<name>"},
		{"../" + u + tag + sha, "../<name>"},
		{"/abs/x" + u + tag + sha, "../<name>"},
		{"../a$b" + u + tag + sha, "../<name>"},
		{"../r" + u + tag + sha, "the job's own repository"},
		{"../R" + u + tag + sha, "the job's own repository"},
		{"../repo" + u + tag + sha, "the job directory's own"},
		{"../job.json" + u + tag + sha, "the job directory's own"},
		{"../JOB.md" + u + tag + sha, "the job directory's own"},
		{"../x https://gitlab.com/o/x.git" + tag + sha, "gitlab.com"},
		{"../x http://github.com/o/x.git" + tag + sha, "http://github.com"},
		{"../x git@github.com:o/x.git" + tag + sha, "github.com"},
		{"../x -uhttps://github.com/o/x.git" + tag + sha, "clone URL"},
		{"../x ../x.git" + tag + sha, "clone URL"},
		{"../x" + u + "-v1" + sha, "tag"},
		{"../x" + u + "v1..2" + sha, "tag"},
		{"../x" + u + "v1.lock" + sha, "tag"},
		{"../x" + u + "v1/" + sha, "tag"},
		{"../x" + u + ".v1" + sha, "tag"},
		{"../x" + u + "v1~" + sha, "tag"},
		{"../x" + u + "a//b" + sha, "tag"},
		{"../x" + u + "v1@{0}" + sha, "tag"},
		{"../x" + u + "@" + sha, "tag"},
		{"../x" + u + tag + " " + strings.ToUpper(sibSHA), "40 lowercase hex"},
		{"../x" + u + tag + " " + sibSHA[:39], "40 lowercase hex"},
		{"../x" + u + tag + " " + strings.Repeat("g", 40), "40 lowercase hex"},
		{"../x" + u + tag, "four fields"},
		{"../x" + u + tag + sha + " # pinned", "four fields"},
	} {
		_, err := parseSiblings_internal("# head\n"+kase.line+"\n", repoURL, repoName)
		if err == nil || !strings.Contains(err.Error(), "SIBLINGS line 2 ") || !strings.Contains(err.Error(), strings.TrimSpace(kase.line)) || !strings.Contains(err.Error(), kase.why) {
			t.Errorf("%q: %v, want a refusal naming line 2, its text and %q", kase.line, err, kase.why)
		}
	}
	_, err = parseSiblings_internal(sibLine+"\n../Serialize.rs"+u+tag+sha+"\n", repoURL, repoName)
	if err == nil || !strings.Contains(err.Error(), "SIBLINGS line 2 ") || !strings.Contains(err.Error(), "twice") {
		t.Errorf("a name given twice: %v", err)
	}
}

// A sibling's clone URL is on the stream's own origin: the same scheme and
// host (any case; the user part and the path aside). A stream on the local
// file system (a path, or a file:// URL) takes siblings on it alone, by
// either form.
func TestSiblingOriginIsTheStreamsOwn(t *testing.T) {
	t.Parallel()
	for _, kase := range []struct {
		stream, sibling string
		same            bool
	}{
		{"https://github.com/o/r.git", "https://github.com/x/y.git", true},
		{"https://github.com/o/r.git", "https://user@GITHUB.com/x/y", true},
		{"https://github.com/o/r.git", "https://github.com:8443/x/y.git", false},
		{"https://github.com/o/r.git", "ssh://github.com/x/y.git", false},
		{"https://github.com/o/r.git", "file:///x/y.git", false},
		{"file:///s/r.git", "file:///x/y.git", true},
		{"file:///s/r.git", "/x/y.git", true},
		{"/s/r.git", "file:///x/y.git", true},
		{"/s/r.git", "https://github.com/x/y.git", false},
		{"git@github.com:o/r.git", "git@github.com:x/y.git", true},
		{"git@github.com:o/r.git", "https://github.com/x/y.git", false},
	} {
		stream, err := cloneOrigin_internal(kase.stream)
		if err != nil {
			t.Fatalf("%s: %v", kase.stream, err)
		}
		sib, err := cloneOrigin_internal(kase.sibling)
		if (err == nil && sib == stream) != kase.same {
			t.Errorf("%s beside %s: origin %q (%v) against %q, want same=%t", kase.sibling, kase.stream, sib, err, stream, kase.same)
		}
	}
}

// sibFake answers staging's git for a job repo whose base has file as its
// SIBLINGS ("" for none), whose sibling tag names tagSHA and whose checkouts
// are at their own sha: init makes its directory.
func sibFake_internal(file, tagSHA string) *edge.FakeExec {
	fx := edge.NewFakeExec()
	fx.On("git", func(c edge.FakeCall) edge.FakeOutcome {
		args := c.Cmd.Argv[gitArg0_internal:]
		switch {
		case args[0] == "init":
			_ = os.MkdirAll(args[len(args)-1], 0o755)
		case args[0] == "remote" && args[1] == "get-url":
			return edge.FakeOutcome{Stdout: testCloneURL + "\n"}
		case args[0] == "ls-tree" && file != "":
			return edge.FakeOutcome{Stdout: sibBlob}
		case args[0] == "cat-file":
			return edge.FakeOutcome{Stdout: file}
		case args[0] == "rev-parse" && strings.HasSuffix(args[len(args)-1], "^{commit}"):
			return edge.FakeOutcome{Stdout: tagSHA + "\n"}
		case args[0] == "rev-parse" && filepath.Base(c.Cmd.Dir) == "repo":
			return edge.FakeOutcome{Stdout: jobSHA40 + "\n"}
		case args[0] == "rev-parse":
			return edge.FakeOutcome{Stdout: sibSHA + "\n"}
		}
		return edge.FakeOutcome{}
	})
	return fx
}

// sibJob_internal is a work job of o/r on github whose job directory is
// <roots>/jobs/c1, and the worker's git staging from <roots>.
func sibJob_internal(t *testing.T, fx *edge.FakeExec) (*OSGitOps, Job, string) {
	t.Helper()
	roots := t.TempDir()
	t.Cleanup(func() { writable_internal(t, roots) })
	jobDir := filepath.Join(roots, "jobs", "c1")
	g := &OSGitOps{WorkRoot: filepath.Join(roots, "clones"), MirrorRoot: filepath.Join(roots, "nova-bench", "mirror"), SiblingsRoot: filepath.Join(roots, "nova-bench", "siblings"), Exec: fx}
	j := Job{Repo: "o/r", Forge: "github", Kind: "work", Base: "main", Branch: "sprint/s/c1", BaseSHA: jobSHA40, Consumer: "c1",
		JobDir: jobDir, WorktreeDir: filepath.Join(jobDir, "repo"), CommitName: testCommitName, CommitEmail: testCommitEmail}
	return g, j, roots
}

// writable_internal gives back the write bits the staged trees lost, so the
// test's directory can be removed.
func writable_internal(t *testing.T, root string) {
	t.Helper()
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
}

// A repo whose base has a SIBLINGS file: staging the job reads the file at
// the base in the worker's own work clone (never the card's head), fetches
// the sibling's tag into its bare mirror from the clone URL with the
// credential, makes its tree from the mirror without it (no alternates) at
// <bench>/siblings/<name>/<sha>, checks it is at the pinned sha, makes it
// read-only, links it beside the worktree as ../<name>, and keeps its path
// outside the job directory, where the wall reads it as a --read grant.
func TestStageWorktreeStagesTheSiblingsFileAtTheBase(t *testing.T) {
	t.Parallel()
	fx := sibFake_internal("# pinned\n"+sibLine+"\n", sibSHA)
	g, j, roots := sibJob_internal(t, fx)
	if _, err := g.StageWorktree(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	mirror := filepath.Join(roots, "nova-bench", "mirror", "serialize.rs.git")
	tree, err := filepath.EvalSymlinks(filepath.Join(roots, "nova-bench", "siblings", "serialize.rs", sibSHA))
	if err != nil {
		t.Fatal(err)
	}
	argv, dirs, remote := stageCalls_internal(fx)
	var sib []string
	for i, a := range argv {
		if strings.HasPrefix(a, "ls-tree") || strings.HasPrefix(a, "cat-file") {
			if dirs[i] != filepath.Join(roots, "clones", "r") {
				t.Fatalf("%q ran in %s, not the worker's work clone", a, dirs[i])
			}
		}
		if strings.Contains(a+" "+dirs[i], "serialize.rs") || strings.HasPrefix(a, "ls-tree") || strings.HasPrefix(a, "cat-file") {
			sib = append(sib, a)
			if network := strings.Contains(a, sibURL); remote[i] != network {
				t.Fatalf("%q: credential %t, reaches the clone URL %t", a, remote[i], network)
			}
		}
	}
	tmp := filepath.Join(roots, "nova-bench", "siblings", "serialize.rs", sibSHA+".tmp")
	want := []string{
		"ls-tree " + jobSHA40 + " -- SIBLINGS",
		"cat-file blob 5ib5ib5ib5ib5ib5ib5ib5ib5ib5ib5ib5ib5ib5",
		"init --quiet --bare " + mirror,
		"for-each-ref --count=1 --format=%(refname)",
		fetchFlags + " --depth=1 " + sibURL + " +refs/tags/v5.0.0:refs/tags/v5.0.0",
		"rev-parse --verify --quiet refs/tags/v5.0.0^{commit}",
		"init --quiet " + tmp,
		fetchFlags + " --depth=1 " + mirror + " " + sibSHA,
		"checkout --quiet --detach " + sibSHA,
		"rev-parse HEAD",
	}
	if strings.Join(sib, "\n") != strings.Join(want, "\n") {
		t.Fatalf("sibling staging ran\n%s\nwant first\n%s", strings.Join(sib, "\n"), strings.Join(want, "\n"))
	}
	if got, err := os.Readlink(filepath.Join(j.JobDir, "serialize.rs")); err != nil || got != tree {
		t.Fatalf("../serialize.rs links to %q (%v), want %s", got, err, tree)
	}
	if st, err := os.Stat(tree); err != nil || st.Mode().Perm()&0o222 != 0 {
		t.Fatalf("the tree %s is %v (%v), want read-only", tree, st.Mode(), err)
	}
	w := &OSReserveWall{Config: WallConfig{ReadPaths: []string{"/toolchain"}}}
	if reads := w.effectiveConfig_internal(j).ReadPaths; !slices.Contains(reads, tree) || !slices.Contains(reads, "/toolchain") {
		t.Fatalf("the wall reads %q, want the tree %s beside the operator's", reads, tree)
	}
	if reads := w.effectiveConfig_internal(Job{Repo: "o/r", Forge: "github", JobDir: filepath.Join(roots, "jobs", "c2")}).ReadPaths; slices.Contains(reads, tree) {
		t.Fatalf("another job's wall reads the tree: %q", reads)
	}
	if kept, err := os.ReadFile(filepath.Join(roots, RecordsDir, "c1", SiblingGrantsName)); err != nil || string(kept) != tree+"\n" {
		t.Fatalf("the kept grants are %q (%v)", kept, err)
	}

	// A second job at the same pin reuses the tree: no fetch, only a check
	// that it is at the sha.
	fx2 := sibFake_internal(sibLine+"\n", sibSHA)
	g.Exec = fx2
	j2 := j
	j2.Consumer, j2.JobDir, j2.WorktreeDir = "c2", filepath.Join(roots, "jobs", "c2"), filepath.Join(roots, "jobs", "c2", "repo")
	if _, err := g.StageWorktree(context.Background(), j2); err != nil {
		t.Fatal(err)
	}
	argv2, dirs2, _ := stageCalls_internal(fx2)
	for i, a := range argv2 {
		if (strings.HasPrefix(a, "fetch") || strings.HasPrefix(a, "init")) && strings.Contains(a+dirs2[i], "serialize.rs") {
			t.Fatalf("a staged tree was fetched again: %q in %s", a, dirs2[i])
		}
	}
	treeDir := filepath.Join(roots, "nova-bench", "siblings", "serialize.rs", sibSHA)
	if i := slices.Index(dirs2, treeDir); i < 0 || argv2[i] != "rev-parse HEAD" {
		t.Fatalf("the reused tree was not checked at its sha: %q in %q", argv2, dirs2)
	}
	if got, err := os.Readlink(filepath.Join(j2.JobDir, "serialize.rs")); err != nil || got != tree {
		t.Fatalf("the second job links to %q (%v)", got, err)
	}
}

// No SIBLINGS file at the base: nothing is fetched, linked or granted.
func TestStageWorktreeWithNoSiblingsFileStagesNone(t *testing.T) {
	t.Parallel()
	fx := sibFake_internal("", sibSHA)
	g, j, roots := sibJob_internal(t, fx)
	if _, err := g.StageWorktree(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	argv, _, _ := stageCalls_internal(fx)
	if slices.ContainsFunc(argv, func(a string) bool { return strings.HasPrefix(a, "fetch") || strings.HasPrefix(a, "cat-file") }) {
		t.Fatalf("staging with no SIBLINGS file ran %q", argv)
	}
	if fileExists_internal(filepath.Join(roots, RecordsDir, "c1", SiblingGrantsName)) || fileExists_internal(filepath.Join(roots, "nova-bench", "siblings")) {
		t.Fatal("a job with no siblings kept grants or made a tree")
	}
}

// Each failure is a staging fault naming the sibling, and none leaves a
// partial stage: a malformed file fetches nothing; a tag at another sha
// than the pin, or a tree whose checkout is not the pin, links nothing.
func TestStageWorktreeSiblingFaultsNameTheSibling(t *testing.T) {
	t.Parallel()
	for _, kase := range []struct {
		name, file, tagSHA, why string
	}{
		{"malformed", sibLine + "\n../x https://gitlab.com/o/x.git v1 " + sibSHA + "\n", sibSHA, "SIBLINGS line 2 "},
		{"tag moved", sibLine + "\n", jobSHA40, "sibling ../serialize.rs " + sibURL + "@v5.0.0: "},
	} {
		t.Run(kase.name, func(t *testing.T) {
			t.Parallel()
			fx := sibFake_internal(kase.file, kase.tagSHA)
			g, j, roots := sibJob_internal(t, fx)
			_, err := g.StageWorktree(context.Background(), j)
			if err == nil || !strings.Contains(err.Error(), kase.why) {
				t.Fatalf("staging: %v, want a fault naming %q", err, kase.why)
			}
			argv, _, _ := stageCalls_internal(fx)
			if kase.name == "malformed" && slices.ContainsFunc(argv, func(a string) bool { return strings.HasPrefix(a, "fetch") }) {
				t.Fatalf("a malformed SIBLINGS file fetched: %q", argv)
			}
			if fileExists_internal(filepath.Join(j.JobDir, "serialize.rs")) || fileExists_internal(filepath.Join(roots, RecordsDir, "c1", SiblingGrantsName)) {
				t.Fatal("a failed sibling stage linked or granted a tree")
			}
			if fileExists_internal(filepath.Join(roots, "nova-bench", "siblings", "serialize.rs", sibSHA)) {
				t.Fatal("a failed sibling stage left a tree at the pin")
			}
		})
	}
}

// The job directory's own entry at a sibling's name (one the harness made)
// is never replaced: a fault, not a link over it.
func TestPlaceSiblingRefusesAnEntryItDidNotMake(t *testing.T) {
	t.Parallel()
	jobDir, tree := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(jobDir, "serialize.rs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := placeSibling_internal(jobDir, "serialize.rs", tree); err == nil || !strings.Contains(err.Error(), "not a link the worker made") {
		t.Fatalf("placing over a directory: %v", err)
	}
	other := t.TempDir()
	if err := os.Symlink(other, filepath.Join(jobDir, "s2")); err != nil {
		t.Fatal(err)
	}
	if err := placeSibling_internal(jobDir, "s2", tree); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(filepath.Join(jobDir, "s2")); err != nil || got != tree {
		t.Fatalf("a stale link was left at %q (%v)", got, err)
	}
}
