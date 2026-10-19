package sprint_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rename guard in LandScope reads cardgen.AlwaysInPaths for both sides of a rename
// (docs/SPEC-SPRINT.md section 7, the lander's checks): moving a source file to a test
// name is refused in every language IsTestFile accepts, while a new file that merely has
// a test name is inside every PATHS by rule. The rigs are the existing mergeRig git rig
// in t.TempDir().
func TestARenameToAnyLanguagesTestNameIsStillRefused(t *testing.T) {
	t.Parallel()
	paths := []string{"src/main.c"}
	t.Run("source renamed to a test name in C, Python and JavaScript is refused", func(t *testing.T) {
		t.Parallel()
		base := map[string]string{
			"src/main.c":   "int main(void) { return 0; }\n",
			"src/parser.c": "int parse(void) { return 0; }\n",
			"lib/util.py":  "def run():\n    return 1\n",
			"lib/util.js":  "export const run = () => 1;\n",
		}
		r := newMergeRig(t, base, map[string]string{"src/main.c": "int main(void) { return 1; }\n"})
		require.NoError(t, os.MkdirAll(filepath.Join(r.dir, "tests"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(r.dir, "lib", "__tests__"), 0o755))
		r.must("mv", "src/parser.c", "src/parser_test.c")
		r.must("mv", "lib/util.py", "tests/util.py")
		r.must("mv", "lib/util.js", "lib/__tests__/util.js")
		r.must("commit", "-q", "-m", "move source files onto test names")
		_, _, _, refused := r.landScope(paths)
		assert.ElementsMatch(t, []string{"src/parser_test.c", "tests/util.py", "lib/__tests__/util.js"}, refused)
	})
	t.Run("a new test-named file is not refused", func(t *testing.T) {
		t.Parallel()
		r := newMergeRig(t, map[string]string{"src/main.c": "int main(void) { return 0; }\n"},
			map[string]string{
				"src/main.c":         "int main(void) { return 1; }\n",
				"src/parser_test.c":  "int parse(void) { return 0; }\n",
				"tests/test_util.py": "def run():\n    return 1\n",
			})
		_, _, amended, refused := r.landScope(paths)
		assert.Empty(t, refused, "a new file with a test name is inside every PATHS by rule")
		assert.Empty(t, amended, "inside by rule is not a scope amendment")
	})
}
