package swarm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registryTree makes a throwaway git repository under t.TempDir() holding files, and returns
// its directory and the full sha of its one commit: the BASE tip a brief is read against.
func registryTree(t *testing.T, files map[string]string) (dir, sha string) {
	t.Helper()
	dir = t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	return dir, git("rev-parse", "HEAD")
}

// registryFindings is the paths-cover-registry findings, and the corrected lines, LintBrief
// draws on raw against bb.
func registryFindings(raw string, bb BriefBase) ([]CardHeaderFinding, BriefFix) {
	fs, fix := LintBrief([]byte(raw), bb)
	return findingsWith(fs, "paths-cover-registry"), fix
}

// deltaQuaternionsBrief is the header of delta-quaternions-reject-zero-and-normalize-safely as
// sprint 21 admitted it (attempt 2 was refused by the lander, E12, for changing Makefile), with
// the PATHS line given.
func deltaQuaternionsBrief(paths string) string {
	return "RESULT: delta-quaternions-reject-zero-and-normalize-safely sha=170de1b39b1f tier: heavy\n" +
		"REPO: robotgamedevelopers/delta\n" +
		"BASE: sprint/delta-audit\n" +
		"KIND: fix-red\n" +
		"PATHS: " + paths + "\n" +
		"NEW: test/delta_quaternion_zero_test.cpp\n" +
		"TEST: none the suite is C++, so the Go-only TEST form cannot name it; DONE-WHEN names its runner and the test function TestZeroQuaternionNeverReachesNormalize\n" +
		"START: delta_quaternion.cpp, delta_quaternion.h, delta_encoding.h, test/delta_quaternion_zero_test.cpp\n\n" +
		"THE TASK. normalize returns the identity quaternion when norm_squared <= 0; the readers refuse (0,0,0,0).\n"
}

// box3dReferenceBrief is the header of fixed3d-box3d-reference-harness as sprint 21 admitted
// it (attempts refused by the lander, E12, for changing test/main.c), with the PATHS line given.
func box3dReferenceBrief(paths string) string {
	return "RESULT: fixed3d-box3d-reference-harness sha=1df2e22f8899 tier: heavy\n" +
		"REPO: mas-bandwidth/fixed3d\n" +
		"BASE: sprint/fixed3d-box3d-ports\n" +
		"KIND: port\n" +
		"PATHS: " + paths + "\n" +
		"START: test/test_determinism.c (read)\n" +
		"NEW: tools/reference/README.md, tools/reference/ref_scene.c, tools/reference/scenes/*.scene, test/test_reference_scene.c\n" +
		"TEST: none because the suites are C and not a Go package; STEP 2 names the red-first test ReferenceSceneParseTest and the command that runs it\n\n" +
		"THE TASK. Build the float box3d reference harness; nothing in src/ or include/ changes.\n"
}

// The paths-cover-registry rule, replayed from sprint 21's lander E12 refusals: a brief that
// adds a C or C++ test source is refused when a file at the BASE tip that registers its
// sibling tests (a Makefile, test/main.c, a CMakeLists.txt naming them) is not on PATHS, the
// finding naming the registry and the corrected PATHS line carrying it; with the registry on
// PATHS the brief passes; a tree with no registry draws no finding; a Go test needs none; and
// with no tree handed over the finding is MISSING.
func TestPathsCoverTheTestRegistry(t *testing.T) {
	t.Parallel()

	// delta: the Makefile lists every test source.
	deltaDir, deltaSha := registryTree(t, map[string]string{
		"Makefile":             "TESTS = test/delta_test.cpp\n\ntest: $(TESTS)\n\t$(CXX) -o delta_test $(TESTS)\n",
		"delta_quaternion.cpp": "// q\n",
		"delta_quaternion.h":   "// q\n",
		"delta_encoding.h":     "// e\n",
		"test/delta_test.cpp":  "int main() { return 0; }\n",
	})
	delta := BriefBase{Repo: deltaDir, Sha: deltaSha}
	admitted := "delta_quaternion.cpp,delta_quaternion.h,delta_encoding.h,test/delta_quaternion_zero_test.cpp"
	fs, fix := registryFindings(deltaQuaternionsBrief(admitted), delta)
	require.Len(t, fs, 1, "the delta brief as admitted is refused")
	assert.Equal(t, 5, fs[0].Line, "the finding points at PATHS")
	assert.Contains(t, fs[0].Excerpt, `"Makefile"`, "the finding names the registry")
	assert.Contains(t, fs[0].Excerpt, "test/delta_quaternion_zero_test.cpp", "and the test source it adds")
	assert.Contains(t, fix, "PATHS: "+admitted+",Makefile", "the corrected PATHS line carries the registry")
	fs, _ = registryFindings(deltaQuaternionsBrief(admitted+",Makefile"), delta)
	assert.Empty(t, fs, "with the Makefile on PATHS the delta brief passes")

	// box3d: test/main.c runs every suite; test/CMakeLists.txt builds every test file.
	boxDir, boxSha := registryTree(t, map[string]string{
		"CMakeLists.txt":            "project(box3d C)\nadd_subdirectory(test)\n",
		"test/CMakeLists.txt":       "add_executable(test main.c test_world.c test_determinism.c)\n",
		"test/main.c":               "int test_world(void);\nint main(void) { RunSuite(\"test_world\", test_world); RunSuite(\"test_determinism\", test_determinism); return 0; }\n",
		"test/test_world.c":         "int test_world(void) { return 0; }\n",
		"test/test_determinism.c":   "int test_determinism(void) { return 0; }\n",
		"tools/reference/README.md": "x\n",
	})
	box := BriefBase{Repo: boxDir, Sha: boxSha}
	admitted = "CMakeLists.txt, test/CMakeLists.txt, CLAUDE.md"
	fs, fix = registryFindings(box3dReferenceBrief(admitted), box)
	require.Len(t, fs, 1, "the fixed3d brief as admitted is refused")
	assert.Contains(t, fs[0].Excerpt, `"test/main.c"`, "the finding names test/main.c")
	assert.NotContains(t, fs[0].Excerpt, `"test/CMakeLists.txt"`, "a registry already on PATHS is not named")
	assert.NotContains(t, fs[0].Excerpt, `"CMakeLists.txt"`, "the root CMakeLists.txt names no test source")
	assert.Contains(t, fix, "PATHS: CMakeLists.txt,test/CMakeLists.txt,CLAUDE.md,test/main.c")
	fs, _ = registryFindings(box3dReferenceBrief(admitted+", test/main.c"), box)
	assert.Empty(t, fs, "with test/main.c on PATHS the fixed3d brief passes")
	fs, _ = registryFindings(box3dReferenceBrief(admitted+", test/**"), box)
	assert.Empty(t, fs, "a glob covering the registry covers it")

	// a tree with no registry: the test sources are found by a glob, nothing names them.
	bareDir, bareSha := registryTree(t, map[string]string{
		"Makefile":             "TESTS = $(wildcard test/*.cpp)\n",
		"delta_quaternion.cpp": "// q\n",
		"delta_quaternion.h":   "// q\n",
		"delta_encoding.h":     "// e\n",
		"test/delta_test.cpp":  "int main() { return 0; }\n",
	})
	fs, _ = registryFindings(deltaQuaternionsBrief("delta_quaternion.cpp,delta_quaternion.h,delta_encoding.h,test/delta_quaternion_zero_test.cpp"), BriefBase{Repo: bareDir, Sha: bareSha})
	assert.Empty(t, fs, "a tree with no registry draws no finding")

	// a Go test needs no registry, and a brief that adds no test source is not this rule's.
	goBrief := "card x tier: pro\nREPO: example.com/m\nBASE: sprint/x\nPATHS: pkg/x/a.go\nNEW: test/a_test.go\n\nTHE TASK. x\n"
	fs, _ = registryFindings(goBrief, delta)
	assert.Empty(t, fs, "a _test.go source needs no registry")
	fs, _ = registryFindings("card x tier: pro\nREPO: example.com/m\nBASE: sprint/x\nPATHS: delta_quaternion.cpp\n\nTHE TASK. x\n", delta)
	assert.Empty(t, fs, "a brief adding no test source draws no finding")

	// no evidence is not negative evidence: no tree handed over is MISSING.
	fs, _ = registryFindings(deltaQuaternionsBrief(admitted), BriefBase{})
	require.Len(t, fs, 1)
	assert.True(t, strings.HasPrefix(fs[0].Excerpt, "MISSING"), fs[0].Excerpt)
	fs, _ = registryFindings(strings.Replace(deltaQuaternionsBrief(admitted), "REPO: robotgamedevelopers/delta\n", "", 1), BriefBase{})
	assert.Empty(t, fs, "a brief with no REPO has no base to read")

	assert.Contains(t, BriefRemedy("paths-cover-registry"), "registry")
}
