package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/testkit/fsx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRecipe = "module\tex.com/tools\tex.com/sprint\n" +
	"move\tcmd/app\n" +
	"rename\tfleet\tinternal/fleetrules\n" +
	"literal\t\"../../fleet/rules.txt\"\t\"../../internal/fleetrules/rules.txt\"\n" +
	"literal-file\tcmd/app/outside.go\tmachineVersions func(ctx context.Context, machines []string) (sprint.VersionsM, error)\tmachineVersions func(ctx context.Context, machines []string) (sprint.VersionsM, error); novaTools func(bin string) sprint.NovaToolsProbe\n" +
	"literal-file\tcmd/app/seat.go\tst, err := a.store(*c)\tif code := a.refuseNovaTools(verb, stderr); code != 0 { return code }; st, err := a.store(*c)\n" +
	"doc\tdocs/SPEC.md\n" +
	"section\tdocs/CLI.md\tapp\n" +
	"ratings\tapp\n" +
	"model\tM\n" +
	"keep\tREADME.md\n" +
	"keep\tinternal/sprint/toolsversion.go\n" +
	"keep\ttools/seed\n" +
	"gomod\n"

var testRelease = release{tag: "v1.1.0", commit: "0123456789abcdef0123456789abcdef01234567"}

func fixture(t *testing.T) (from, keep string) {
	from = fsx.Tree(t, map[string]string{
		"go.mod":                         "module ex.com/tools\n\ngo 1.26\n\ntool (\n\tex.com/lint\n)\n\nrequire ex.com/dep v1.0.0\n",
		"go.sum":                         "ex.com/dep v1.0.0 h1:x\n",
		"cmd/app/main.go":                "package main\n\nimport (\n\t\"fmt\"\n\n\t\"ex.com/tools/internal/b\"\n\t\"ex.com/tools/fleet\"\n)\n\nfunc main() { fmt.Println(b.B, fleet.F) }\n",
		"cmd/app/outside.go":             "package main\n\nimport sprint \"ex.com/tools/internal/b\"\n\ntype outside struct {\n\tmachineVersions func(ctx context.Context, machines []string) (sprint.VersionsM, error)\n}\n",
		"cmd/app/seat.go":                "package main\n\nimport \"ex.com/tools/internal/b\"\n\nvar _ = b.B\n\nfunc (a *app) runSeatCheckVerb(verb string, args []string, stdout, stderr io.Writer) int {\n\tst, err := a.store(*c)\n\treturn 0\n}\n",
		"cmd/app/unrelated.go":           "package main\n\nfunc unrelated() {\n\tst, err := a.store(*c)\n\t_ = st\n\t_ = err\n}\n",
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
	keep = fsx.Tree(t, map[string]string{
		"README.md":                       "ours\n",
		"go.mod":                          "module ex.com/sprint\n\ngo 1.26\n\ntool ex.com/dead\n",
		"docs/CLI.md":                     "# The app\n\nMoved.\n\n## app\n\nold\n",
		"tools/seed/x.go":                 "package main\n",
		"internal/sprint/toolsversion.go": "package sprint\n\nconst NovaToolsVersion = \"v1.1.0\"\nconst NovaToolsVersionFile = \"NOVA-TOOLS-VERSION\"\n",
	})
	return from, keep
}

func TestTheSeedMovesCopiesAndRewritesByTheRecipe(t *testing.T) {
	t.Parallel()
	from, keep := fixture(t)
	r, err := parseRecipe(testRecipe)
	require.NoError(t, err)
	out := filepath.Join(t.TempDir(), "out")
	rep, err := run(r, testRelease, from, keep, out)
	require.NoError(t, err)

	assert.Equal(t, "internal/b internal/c internal/fleetrules internal/t", strings.Join(rep.copied, " "), "copied")
	assert.Equal(t, "cmd/app", strings.Join(rep.movedPkgs, " "), "moved")
	assert.Equal(t, "Helper.tla Leaf.tla M.tla MCM.tla", strings.Join(rep.tlaModules, " "), "TLA closure")

	main := fsx.Read(t, filepath.Join(out, "cmd/app/main.go"))
	assert.Contains(t, main, "\t\"ex.com/sprint/internal/b\"\n\t\"ex.com/sprint/internal/fleetrules\"\n", "imports not rewritten and sorted")
	test := fsx.Read(t, filepath.Join(out, "cmd/app/main_test.go"))
	assert.Contains(t, test, "\"ex.com/sprint/internal/t\"")
	assert.Contains(t, test, "\"../../internal/fleetrules/rules.txt\"")
	outside := fsx.Read(t, filepath.Join(out, "cmd/app/outside.go"))
	assert.Contains(t, outside, "novaTools", "seat-check outside contract not retained")
	assert.Contains(t, outside, "func(bin string) sprint.NovaToolsProbe")
	seat := fsx.Read(t, filepath.Join(out, "cmd/app/seat.go"))
	assert.Contains(t, seat, "a.refuseNovaTools(verb, stderr)", "seat-check gate is not before store access")
	assert.Contains(t, seat, "st, err := a.store(*c)")
	assert.NotContains(t, fsx.Read(t, filepath.Join(out, "cmd/app/unrelated.go")), "refuseNovaTools", "path-scoped seat literal leaked into unrelated code")
	assert.Equal(t, "ex.com/tools/internal/b stays as written\n", fsx.Read(t, filepath.Join(out, "cmd/app/testdata/golden.txt")), "non-Go file rewritten")
	assert.Contains(t, fsx.Read(t, filepath.Join(out, "internal/b/b.go")), "\"ex.com/sprint/internal/c\"", "copied file not rewritten")
	assert.Contains(t, fsx.Read(t, filepath.Join(out, "internal/b/suite.go")), "\"ex.com/sprint/internal/c\"", "embedded Go source import not rewritten")
	for _, f := range []string{"internal/b/data/x.txt", "internal/b/NOTE.md", "internal/fleetrules/rules.go", "internal/fleetrules/rules.txt",
		"docs/SPEC.md", "docs/ratings/1.0/app-read.md", "docs/ratings/1.0/x/app-use.md",
		"tla/M.tla", "tla/MCM.tla", "tla/Helper.tla", "tla/Leaf.tla", "tla/MCM.cfg", "tla/MCMBroken.cfg", "tla/README-M.md", "tla/m-bench/run.txt",
		"README.md", "tools/seed/x.go", "go.sum"} {
		assert.True(t, fsx.Exists(t, filepath.Join(out, f)), "%s missing", f)
	}
	for _, f := range []string{"internal/b/b_test.go", "internal/never/n.go", "internal/unused/u.go", "fleet/rules.go",
		"internal/fleetrules/loops.yml", "docs/OTHER.md", "docs/ratings/1.0/other-read.md", "tla/MCN.tla", "tla/MCN.cfg"} {
		assert.False(t, fsx.Exists(t, filepath.Join(out, f)), "%s taken", f)
	}
	assert.Equal(t, "# The app\n\nMoved.\n\n## app\n\napp verbs\n\n\n", fsx.Read(t, filepath.Join(out, "docs/CLI.md")), "CLI.md")
	assert.Equal(t, "config\tmodule\nMCM.cfg\tMCM.tla\nMCMBroken.cfg\tMCM.tla\n", fsx.Read(t, filepath.Join(out, "tla/CASES.tsv")), "CASES")
	assert.Equal(t, "config\tmodule\trest\nMCM.cfg\tMCM.tla\t1\n", fsx.Read(t, filepath.Join(out, "tla/RUNS.tsv")), "RUNS")
	assert.Equal(t, "module ex.com/sprint\n\ngo 1.26\n\ntool ex.com/dead\n\nrequire ex.com/dep v1.0.0\n", fsx.Read(t, filepath.Join(out, "go.mod")), "go.mod")
	assert.Equal(t, sprint.FormatNovaToolsVersion("v1.1.0", testRelease.commit), fsx.Read(t, filepath.Join(out, sprint.NovaToolsVersionFile)), "runtime pin")
	assert.Contains(t, fsx.Read(t, filepath.Join(out, "internal/sprint/toolsversion.go")), `NovaToolsVersion = "v1.1.0"`, "runtime version constant not synchronized")
}

func TestSeedSynchronizesCanaryPinAndRuntimeConstant(t *testing.T) {
	t.Parallel()
	from, keep := fixture(t)
	r, err := parseRecipe(testRecipe)
	require.NoError(t, err)
	rel := release{tag: "v1.2.0-local.20261008.1", commit: testRelease.commit}
	out := filepath.Join(t.TempDir(), "out")
	_, err = run(r, rel, from, keep, out)
	require.NoError(t, err)
	want := sprint.FormatNovaToolsVersion(rel.tag, rel.commit)
	assert.Equal(t, want, fsx.Read(t, filepath.Join(out, sprint.NovaToolsVersionFile)), "pin")
	assert.Contains(t, fsx.Read(t, filepath.Join(out, "internal/sprint/toolsversion.go")), `NovaToolsVersion = "`+rel.tag+`"`, "runtime constant not synchronized")
}

func TestTheSeedRefusesWithoutAReleaseToRecord(t *testing.T) {
	t.Parallel()
	from, keep := fixture(t)
	r, err := parseRecipe(testRecipe)
	require.NoError(t, err)
	out := filepath.Join(t.TempDir(), "out")
	_, err = run(r, release{}, from, keep, out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), sprint.NovaToolsVersionFile)
	assert.False(t, fsx.Exists(t, out), "%s written", out)
}

func TestTheSeedRecordsTheCheckoutsReleaseTag(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=seed", "-c", "user.email=seed@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v\n%s", args, out)
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "one")
	_, err := checkoutRelease(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no release tag")
	git("tag", "-a", "v1.2.0-local.20261008.1", "-m", "private canary")
	git("tag", "v1.3.0+build.1")
	git("tag", "nightly")
	canary, err := checkoutRelease(dir)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.0-local.20261008.1", canary.tag)
	assert.NotEmpty(t, canary.commit)
	git("tag", "v1.0.0")
	git("tag", "v1.1.0")
	rel, err := checkoutRelease(dir)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.0-local.20261008.1", rel.tag)
	assert.Len(t, rel.commit, 40)
	stray := filepath.Join(dir, "stray.txt")
	require.NoError(t, os.WriteFile(stray, []byte("not in the release\n"), 0o644))
	_, err = checkoutRelease(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "uncommitted changes")
	require.NoError(t, os.Remove(stray))
	_, err = checkoutRelease(dir)
	require.NoError(t, err)
	git("commit", "-q", "--allow-empty", "-m", "two")
	assert.Error(t, func() error { _, err := checkoutRelease(dir); return err }(), "a commit past the tag recorded the tag")
}

func TestTheSeedRefusesAnOutThatExists(t *testing.T) {
	t.Parallel()
	from, keep := fixture(t)
	r, err := parseRecipe(testRecipe)
	require.NoError(t, err)
	out := t.TempDir()
	_, err = run(r, testRelease, from, keep, out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestTheSeedRefusesAMissingPiece(t *testing.T) {
	t.Parallel()
	for name, line := range map[string]string{
		"a doc":                 "doc\tdocs/NONE.md\n",
		"a section":             "section\tdocs/CLI.md\tnone\n",
		"a model":               "model\tNone\n",
		"a path literal source": "literal-file\tcmd/app/missing.go\tbefore\tafter\n",
		"a path literal anchor": "literal-file\tcmd/app/main.go\tmissing source\tinserted source\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			from, keep := fixture(t)
			r, err := parseRecipe(testRecipe + line)
			require.NoError(t, err)
			_, err = run(r, testRelease, from, keep, filepath.Join(t.TempDir(), "out"))
			assert.Error(t, err, "no error")
		})
	}
}

func TestTheRecipeRefusesABadLine(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"move\n", "frobnicate\tx\n", "rename\tfleet\n", "move\tx\n", "literal-file\tpath\tx\n"} {
		_, err := parseRecipe(bad)
		assert.Errorf(t, err, "%q accepted", bad)
	}
}

func TestTheShippedRecipeParses(t *testing.T) {
	t.Parallel()
	r, err := readRecipe("RECIPE")
	require.NoError(t, err)
	assert.Equal(t, "github.com/mas-bandwidth/nova-sprint", r.toMod)
	assert.NotEmpty(t, r.move)
	assert.True(t, r.gomod)
}
