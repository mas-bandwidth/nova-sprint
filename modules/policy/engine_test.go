package policy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// appended is the default body with extra after it, and the line extra starts on.
func appended(extra string) (string, int) {
	return defaultBody + extra, strings.Count(defaultBody, "\n") + 1
}

// TestCompileRefusesForbidden: each thing the embedding forbids is refused
// at compile with the version and the line named.
func TestCompileRefusesForbidden(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, extra, want string }{
		{"while", "def _w():\n    while True:\n        pass\n", "this Starlark dialect does not support while loops"},
		{"top-level for", "for _x in []:\n    pass\n", "for loop not within a function"},
		{"top-level if", "if True:\n    pass\n", "if statement not within a function"},
		{"load", "load(\"other.star\", \"x\")\n", "load not implemented by this application"},
		{"global reassignment", "_A = 1\n_A = 2\n", "cannot reassign global _A declared at v1.star:%d:1"},
		{"set", "_S = set()\n", "this Starlark dialect does not support sets"},
		{"a clock", "_T = time.now()\n", "undefined: time"},
		{"random", "_R = random()\n", "undefined: random"},
		{"a file", "_F = open(\"/etc/passwd\")\n", "undefined: open"},
		{"top-level step budget", "_X = [x for x in range(100000000)]\n", "over the step budget of 1000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body, line := appended(tc.extra)
			_, f := compile(1, "v1", body, newThread("v1"))
			require.NotNil(t, f)
			want := tc.want
			if strings.Contains(want, "%d") {
				want = fmt.Sprintf(want, line)
			}
			errLine := line
			if tc.name == "global reassignment" || tc.name == "while" {
				errLine = line + 1
			}
			assert.Equal(t, fmt.Sprintf("POLICY REFUSED v1 line %d: %s", errLine, want), f.Error())
		})
	}
}

// TestCompileRefusesNames: the closed set of top-level names and each
// question's arity, with exact text.
func TestCompileRefusesNames(t *testing.T) {
	t.Parallel()
	misspelt, line := appended("def teir(card):\n    return \"pro\"\n")
	unknown, unknownLine := appended("teir = 1\n")
	arity, arityLine := override("reads", "def reads(card, extra):\n    return 1")
	notFn, notFnLine := override("order", "order = (1,)")
	missing, _ := override("prices", "")
	names := "catalog landing limits order prices reads rows stitch timeouts verdict width"
	for _, tc := range []struct{ name, body, want string }{
		{"misspelt question", misspelt, fmt.Sprintf("POLICY REFUSED v1 line %d: unknown top-level name teir: a question is one of %s, a helper starts with _", line, names)},
		{"unknown value", unknown, fmt.Sprintf("POLICY REFUSED v1 line %d: unknown top-level name teir: a question is one of %s, a helper starts with _", unknownLine, names)},
		{"wrong arity", arity, fmt.Sprintf("POLICY REFUSED v1 line %d: reads takes 1 parameter(s): def reads(card)", arityLine)},
		{"not a function", notFn, fmt.Sprintf("POLICY REFUSED v1 line %d: order is a tuple, not a function: define def order(card)", notFnLine)},
		{"missing question", missing, fmt.Sprintf("POLICY REFUSED v1 line %d, end of file: question prices is missing: define def prices()", strings.Count(strings.TrimRight(missing, "\n"), "\n")+1)}, // the file's last line
		{"over 64 KiB", defaultBody + "#" + strings.Repeat("x", MaxBody) + "\n", fmt.Sprintf("POLICY REFUSED v1 line %d, end of file: the file is %d bytes, over 65536", strings.Count(defaultBody, "\n")+1, len(defaultBody)+MaxBody+2)},
	} {
		_, f := compile(1, "v1", tc.body, newThread("v1"))
		require.NotNil(t, f, tc.name)
		assert.Equal(t, tc.want, f.Error(), tc.name)
	}
	lambda, _ := override("order", "order = lambda card: (1,)")
	_, f := compile(1, "v1", lambda, newThread("v1"))
	assert.Nil(t, f, "a lambda is a function")
}

// TestRuntimeForbidden: recursion, a frozen global's mutation and an
// unbounded comprehension fail at call time with the line named; the
// tick falls back to the default.
func TestRuntimeForbidden(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	recursion, _ := override("order", "def order(card):\n    return _f(card)")
	recursion += "def _f(c):\n    return _f(c)\n"
	recLine := strings.Count(recursion, "\n") // the line of the recursive call
	frozen, frozenLine := override("order", "def order(card):\n    _L.append(1)\n    return (1,)")
	frozen = "_L = []\n" + frozen
	unbounded, budgetLine := override("order", "def order(card):\n    return (len([x for x in range(100000000)]),)")
	for _, tc := range []struct{ name, body, want string }{
		{"recursion", recursion, fmt.Sprintf("POLICY REFUSED v2 line %d order(p1): function _f called recursively", recLine)},
		{"frozen global", frozen, fmt.Sprintf("POLICY REFUSED v2 line %d order(p1): append: cannot append to frozen list", frozenLine+2)},
		{"step budget", unbounded, fmt.Sprintf("POLICY REFUSED v2 line %d order(p1): over the step budget of 1000000", budgetLine+1)},
	} {
		a, fs := testEngine().Answer(Versions{Active: pv(2, tc.body)}, w.snap(), w.ro, FullQuestions(w.snap(), nil))
		require.Len(t, fs, 1, tc.name)
		assert.Equal(t, tc.want, fs[0].Error(), tc.name)
		assert.Equal(t, 0, a.Version, "%s: the default answered", tc.name)
	}
}

// TestCompileOncePerVersion: a version is compiled once however many ticks
// it answers; the built-in is compiled only when it is needed.
func TestCompileOncePerVersion(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	e := testEngine()
	for range 5 {
		a, fs := e.Answer(Versions{Active: pv(1, defaultBody)}, w.snap(), w.ro, FullQuestions(w.snap(), nil))
		require.Empty(t, fs)
		assert.Equal(t, 1, a.Version)
	}
	assert.Equal(t, 1, e.Compiles())
}

// TestDeterministic: one version and one input give one answer, over many
// calls and fresh engines (no cache) alike.
func TestDeterministic(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	q := FullQuestions(w.snap(), nil)
	first, fs := testEngine().Answer(Versions{Active: pv(1, defaultBody)}, w.snap(), w.ro, q)
	require.Empty(t, fs)
	cached := testEngine()
	for range 20 {
		again, _ := testEngine().Answer(Versions{Active: pv(1, defaultBody)}, w.snap(), w.ro, q)
		assert.Equal(t, first, again)
		warm, _ := cached.Answer(Versions{Active: pv(1, defaultBody)}, w.snap(), w.ro, q)
		assert.Equal(t, first, warm)
	}
}
