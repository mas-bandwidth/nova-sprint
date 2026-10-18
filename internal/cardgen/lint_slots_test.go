package cardgen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// placeholders are the Lint findings named placeholder.
func placeholders(id, brief string) []LintFinding {
	var out []LintFinding
	for _, f := range Lint(id, brief) {
		if f.Check == "placeholder" {
			out = append(out, f)
		}
	}
	return out
}

// 2026-10-18: nova-card lint read every <word> as a leftover template slot, so a card
// that was filled in and admitted by nova-sprint add came back from nova-card lint with a
// finding on each of the template's own slots: the RESULT line's sha=<sha12>, the report
// wording `Head: <sha>` and `--head <sha>`, the commit format `<id>: <what>`, and anything
// inside a backtick span (a command, a type, a format being described). Those are the
// template's words, not leftovers. A real leftover in prose (`Fix <what> in the parser.`)
// is still a finding.
func TestALintSkipsTheTemplatesSlotsAndKeepsARealLeftover(t *testing.T) {
	t.Parallel()
	base := Render(header, Card{ID: "a", File: "x/y.go", Paths: []string{"x/y.go"}, Test: "x TestA", Tier: "pro", Kind: "fix-red", Task: "Do it."})
	require.Empty(t, Lint("a", base), "the rendered brief is the clean base")

	slots := strings.Replace(base, "sha=0123456789ab", "sha=<sha12>", 1)
	slots = strings.Replace(slots, "Do it.", strings.Join([]string{
		"Commit as `a: <what>` and finish with `--head <sha>`.",
		"Finish LAND with --head <sha>; the report's line 2 is Head: <sha>.",
		"The commit format is <id>: <what>.",
		"The type `Plan[<elem>]` and the path `internal/<package>/<file>.go` are spellings, not slots.",
	}, "\n"), 1)
	assert.Empty(t, placeholders("a", slots), "the template's own slots are no leftover")

	const leftoverLine = "Fix <what> in the parser."
	leftover := strings.Replace(slots, "The type `Plan", leftoverLine+"\nThe type `Plan", 1)
	require.NotEqual(t, slots, leftover)
	line := 0
	for i, l := range strings.Split(leftover, "\n") {
		if l == leftoverLine {
			line = i + 1
		}
	}
	got := placeholders("a", leftover)
	require.Len(t, got, 1, "a real leftover in prose is one finding: %v", got)
	assert.Equal(t, line, got[0].Line)
	assert.Equal(t, "<what>", got[0].Excerpt)
}
