package sprint

import (
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The reach check of the machine lint (docs/SPEC-SPRINT.md section 6, the work lint): an
// exported function or method the change adds must have a reference from a non-test file
// in its package, read with go/parser. The view is hand-built here, so the test pins the
// check and reads no repository.

// reachView is a view whose package holds the sources, one per file, with the diff that
// adds them.
func reachView(diff string, sources map[string]string) WorkView {
	ls := make([]string, 0, len(sources))
	for name := range sources {
		ls = append(ls, name)
	}
	return WorkView{
		Diff: diff,
		Ls:   func(string) []string { return ls },
		Show: func(p string) ([]byte, bool) {
			src, ok := sources[path.Base(p)]
			return []byte(src), ok
		},
	}
}

const deadDiff = "diff --git a/internal/sprint/a.go b/internal/sprint/a.go\n" +
	"--- a/internal/sprint/a.go\n" +
	"+++ b/internal/sprint/a.go\n" +
	"@@ -1 +1,2 @@\n" +
	" package sprint\n" +
	"+func Dead() int { return 1 }\n"

func TestFindNonTestReferencesMarksANonTestReference(t *testing.T) {
	t.Parallel()
	v := reachView(deadDiff, map[string]string{
		"a.go": "package sprint\nfunc Dead() int { return 1 }\nfunc Call() int { return Dead() }\n",
	})
	reached := FindNonTestReferences(v, "internal/sprint", map[string]bool{"Dead": true})
	assert.True(t, reached["Dead"], "a call from a non-test file is a reference")
}

func TestFindNonTestReferencesIgnoresTheDeclarationAndTestFiles(t *testing.T) {
	t.Parallel()
	v := reachView(deadDiff, map[string]string{
		"a.go":      "package sprint\nfunc Dead() int { return 1 }\n",
		"a_test.go": "package sprint\nfunc TestDead(t *testing.T) { _ = Dead() }\n",
	})
	reached := FindNonTestReferences(v, "internal/sprint", map[string]bool{"Dead": true})
	assert.False(t, reached["Dead"], "the declaration is no reference, and a _test.go file is not read")
}

func TestReachFindingsNamesAnUnreachedExportedSymbol(t *testing.T) {
	t.Parallel()
	v := reachView(deadDiff, map[string]string{
		"a.go": "package sprint\nfunc Dead() int { return 1 }\n",
	})
	fs := reachFindings(v)
	require.Len(t, fs, 1, "one unreached symbol is one finding")
	assert.Equal(t, LintUnreached, fs[0].Token)
	assert.Contains(t, fs[0].String(), "Dead")
	assert.Contains(t, fs[0].String(), "(remedy: ")
}

func TestReachFindingsPassesAReachedSymbol(t *testing.T) {
	t.Parallel()
	v := reachView(deadDiff, map[string]string{
		"a.go": "package sprint\nfunc Dead() int { return 1 }\nfunc Call() int { return Dead() }\n",
	})
	assert.Empty(t, reachFindings(v), "a symbol a non-test file calls is reached")
}

func TestReachFindingsSkipsABodyOnlyChange(t *testing.T) {
	t.Parallel()
	diff := "diff --git a/internal/sprint/a.go b/internal/sprint/a.go\n" +
		"--- a/internal/sprint/a.go\n" +
		"+++ b/internal/sprint/a.go\n" +
		"@@ -1,2 +1,2 @@\n" +
		" package sprint\n" +
		"-func A() int { return 1 }\n" +
		"+func A() int { return 2 }\n"
	v := reachView(diff, map[string]string{
		"a.go": "package sprint\nfunc A() int { return 2 }\n",
	})
	assert.Empty(t, reachFindings(v), "a change that adds no exported symbol has no reach finding")
}
