package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every verb's -h states its effect, one of the four classes the onboarding
// standard names (docs/STANDARD.md section 2: effect: inspection|local
// write|store write|delivery), and a verb with read and write forms says so.
func TestEveryVerbHasAnAccurateEffectLineAndAddHelpPointsAtTheTemplate(t *testing.T) {
	t.Parallel()

	classes := []string{"inspection: ", "local write: ", "store write: ", "delivery: "}
	for _, v := range verbs {
		e, ok := verbEffect[v.name]
		require.True(t, ok, "%s has no effect line", v.name)
		known := false
		for _, c := range classes {
			if strings.HasPrefix(e, c) {
				known = true
				break
			}
		}
		assert.True(t, known, "%s effect %q does not start with a class word", v.name, e)
	}

	assert.Contains(t, verbEffect["needs"], "set", "needs effect names its write form")
	assert.Contains(t, verbEffect["seat"], "repair", "seat effect names its write form")
	assert.Contains(t, verbEffect["seat"], "push", "seat effect names its write form")

	a := newApp(func(string) string { return "" })
	var out, errb bytes.Buffer

	code := runInto(a, []string{"help", "add"}, &out, &errb)
	require.Equal(t, 0, code, "help add: %s", errb.String())
	assert.Contains(t, out.String(), "nova-swarm template --name card", "help add points at the template")
	// the brief guidance replacing the whole template is short
	_, tail, found := strings.Cut(out.String(), "effect: ")
	require.True(t, found, "help add lacks an effect line")
	tailLines := strings.Split(strings.TrimSpace(tail), "\n")
	assert.Less(t, len(tailLines), 40, "help add's guidance must be shorter than 40 lines")

	out.Reset()
	errb.Reset()
	code = runInto(a, []string{"help", "brief"}, &out, &errb)
	require.Equal(t, 0, code, "help brief: %s", errb.String())
	assert.Contains(t, out.String(), "RESULT:", "help brief prints the brief template")
	assert.Contains(t, out.String(), "STEP 6.", "help brief prints the whole brief template")
}
