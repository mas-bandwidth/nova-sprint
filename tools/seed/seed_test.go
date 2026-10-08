package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

const testRecipe = "module\tex.com/tools\tex.com/sprint\n" +
	"move\tcmd/app\n" +
	"rename\tfleet\tinternal/fleetrules\n" +
	"literal\t\"../../fleet/rules.txt\"\t\"../../internal/fleetrules/rules.txt\"\n" +
	"doc\tdocs/SPEC.md\n" +
	"section\tdocs/CLI.md\tapp\n" +
	"ratings\tapp\n" +
	"model\tM\n" +
	"keep\tREADME.md\n" +
	"keep\tinternal/sprint/toolsversion.go\n" +
	"keep\ttools/seed\n" +
	"gomod\n"

var testRelease = release{tag: "v1.1.0", commit: "0123456789abcdef0123456789abcdef01234567"}

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func read(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
	return err == nil
}

func fixture(t *testing.T) (from, keep string) {
	from, keep = t.TempDir(), t.TempDir()
	write(t, from, map[string]string{
		"go.mod":                         "module ex.com/tools\n\ngo 1.26\n\ntool (\n\tex.com/lint\n)\n\nrequire ex.com/dep v1.0.0\n",
		"go.sum":                         "ex.com/dep v1.0.0 h1:x\n",
		"cmd/app/main.go":                "package main\n\nimport (\n\t\"fmt\"\n\n\t\"ex.com/tools/internal/b\"\n\t\"ex.com/tools/fleet\"\n)\n\nfunc main() { fmt.Println(b.B, fleet.F) }\n",
		"cmd/app/main_test.go":           "package main\n\nimport (\n\t\"testing\"\n\n\t\"ex.com/tools/internal/t\"\n)\n\nconst rules = \"../../fleet/rules.txt\"\n\nfunc TestX(*testing.T) { _ = t.T }\n",
		"cmd/app/testdata/golden.txt":    "ex.com/tools/internal/b stays as written\n",
		"internal/b/b.go":                "package b\n\nimport _ \"embed\"\n\nimport \"ex.com/tools/internal/c\"\n\n//go:embed data/*.txt suite.go\nvar raw string\n\nvar B = c.C\n",
		"internal/b/suite.go":            "package b\n\nimport \"ex.com/tools/internal/c\"\n\nconst Suite = c.C\n",
		"internal/b/data/x.txt":          "x\n",
		"internal/b/NOTE.md":             "a note\n",
		"internal/b/b_test.go":           "package b\n\nimport \"ex.com/tools/internal/never\"\n",
		"internal/c/c.go":                "package c\n\nconst C = 1\n",
		"internal/t/t.go":                "package t\n\nconst T = 1\n",
		"internal/never/n.go":            "package never\n",
		"internal/unused/u.go":           "package unused\n",
		"fleet/rules.go":                 "package fleet\n\nimport _ \"embed\"\n\n//go:embed rules.txt\nvar F string\n",
		"fleet/rules.txt":                "rules\n",
		"fleet/loops.yml":                "not taken\n",
		"docs/SPEC.md":                   "spec\n",
		"docs/OTHER.md":                  "not taken\n",
		"docs/CLI.md":                    "# All tools\n\n## other\n\nno\n\n## app\n\napp verbs\n\n## last\n\nno\n",
		"docs/ratings/1.0/app-read.md":   "r\n",
		"docs/ratings/1.0/x/app-use.md":  "u\n",
		"docs/ratings/1.0/other-read.md": "no\n",
		"tla/M.tla":                      "---- MODULE M ----\nEXTENDS Helper, Naturals\n====\n",
		"tla/Helper.tla":                 "---- MODULE Helper ----\nEXTENDS Leaf\n====\n",
		"tla/Leaf.tla":                   "---- MODULE Leaf ----\nLeaf == TRUE\n====\n",
		"tla/MCM.tla":                    "---- MODULE MCM ----\nEXTENDS M\n====\n",
		"tla/MCM.cfg":                    "cfg\n",
		"tla/MCMBroken.cfg":              "broken\n",
		"tla/MCN.tla":                    "n\n",
		"tla/MCN.cfg":                    "n\n",
		"tla/README-M.md":                "readme\n",
		"tla/m-bench/run.txt":            "bench\n",
		"tla/CASES.tsv":                  "config\tmodule\nMCN.cfg\tMCN.tla\nMCM.cfg\tMCM.tla\nMCMBroken.cfg\tMCM.tla\n",
		"tla/RUNS.tsv":                   "config\tmodule\trest\nMCM.cfg\tMCM.tla\t1\nMCN.cfg\tMCN.tla\t2\n",
	})
	write(t, keep, map[string]string{
		"README.md":                       "ours\n",
		"go.mod":                          "module ex.com/sprint\n\ngo 1.26\n\ntool ex.com/dead\n",
		"docs/CLI.md":                     "# The app\n\nMoved.\n\n## app\n\nold\n",
		"tools/seed/x.go":                 "package main\n",
		"internal/sprint/toolsversion.go": "package sprint\n\nconst NovaToolsVersion = \"v1.1.0\"\nconst NovaToolsVersionFile = \"NOVA-TOOLS-VERSION\"\n",
	})
	return from, keep
}

func TestTheSeedMovesCopiesAndRewritesByTheRecipe(t *testing.T) {
	from, keep := fixture(t)
	r, err := parseRecipe(testRecipe)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	rep, err := run(r, testRelease, from, keep, out)
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(rep.copied, " "); got != "internal/b internal/c internal/fleetrules internal/t" {
		t.Errorf("copied %q", got)
	}
	if got := strings.Join(rep.movedPkgs, " "); got != "cmd/app" {
		t.Errorf("moved %q", got)
	}
	if got := strings.Join(rep.tlaModules, " "); got != "Helper.tla Leaf.tla M.tla MCM.tla" {
		t.Errorf("TLA closure %q", got)
	}

	main := read(t, out, "cmd/app/main.go")
	if !strings.Contains(main, "\t\"ex.com/sprint/internal/b\"\n\t\"ex.com/sprint/internal/fleetrules\"\n") {
		t.Errorf("imports not rewritten and sorted:\n%s", main)
	}
	test := read(t, out, "cmd/app/main_test.go")
	if !strings.Contains(test, "\"ex.com/sprint/internal/t\"") || !strings.Contains(test, "\"../../internal/fleetrules/rules.txt\"") {
		t.Errorf("test file:\n%s", test)
	}
	if got := read(t, out, "cmd/app/testdata/golden.txt"); got != "ex.com/tools/internal/b stays as written\n" {
		t.Errorf("non-Go file rewritten: %q", got)
	}
	if got := read(t, out, "internal/b/b.go"); !strings.Contains(got, "\"ex.com/sprint/internal/c\"") {
		t.Errorf("copied file not rewritten:\n%s", got)
	}
	if got := read(t, out, "internal/b/suite.go"); !strings.Contains(got, "\"ex.com/sprint/internal/c\"") {
		t.Errorf("embedded Go source import not rewritten:\n%s", got)
	}
	for _, f := range []string{"internal/b/data/x.txt", "internal/b/NOTE.md", "internal/fleetrules/rules.go", "internal/fleetrules/rules.txt",
		"docs/SPEC.md", "docs/ratings/1.0/app-read.md", "docs/ratings/1.0/x/app-use.md",
		"tla/M.tla", "tla/MCM.tla", "tla/Helper.tla", "tla/Leaf.tla", "tla/MCM.cfg", "tla/MCMBroken.cfg", "tla/README-M.md", "tla/m-bench/run.txt",
		"README.md", "tools/seed/x.go", "go.sum"} {
		if !exists(out, f) {
			t.Errorf("%s missing", f)
		}
	}
	for _, f := range []string{"internal/b/b_test.go", "internal/never/n.go", "internal/unused/u.go", "fleet/rules.go",
		"internal/fleetrules/loops.yml", "docs/OTHER.md", "docs/ratings/1.0/other-read.md", "tla/MCN.tla", "tla/MCN.cfg"} {
		if exists(out, f) {
			t.Errorf("%s taken", f)
		}
	}
	if got := read(t, out, "docs/CLI.md"); got != "# The app\n\nMoved.\n\n## app\n\napp verbs\n\n\n" {
		t.Errorf("CLI.md %q", got)
	}
	if got := read(t, out, "tla/CASES.tsv"); got != "config\tmodule\nMCM.cfg\tMCM.tla\nMCMBroken.cfg\tMCM.tla\n" {
		t.Errorf("CASES %q", got)
	}
	if got := read(t, out, "tla/RUNS.tsv"); got != "config\tmodule\trest\nMCM.cfg\tMCM.tla\t1\n" {
		t.Errorf("RUNS %q", got)
	}
	if got := read(t, out, "go.mod"); got != "module ex.com/sprint\n\ngo 1.26\n\ntool ex.com/dead\n\nrequire ex.com/dep v1.0.0\n" {
		t.Errorf("go.mod %q", got)
	}
	if got, want := read(t, out, sprint.NovaToolsVersionFile), sprint.FormatNovaToolsVersion("v1.1.0", testRelease.commit); got != want {
		t.Errorf("%s %q, want %q", sprint.NovaToolsVersionFile, got, want)
	}
	if got := read(t, out, "internal/sprint/toolsversion.go"); !strings.Contains(got, `NovaToolsVersion = "v1.1.0"`) {
		t.Errorf("runtime version constant not synchronized: %s", got)
	}
}

func TestSeedSynchronizesCanaryPinAndRuntimeConstant(t *testing.T) {
	from, keep := fixture(t)
	r, err := parseRecipe(testRecipe)
	if err != nil {
		t.Fatal(err)
	}
	rel := release{tag: "v1.2.0-local.20261008.1", commit: testRelease.commit}
	out := filepath.Join(t.TempDir(), "out")
	if _, err := run(r, rel, from, keep, out); err != nil {
		t.Fatal(err)
	}
	want := sprint.FormatNovaToolsVersion(rel.tag, rel.commit)
	if got := read(t, out, sprint.NovaToolsVersionFile); got != want {
		t.Fatalf("pin %q, want %q", got, want)
	}
	if got := read(t, out, "internal/sprint/toolsversion.go"); !strings.Contains(got, `NovaToolsVersion = "`+rel.tag+`"`) {
		t.Fatalf("runtime constant not synchronized: %s", got)
	}
}

func TestTheSeedRefusesWithoutAReleaseToRecord(t *testing.T) {
	from, keep := fixture(t)
	r, err := parseRecipe(testRecipe)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	if _, err := run(r, release{}, from, keep, out); err == nil || !strings.Contains(err.Error(), sprint.NovaToolsVersionFile) {
		t.Fatalf("got %v", err)
	}
	if exists(out, ".") {
		t.Errorf("%s written", out)
	}
}

func TestTheSeedRecordsTheCheckoutsReleaseTag(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=seed", "-c", "user.email=seed@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "one")
	if _, err := checkoutRelease(dir); err == nil || !strings.Contains(err.Error(), "no release tag") {
		t.Fatalf("an untagged checkout: %v", err)
	}
	git("tag", "-a", "v1.2.0-local.20261008.1", "-m", "private canary")
	git("tag", "v1.3.0+build.1")
	git("tag", "nightly")
	canary, err := checkoutRelease(dir)
	if err != nil || canary.tag != "v1.2.0-local.20261008.1" || canary.commit == "" {
		t.Fatalf("clean annotated canary tag: %+v %v", canary, err)
	}
	git("tag", "v1.0.0")
	git("tag", "v1.1.0")
	rel, err := checkoutRelease(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rel.tag != "v1.2.0-local.20261008.1" || len(rel.commit) != 40 {
		t.Fatalf("got %+v, want highest canonical canary tag at the full head", rel)
	}
	stray := filepath.Join(dir, "stray.txt")
	if err := os.WriteFile(stray, []byte("not in the release\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := checkoutRelease(dir); err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("a dirty checkout at the tag: %v", err)
	}
	if err := os.Remove(stray); err != nil {
		t.Fatal(err)
	}
	if _, err := checkoutRelease(dir); err != nil {
		t.Fatalf("clean again: %v", err)
	}
	git("commit", "-q", "--allow-empty", "-m", "two")
	if _, err := checkoutRelease(dir); err == nil {
		t.Fatal("a commit past the tag recorded the tag")
	}
}

func TestTheSeedRefusesAnOutThatExists(t *testing.T) {
	from, keep := fixture(t)
	r, err := parseRecipe(testRecipe)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if _, err := run(r, testRelease, from, keep, out); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("got %v", err)
	}
}

func TestTheSeedRefusesAMissingPiece(t *testing.T) {
	for name, line := range map[string]string{
		"a doc":     "doc\tdocs/NONE.md\n",
		"a section": "section\tdocs/CLI.md\tnone\n",
		"a model":   "model\tNone\n",
	} {
		t.Run(name, func(t *testing.T) {
			from, keep := fixture(t)
			r, err := parseRecipe(testRecipe + line)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := run(r, testRelease, from, keep, filepath.Join(t.TempDir(), "out")); err == nil {
				t.Fatal("no error")
			}
		})
	}
}

func TestTheRecipeRefusesABadLine(t *testing.T) {
	for _, bad := range []string{"move\n", "frobnicate\tx\n", "rename\tfleet\n", "move\tx\n"} {
		if _, err := parseRecipe(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestTheShippedRecipeParses(t *testing.T) {
	r, err := readRecipe("RECIPE")
	if err != nil {
		t.Fatal(err)
	}
	if r.toMod != "github.com/mas-bandwidth/nova-sprint" || len(r.move) == 0 || !r.gomod {
		t.Fatalf("recipe %+v", r)
	}
}
