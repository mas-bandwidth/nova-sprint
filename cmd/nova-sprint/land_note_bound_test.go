package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The lander's check findings are joined within the note bound the merge verb accepts;
// a list that does not fit keeps whole findings and says how many it left out and where
// they are, never cut mid-line and never cut silently.
func TestBoundedChecksKeepsWholeFindingsAndSaysWhatItLeftOut(t *testing.T) {
	t.Parallel()
	short := []string{"tla/RUNS.tsv:3 has no current run record (E4)", "a sentence fragment at docs/x.md:4 (E4)"}
	require.Equal(t, strings.Join(short, "; "), boundedChecks(short, 1000), "a list that fits is joined as before")

	var long []string
	for i := 0; i < 400; i++ {
		long = append(long, fmt.Sprintf("tla/RUNS.tsv:%d has no current run record for the case MCCase%03d.cfg (group g) (E4)", i, i))
	}
	bound := 4096
	note := boundedChecks(long, bound)
	require.LessOrEqual(t, len(note), bound, "the note fits the bound")
	require.Contains(t, note, "more findings (")
	require.Contains(t, note, fmt.Sprintf("over the %d-byte note bound", bound))
	require.Contains(t, note, "the whole list is in the lander's output")
	// every finding kept is whole: the note's findings are a prefix of the list
	head := strings.SplitN(note, "; and ", 2)[0]
	for _, f := range strings.Split(head, "; ") {
		require.Contains(t, long, f, "a kept finding is one of the list, whole")
	}
	// the count it says it left out is the truth
	kept := len(strings.Split(head, "; "))
	require.Contains(t, note, fmt.Sprintf("and %d more findings", len(long)-kept))
}
