package ci

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheUnitRuleScanCountsAProgramExec pins the program kind of the unit rule
// (Glenn, 2026-10-16: "no programs, no live services, no real time"): a unit-tier
// test file that runs a program with os/exec.Command or exec.CommandContext, or
// that calls a named rig helper whose body runs one (landRig.git), is a program
// use. A functional-tagged file is the functional tier and counts nothing.
func TestTheUnitRuleScanCountsAProgramExec(t *testing.T) {
	t.Parallel()
	scan := func(t *testing.T, src string) []string {
		t.Helper()
		got, err := scanUnitRule(map[string][]byte{"p/x_test.go": []byte(src)})
		require.NoError(t, err)
		var out []string
		for _, f := range got {
			out = append(out, fmt.Sprintf("%d %s %s", f.Line, f.Kind, f.What))
		}
		return out
	}
	yes := map[string]struct {
		src  string
		want []string
	}{
		"an exec.Command": {"package p\nimport \"os/exec\"\nfunc TestX() { exec.Command(\"git\", \"status\") }\n",
			[]string{"3 program os/exec.Command git"}},
		"an exec.CommandContext": {"package p\nimport \"os/exec\"\nfunc TestX() { exec.CommandContext(ctx, \"git\", \"status\") }\n",
			[]string{"3 program os/exec.CommandContext git"}},
		"a one-level rig helper call": {
			"package p\nfunc TestX(r R) { r.git(\"status\") }\nfunc (r R) git(args ...string) string { return \"\" }\n",
			[]string{"2 program git"}},
	}
	for name, c := range yes {
		t.Run("yes/"+name, func(t *testing.T) {
			assert.Equal(t, c.want, scan(t, c.src))
		})
	}
	no := map[string]string{
		"a functional file": "//go:build functional\n\npackage p\nimport \"os/exec\"\nfunc TestX() { exec.Command(\"git\", \"status\") }\n",
	}
	for name, src := range no {
		t.Run("no/"+name, func(t *testing.T) {
			assert.Empty(t, scan(t, src))
		})
	}
}
