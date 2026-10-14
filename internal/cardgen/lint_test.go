package cardgen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// planBrief is a minimal card brief with the typed header lines a plan review
// reads.
func planBrief(paths, shared, needs string) string {
	b := "# t\nTEST: x\nREPO: r\nBASE: main\n"
	if shared != "" {
		b += "SHARED: " + shared + "\n"
	}
	if paths != "" {
		b += "PATHS: " + paths + "\n"
	}
	if needs != "" {
		b += "Needs: " + needs + "\n"
	}
	return b
}

// A brief whose text names a sibling path (../<name>, a sibling repository by name)
// is refused at admission unless it carries a SIBLINGS header naming the repositories
// (owner/name@ref, comma separated), with the path quoted (docs/SPEC-CARD-CONTRACT.md,
// the SIBLINGS header; docs/SPEC-STAGING.md, the siblings): the harness is pointed at
// the sibling by the staged JOB.md, so a brief that reaches outside its tree must say
// where the sibling comes from.
func TestALintRefusesASiblingPathWithoutASIBLINGSHeader(t *testing.T) {
	t.Parallel()
	base := Render(header, Card{ID: "a", File: "x/y.go", Paths: []string{"x/y.go"}, Test: "x TestA", Tier: "pro", Kind: "fix-red", Task: "Do it."})
	require.Empty(t, Lint("a", base), "the rendered brief is the clean base")

	naming := strings.Replace(base, "Do it.", "The harness expects the C++ reference corpus at ../serialize next to the checkout.", 1)
	checks := Lint("a", naming)
	var sibling LintFinding
	for _, f := range checks {
		if f.Check == "sibling-path" {
			sibling = f
		}
	}
	require.NotEmpty(t, sibling.Check, "a sibling path with no SIBLINGS header is refused")
	assert.Contains(t, sibling.Excerpt, "../serialize", "the path is quoted in the refusal")

	withHeader := strings.Replace(naming, "\nTHE TASK.", "\nSIBLINGS: mas-bandwidth/serialize@abc\nTHE TASK.", 1)
	assert.Empty(t, Lint("a", withHeader), "with the SIBLINGS header the sibling is admitted")
}

// A brief that names a sibling path but no sibling repository matches it in its
// SIBLINGS header is still refused: the header's repositories must cover the paths
// the text names.
func TestALintRefusesASiblingPathNotListedInTheHeader(t *testing.T) {
	t.Parallel()
	base := Render(header, Card{ID: "a", File: "x/y.go", Paths: []string{"x/y.go"}, Test: "x TestA", Tier: "pro", Kind: "fix-red", Task: "Do it."})
	naming := strings.Replace(base, "Do it.", "Compare against the reference corpus at ../serialize.c next to the checkout.", 1)
	naming = strings.Replace(naming, "\nTHE TASK.", "\nSIBLINGS: mas-bandwidth/serialize@abc\nTHE TASK.", 1)
	checks := Lint("a", naming)
	found := false
	for _, f := range checks {
		if f.Check == "sibling-path" && strings.Contains(f.Excerpt, "../serialize.c") {
			found = true
		}
	}
	assert.True(t, found, "a sibling path absent from the header is refused and quoted")
}
