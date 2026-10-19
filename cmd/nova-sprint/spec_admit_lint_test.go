package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The standard's R-LINT-ON-ADD (standard/sprint/s2.md, NS-2.2; the owner, 2026-10-18: "Card
// lint is always run on new cards."): every path that admits or replaces a brief runs the card
// lint and refuses a brief that fails it, writing nothing. This pins the paths that lint today;
// the twin, the machine's own cards, and the card checks on a brief edit and a recut are the
// row's divergences (standard/pins/s2.tsv, sprint-spec-fix-lint-on-every-admit).

// TestEveryAdmitPathRefusesALintFailingBrief: a brief that lacks the child rules is refused,
// exit 2, naming the lint's findings and that the brief fails the card lint, with nothing
// written, by add with the brief given by flag, by add from a file, by add from a directory, by add replacing a card, by a brief edit
// of an unstarted primary and by a recut with a new brief; each of those paths admits a brief
// that passes; and none of their verbs offers a flag that skips the lint.
func TestEveryAdmitPathRefusesALintFailingBrief(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 1 --one --brief-file " + writeBrief(t, "the old work"))
	ta.ok("add --stream s1 s1-r --one --brief-file " + writeBrief(t, "the work to recut"))

	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.md")
	require.NoError(t, os.WriteFile(bad, []byte("handle the empty case\n"), 0o600))
	badDir := filepath.Join(dir, "bad")
	require.NoError(t, os.MkdirAll(badDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "s2-1.md"), []byte("handle the empty case\n"), 0o600))
	goodDir := filepath.Join(dir, "good")
	require.NoError(t, os.MkdirAll(goodDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(goodDir, "s3-1.md"), []byte(passingBrief("the directory's work")), 0o600))

	paths := []struct{ path, bad, good string }{
		{"add --brief", "add --stream s4 --count 1 --one --brief 'handle the empty case'",
			"add --stream s4 --count 1 --one --brief '" + strings.ReplaceAll(passingBrief("the flag's work"), "'", "") + "'"},
		{"add --brief-file", "add --stream s2 --count 1 --one --brief-file " + bad,
			"add --stream s2 --count 1 --one --brief-file " + writeBrief(t, "the new work")},
		{"add --brief-dir", "add --stream s3 --brief-dir " + badDir,
			"add --stream s3 --brief-dir " + goodDir},
		{"add --replaces", "add --stream s1 s1-1-tb --replaces s1-1 --brief-file " + bad,
			"add --stream s1 s1-1-tb --replaces s1-1 --brief-file " + writeBrief(t, "the replacing work")},
		{"brief edit", "brief s1-r --brief-file " + bad,
			"brief s1-r --brief-file " + writeBrief(t, "the edited work")},
		{"recut --brief-file", "recut s1-r --brief-file " + bad,
			"recut s1-r --brief-file " + writeBrief(t, "the recut work")},
	}
	for _, c := range paths {
		before := ta.applies()
		code, out, errs := ta.do(c.bad)
		assert.Equal(t, 2, code, "%s: exit %d, out %q, errs %q; want the lint's refusal", c.path, code, out, errs)
		assert.NotContains(t, out, "MOVED", "%s: nothing moves", c.path)
		for _, finding := range []string{"rule-worktree", "rule-no-server", "rule-report-not-done", "fails the card lint"} {
			assert.Contains(t, errs, finding, "%s: the refusal names %q", c.path, finding)
		}
		assert.Equal(t, before, ta.applies(), "%s: a brief that fails the lint wrote nothing", c.path)
		code, out, errs = ta.do(c.good)
		assert.Equal(t, 0, code, "%s: a passing brief is admitted: exit %d, out %q, errs %q", c.path, code, out, errs)
	}

	// no verb of these paths offers a flag that skips the lint
	for _, verb := range []string{"add", "brief", "recut"} {
		_, out, errs := ta.do("help " + verb)
		for _, flag := range []string{"--no-lint", "--skip-lint", "--lint=false", "--no-check", "--force"} {
			assert.NotContains(t, out+errs, flag, "%s offers %s", verb, flag)
		}
	}
}
