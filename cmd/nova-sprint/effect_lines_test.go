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
		e, ok := effectLine(v.name)
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

	assert.Contains(t, effectLine0("needs"), "set", "needs effect names its write form")
	assert.Contains(t, effectLine0("seat"), "repair", "seat effect names its write form")
	assert.Contains(t, effectLine0("seat"), "push", "seat effect names its write form")
	// a store-mutating verb whose line is set in its own file still reads as the
	// standard's store write class, through effectLine
	assert.True(t, strings.HasPrefix(effectLine0("routes rest"), "store write: "), "routes rest is a store write")
	assert.True(t, strings.HasPrefix(effectLine0("adopt"), "local write: "), "adopt reads as a class word too")

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

// effectLine0 is effectLine's line alone, for the assertions that read one verb.
func effectLine0(name string) string {
	e, _ := effectLine(name)
	return e
}
