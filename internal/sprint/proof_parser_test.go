package sprint

import (
	"context"
	"sort"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The real parser checks of the mechanical proof (proof_parser.go): a delete
// whose symbol is gone passes, one with a reference left is a finding naming
// it; a rename whose old name is gone and new name is present passes; an
// assertion rewrite that changes only assertion lines of a test file passes.
// The checks read a WorkView alone, so the tests drive them without git.

// proofView is a head view over files: the head's tracked paths, the diff, and
// Show. The files are the head's tree.
func proofView(diff string, files map[string]string) WorkView {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return WorkView{
		Head:    "abc1234",
		Pushed:  true,
		Tracked: paths,
		Diff:    diff,
		Show: func(p string) ([]byte, bool) {
			s, ok := files[p]
			return []byte(s), ok
		},
	}
}

// proofDiffLines is a unified diff of one file: the header, then the hunk's
// lines verbatim ("@@ ... @@" then the body's lines with their markers).
func proofDiffLines(path, header string, body ...string) string {
	out := []string{
		"diff --git a/" + path + " b/" + path,
		"--- a/" + path,
		"+++ b/" + path,
		header,
	}
	return joinLines(append(out, body...))
}

func joinLines(lines []string) string {
	s := ""
	for i, l := range lines {
		if i > 0 {
			s += "\n"
		}
		s += l
	}
	return s + "\n"
}

func TestTheParserProofChecksADeletionOverTheHead(t *testing.T) {
	t.Parallel()

	t.Run("a symbol the diff removes and the head no longer names passes", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a.go", "@@ -1,2 +1,1 @@", "-func Old() {", "-}", "+"),
			map[string]string{"a/a.go": "package a\n"},
		)
		run := ParserProof(cardhdr.KindDelete, v)
		assert.Equal(t, cardhdr.KindDelete, run.Kind)
		assert.Empty(t, run.Failed)
		assert.Empty(t, run.Waiting)
		require.NotEmpty(t, run.Lines)
		assert.Contains(t, run.Lines[0], "Old")
		assert.Contains(t, run.Lines[0], "no references")
	})

	t.Run("a reference left is a finding at the file and line", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a.go", "@@ -1,2 +1,1 @@", "-func Old() {", "-}", "+"),
			map[string]string{"a/a.go": "package a\n\nfunc New() { Old() }\n"},
		)
		run := ParserProof(cardhdr.KindDelete, v)
		require.Len(t, run.Failed, 1)
		assert.Equal(t, "a/a.go", run.Failed[0].File)
		assert.Contains(t, run.Failed[0].What, "reference to deleted symbol Old remains")
	})

	t.Run("a declaration the head still carries is a finding", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a.go", "@@ -1,2 +1,1 @@", "-func Old() {", "-}", "+"),
			map[string]string{"a/a.go": "package a\n\nfunc Old() {}\n"},
		)
		run := ParserProof(cardhdr.KindDelete, v)
		require.Len(t, run.Failed, 1)
		assert.Contains(t, run.Failed[0].What, "Old")
	})

	t.Run("a declaration the diff changes but puts back is not deleted", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a.go", "@@ -1,3 +1,1 @@",
				"-func Old() {}", "-", "-func Keep() { Old() }", "+func Keep() {}"),
			map[string]string{"a/a.go": "package a\n\nfunc Keep() {}\n"},
		)
		run := ParserProof(cardhdr.KindDelete, v)
		assert.Empty(t, run.Failed)
		require.NotEmpty(t, run.Lines)
		assert.Contains(t, run.Lines[0], "Old")
	})

	t.Run("a diff that removes no declaration is a finding", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a.go", "@@ -1,1 +1,1 @@", "-x := 1", "+x := 2"),
			map[string]string{"a/a.go": "package a\n"},
		)
		run := ParserProof(cardhdr.KindDelete, v)
		require.Len(t, run.Failed, 1)
		assert.Contains(t, run.Failed[0].What, "removes no declaration")
	})
}

func TestTheParserProofChecksARenameOverTheHead(t *testing.T) {
	t.Parallel()

	t.Run("the old name gone and the new name present passes", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a.go", "@@ -1,1 +1,1 @@", "-func Old() {", "+func New() {"),
			map[string]string{"a/a.go": "package a\n\nfunc New() {}\n"},
		)
		run := ParserProof(cardhdr.KindRename, v)
		assert.Empty(t, run.Failed)
		assert.Empty(t, run.Waiting)
		require.NotEmpty(t, run.Lines)
		assert.Contains(t, run.Lines[0], "Old -> New")
	})

	t.Run("the old name left behind is a finding", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a.go", "@@ -1,1 +1,1 @@", "-func Old() {", "+func New() {"),
			map[string]string{"a/a.go": "package a\n\nfunc New() {}\n\nfunc Other() { Old() }\n"},
		)
		run := ParserProof(cardhdr.KindRename, v)
		require.Len(t, run.Failed, 1)
		assert.Contains(t, run.Failed[0].What, "old name Old is still present")
	})

	t.Run("the new name missing is a finding", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a.go", "@@ -1,1 +1,1 @@", "-func Old() {", "+func New() {"),
			map[string]string{"a/a.go": "package a\n"},
		)
		run := ParserProof(cardhdr.KindRename, v)
		require.Len(t, run.Failed, 1)
		assert.Contains(t, run.Failed[0].What, "new name New is not present")
	})
}

func TestTheParserProofChecksAnAssertionRewriteOverTheDiff(t *testing.T) {
	t.Parallel()

	t.Run("only assertion lines of a test file passes", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a_test.go", "@@ -1,1 +1,2 @@", " \tassert.Equal(t, 1, 2)", "+\tassert.Equal(t, 2, 2)"),
			map[string]string{"a/a_test.go": "package a\n"},
		)
		run := ParserProof(cardhdr.KindRewrite, v)
		assert.Empty(t, run.Failed)
		assert.Empty(t, run.Waiting)
		require.NotEmpty(t, run.Lines)
	})

	t.Run("a changed line that asserts nothing is a finding", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a_test.go", "@@ -1,1 +1,2 @@", " \tassert.Equal(t, 1, 2)", "+\tx := 1"),
			map[string]string{"a/a_test.go": "package a\n"},
		)
		run := ParserProof(cardhdr.KindRewrite, v)
		require.Len(t, run.Failed, 1)
		assert.Contains(t, run.Failed[0].What, "not an assertion line")
	})

	t.Run("a code file is a finding", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a.go", "@@ -1,1 +1,2 @@", " \tprintln(1)", "+\tassert.Equal(t, 1, 2)"),
			map[string]string{"a/a.go": "package a\n"},
		)
		run := ParserProof(cardhdr.KindRewrite, v)
		require.Len(t, run.Failed, 1)
		assert.Contains(t, run.Failed[0].What, "not a test file")
	})

	t.Run("removed non-assertion lines in a test file is a finding", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a_test.go", "@@ -1,2 +1,1 @@", "\tassert.Equal(t, 1, 2)", "-\tx := 1", "+\tassert.Equal(t, 2, 2)"),
			map[string]string{"a/a_test.go": "package a\n"},
		)
		run := ParserProof(cardhdr.KindRewrite, v)
		require.Len(t, run.Failed, 1)
		assert.Contains(t, run.Failed[0].What, "not an assertion line")
	})

	t.Run("added line merely containing assertion substring is a finding", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a_test.go", "@@ -1,1 +1,2 @@", " \tassert.Equal(t, 1, 2)", "+\tx := 1 // assert this is a comment"),
			map[string]string{"a/a_test.go": "package a\n"},
		)
		run := ParserProof(cardhdr.KindRewrite, v)
		require.Len(t, run.Failed, 1)
		assert.Contains(t, run.Failed[0].What, "not an assertion line")
	})

	t.Run("assert.State() is not an assertion line", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a_test.go", "@@ -1,1 +1,2 @@", " \tassert.Equal(t, 1, 2)", "+\tassert.State(t, \"foo\")"),
			map[string]string{"a/a_test.go": "package a\n"},
		)
		run := ParserProof(cardhdr.KindRewrite, v)
		require.Len(t, run.Failed, 1)
		assert.Contains(t, run.Failed[0].What, "not an assertion line")
	})

	t.Run("require.State() is not an assertion line", func(t *testing.T) {
		t.Parallel()
		v := proofView(
			proofDiffLines("a/a_test.go", "@@ -1,1 +1,2 @@", " \trequire.Equal(t, 1, 2)", "+\trequire.State(t, \"foo\")"),
			map[string]string{"a/a_test.go": "package a\n"},
		)
		run := ParserProof(cardhdr.KindRewrite, v)
		require.Len(t, run.Failed, 1)
		assert.Contains(t, run.Failed[0].What, "not an assertion line")
	})
}

// TestNewParserProofReadsTheHeadAndRunsTheCheck drives the runner the tick
// uses: the brief's repository and the card's head are read through the clone
// and the view, and the kind's check runs over the view.
func TestNewParserProofReadsTheHeadAndRunsTheCheck(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	pr := w.s.Work.Card("s1-1")
	pr.Fields["brief"] = "REPO: o/r\nKIND: delete\n"
	pr.Fields["head"] = "abc1234"
	v := proofView(
		proofDiffLines("a/a.go", "@@ -1,2 +1,1 @@", "-func Old() {", "-}", "+"),
		map[string]string{"a/a.go": "package a\n"},
	)
	p := NewParserProof(ProofGit{
		Clone: func(string) (string, error) { return "/clone", nil },
		View:  func(context.Context, string, string, string, []string) (WorkView, error) { return v, nil },
		Host:  "machine",
	})
	run := p(w.s, pr)
	assert.Equal(t, cardhdr.KindDelete, run.Kind)
	assert.Equal(t, "machine", run.Host)
	assert.Empty(t, run.Failed)
	assert.Empty(t, run.Waiting)
	require.NotEmpty(t, run.Lines)
}

func TestNewParserProofWaitsWithNoCloneOrNoHead(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	pr := w.s.Work.Card("s1-1")
	pr.Fields["brief"] = "REPO: o/r\nKIND: delete\n"
	pr.Fields["head"] = "abc1234"

	// no clone seam: no proof host is configured
	run := NewParserProof(ProofGit{Clone: nil})(w.s, pr)
	assert.NotEmpty(t, run.Waiting)

	// a clone, but the attempt names no pushed head
	pr.Fields["head"] = ""
	run = NewParserProof(ProofGit{Clone: func(string) (string, error) { return "/clone", nil }})(w.s, pr)
	assert.NotEmpty(t, run.Waiting)
	assert.Contains(t, run.Waiting, "no pushed head")

	// a clone and a head, but the view does not have it
	pr.Fields["head"] = "abc1234"
	run = NewParserProof(ProofGit{
		Clone: func(string) (string, error) { return "/clone", nil },
		View:  func(context.Context, string, string, string, []string) (WorkView, error) { return WorkView{}, nil },
	})(w.s, pr)
	assert.NotEmpty(t, run.Waiting)
	assert.Contains(t, run.Waiting, "is not in the repository")
}

// TestTheProofEnvBindsTheDefaultProof pins the binding the STOP names: the
// proof is not an unwired seam. A start with NOVA_SPRINT_PROOF_BENCH names the
// machine proof; with no host it names none.
func TestTheProofEnvBindsTheDefaultProof(t *testing.T) {
	was := DefaultProof
	t.Cleanup(func() { DefaultProof = was })

	assert.Nil(t, proofFromEnv(func(string) string { return "" }), "no host named is no proof")
	p := proofFromEnv(func(k string) string {
		if k == ProofBenchEnv {
			return "bench-a, bench-b"
		}
		return ""
	})
	require.NotNil(t, p, "a named host binds the machine proof")
	assert.Equal(t, []string{"bench-a", "bench-b"}, proofHosts(" bench-a,bench-b,bench-a "))
}
