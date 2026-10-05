package work

import (
	"strings"
	"testing"
)

// The note is the design the work verbs point at: the program in the tree,
// what stays only in Redis, and when a file is written. The refusal the
// verbs print is the same sentence.
func TestNoteStatesTheTreeTheRedisOnlyPartAndWhenItIsWritten(t *testing.T) {
	t.Parallel()
	if !strings.Contains(Note, Unbuilt) {
		t.Fatal("NOTE.md does not state the refusal")
	}
	for _, s := range []string{
		`(sprint-program "v1"`,
		`(work-tree "v1")`,
		"(:roadmap 1",
		"What stays only in Redis",
		"When it is written",
		"6667c0de0a718dc35607e8119c613dda39bfda81",
	} {
		if !strings.Contains(Note, s) {
			t.Errorf("NOTE.md lacks %q", s)
		}
	}
}
