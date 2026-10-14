package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every verb's -h carries its own exit table (verbExits), and that table names
// only the codes the verb's code can return: 0 (done) and 2 (usage) are the
// codes every verb returns on the paths below, and 3 (or more) is a per-verb
// code, stated in the verb's own table and never in the one shared banner line
// (docs/STANDARD.md section 2, "Every verb's -h quotes the table, or the verb's
// own"). The shared line once named code 3 with three meanings at once (fleet
// sync, friend sync, run); it must name only the common codes.
func TestEveryVerbsExitTableNamesOnlyCodesItReturns(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	// The banner's one shared line names only the common codes. Code 3 (and any
	// code above 2) means different things per verb, so it must not ride on the
	// shared line the whole tool prints.
	for _, code := range []int{3, 4} {
		assert.NotContains(t, exitLine, " "+strconv.Itoa(code)+" ",
			"the shared banner line names code %d for the whole tool, but it is a per-verb code", code)
	}

	for _, v := range verbs {
		v := v
		t.Run(v.name, func(t *testing.T) {
			// the verb's -h prints its own table and exits 0 (the done path)
			code, out, errs := ta.do(v.name + " -h")
			require.Equal(t, 0, code, "%s -h: %d %q %q", v.name, code, out, errs)
			line := verbExits(v.name)
			require.Contains(t, out, line, "%s -h drops its own exit table %q", v.name, line)

			// 0 and 2 are the codes every verb's own paths return: its table names both
			assert.Contains(t, line, "0 ", "%s: table %q names no 0", v.name, line)
			assert.Contains(t, line, "2 ", "%s: table %q names no 2", v.name, line)

			// a verb names 3 (or more) only when it states a table of its own
			for _, code := range []int{3, 4} {
				digit := " " + strconv.Itoa(code) + " "
				if strings.Contains(line, digit) {
					_, own := verbExit[v.name]
					assert.True(t, own, "%s names code %d but states no table of its own", v.name, code)
				}
			}
		})
	}
}
