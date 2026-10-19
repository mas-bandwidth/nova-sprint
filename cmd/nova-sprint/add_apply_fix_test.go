package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// TestAddApplyFixRewritesOnlyTheHeaderPathLines pins add --apply-fix: a brief read from a
// file whose only base finding is a PATHS miss is fixed in place (the corrected PATHS line,
// nothing at or after the header changed, the trailing newline kept), the fixed brief is
// admitted, and stderr prints the old and new line; without --apply-fix the same brief is
// refused exactly as today; a brief with a non-fixable finding as well as the PATHS miss is
// refused whole with nothing written; and a body line after the header that begins PATHS: is
// never rewritten.
func TestAddApplyFixRewritesOnlyTheHeaderPathLines(t *testing.T) {
	t.Parallel()

	rig := func() (*testApp, func(string, string, string) string, func(string, string, string) string) {
		ta := newTestApp(t)
		ta.ok("init --readers reader-a,reader-b --members m1")
		remote, _ := twinRemote(t, ta, map[string]string{
			"internal/x/x.go":            "package x\n",
			"internal/x/x_test.go":       "package x\n\nimport \"testing\"\n\nfunc TestOld(t *testing.T) {}\n",
			"internal/x/testdata/in.txt": "in\n",
		}, "sprint/s1")
		brief := func(base, paths, test string) string {
			return passingBrief("RESULT: c sha=0123456789ab tier: pro\nREPO: " + remote + "\nBASE: " + base + "\nPATHS: " + paths + "\nTEST: ./internal/x " + test + "\n\nTHE TASK. Fix internal/x/x.go.")
		}
		write := func(dir, id, text string) string {
			t.Helper()
			p := filepath.Join(dir, id+".md")
			require.NoError(t, os.WriteFile(p, []byte(text), 0o600))
			return p
		}
		return ta, brief, write
	}

	// (1) a PATHS miss alone, added with --apply-fix: admitted, the file rewritten to the
	// corrected PATHS line, byte-identical after the header, the trailing newline kept.
	{
		ta, brief, write := rig()
		dir := t.TempDir()
		f := write(dir, "fix", brief("sprint/s1", "internal/x/*.go", "TestNew"))
		orig, err := os.ReadFile(f)
		require.NoError(t, err)
		code, out, errs := ta.do("add --stream s1 fix --one --brief-file " + f + " --apply-fix")
		assert.Equal(t, 0, code, "%s%s", out, errs)
		assert.Contains(t, out, "MOVED fix -> ready stream=s1")
		assert.Contains(t, errs, "APPLY-FIX card=fix file="+f+" -PATHS: internal/x/*.go\n")
		assert.Contains(t, errs, "APPLY-FIX card=fix file="+f+" +PATHS: internal/x/*.go,internal/x/testdata/**\n")
		fixed, err := os.ReadFile(f)
		require.NoError(t, err)
		assert.Contains(t, string(fixed), "PATHS: internal/x/*.go,internal/x/testdata/**")
		assert.Equal(t, strings.HasSuffix(string(orig), "\n"), strings.HasSuffix(string(fixed), "\n"), "the trailing newline is kept")
		origLines := strings.Split(string(orig), "\n")
		fixedLines := strings.Split(string(fixed), "\n")
		end := sprint.HeaderEnd(string(orig))
		require.Equal(t, origLines[end:], fixedLines[end:], "nothing after the header changes")
	}

	// (2) the same brief without --apply-fix is refused exactly as today.
	{
		ta, brief, write := rig()
		dir := t.TempDir()
		f := write(dir, "refuse", brief("sprint/s1", "internal/x/*.go", "TestNew"))
		code, out, errs := ta.do("add --stream s1 refuse --one --brief-file " + f)
		assert.Equal(t, 2, code, "%s%s", out, errs)
		assert.Contains(t, errs, "LINT FIX card=refuse PATHS: internal/x/*.go,internal/x/testdata/**\n")
		after, err := os.ReadFile(f)
		require.NoError(t, err)
		assert.Equal(t, brief("sprint/s1", "internal/x/*.go", "TestNew"), string(after), "no write")
	}

	// (3) REVERSED WITNESS: a non-fixable finding (donewhen-test-name) with a PATHS miss,
	// added with --apply-fix, is refused, the store unchanged and the file byte-identical.
	{
		ta, brief, write := rig()
		dir := t.TempDir()
		w := write(dir, "wit", brief("sprint/s1", "internal/x/*.go", "TestOld"))
		wOrig, err := os.ReadFile(w)
		require.NoError(t, err)
		beforeW := ta.applies()
		code, out, errs := ta.do("add --stream s1 wit --one --brief-file " + w + " --apply-fix")
		assert.Equal(t, 2, code, "%s%s", out, errs)
		assert.Contains(t, errs, "check=donewhen-test-name")
		assert.False(t, ta.placed("wit"), "nothing written")
		assert.Equal(t, beforeW, ta.applies(), "no store write")
		wNow, err := os.ReadFile(w)
		require.NoError(t, err)
		assert.Equal(t, wOrig, wNow, "the file is byte-identical")
	}

	// (4) a body line after the header that begins PATHS: is never rewritten.
	{
		bodyBrief := passingBrief("RESULT: c sha=0123456789ab tier: pro\nREPO: r\nBASE: b\nPATHS: x\nNEW: y\n\nTHE TASK. Read the module.") + "\nA body paragraph starts here.\nPATHS: not-a-header-line\n"
		fixed, applied := applyHeaderFix(bodyBrief, swarm.BriefFix{"PATHS: x,z"})
		assert.Contains(t, fixed, "\nPATHS: not-a-header-line\n", "the body line is never rewritten")
		assert.Contains(t, fixed, "\nPATHS: x,z\n", "the header PATHS line is replaced")
		assert.Len(t, applied, 1)
	}
}
