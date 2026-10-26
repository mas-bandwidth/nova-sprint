package cardgen

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// IsTestFile is the one home of "is this path a test file" for the sprint, in every
// language the stream lands (docs/SPEC-SPRINT.md section 7): *_test.*, *.test.*, test_*,
// and any path under a tests/ or __tests__/ directory. Each row says why it is accepted
// or refused, so a near miss cannot drift into the set unnoticed.
func TestOneTestFilePredicateForEveryLanguage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		want   bool
		reason string
	}{
		// Go
		{"a_test.go", true, "a Go test file is <stem>_test.<ext>"},
		{"./internal/x/y_test.go", true, "a leading ./ is dropped before the basename is read"},
		{"x_test.go", true, "<stem>_test.<ext> with a one-letter stem"},
		// C
		{"parser_test.c", true, "a C test file is <stem>_test.<ext>"},
		{"test_net.c", true, "a C test file named test_<rest>"},
		{"src/tests/helpers.c", true, "a C helper under a tests/ directory"},
		// C++
		{"net_test.cpp", true, "a C++ test file is <stem>_test.<ext>"},
		{"parser.test.c", true, "a C test file in the <stem>.test.<ext> shape"},
		// Python
		{"util_test.py", true, "a Python test file is <stem>_test.<ext>"},
		{"test_parser.py", true, "a Python test file named test_<rest>"},
		{"tests/conftest.py", true, "a Python fixture under a tests/ directory"},
		{"tests/integration.rs", true, "a Rust integration test under a tests/ directory"},
		// JavaScript and TypeScript
		{"a_test.js", true, "a JavaScript test file is <stem>_test.<ext>"},
		{"button.test.js", true, "a JavaScript test file is <stem>.test.<ext>"},
		{"api.test.ts", true, "a TypeScript test file is <stem>.test.<ext>"},
		{"lib/__tests__/util.js", true, "a JavaScript test under a __tests__/ directory"},
		// Lua
		{"x_test.lua", true, "a Lua test file is <stem>_test.<ext>"},
		{"test_x.lua", true, "a Lua test file named test_<rest>"},
		// Rust
		{"mod_test.rs", true, "a Rust test file is <stem>_test.<ext>"},
		{"test_mod.rs", true, "a Rust test file named test_<rest>"},

		// near misses
		{"contest.go", false, `"contest" carries the letters but no _test/.test/test_ boundary`},
		{"latest.c", false, `"latest" carries the letters but no _test/.test/test_ boundary`},
		{"testdata_helper.go", false, "a test prefix with no underscore straight after it is not test_"},
		{"test.go", false, "a bare word is source: there is no stem"},
		{"tests.go", false, "a bare word is source: there is no stem"},
		{"testing.go", false, "a bare word is source; testing is not a test name"},
		{"internal/x/test.go", false, "a bare word in a directory is source, as AlwaysInPaths already refuses"},
		{"internal/x/testdata.go", false, "a testdata prefix is not testdata/; AlwaysInPaths already refuses it"},
		{"mytests/a.go", false, "the directory segment must be exactly tests or __tests__"},
		{"test/a.c", false, "a bare test/ directory is not taken in this card"},
		{"a.spec.js", false, "the spec convention is not in the asked set"},
		{"", false, "the empty path names no file"},
		{".", false, "the repository root is not a file"},
		{"../x_test.go", false, "a path that climbs out of the repository is refused"},
		{"/abs/x_test.go", false, "a path that starts at the filesystem root is outside the repository"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsTestFile(tc.name), tc.reason)
		})
	}
}
