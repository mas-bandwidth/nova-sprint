package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The roadmap data merges entry by entry: two cards that each add an entry after the last
// (each moving the closing parentheses) keep both, the tip's first; an addition beside a
// removal keeps the one and drops the other. Both sides changing one entry, an entry
// both add, and an addition inside an entry the other side removes are refused.
func TestUnionSexp(t *testing.T) {
	t.Parallel()
	a, b, c, x := fixItem("a"), fixItem("b"), fixItem("c"), fixItem("x")
	for _, tc := range []struct {
		name               string
		base, ours, theirs string
		want               string // "" when refused
		why                string
	}{
		{"both add after the last", fixesFile(a), fixesFile(a, b), fixesFile(a, c), fixesFile(a, b, c), ""},
		{"one adds, the other removes", fixesFile(a, x), fixesFile(a, x, b), fixesFile(x), fixesFile(x, b), ""},
		{"both remove different entries", fixesFile(a, b, c), fixesFile(a, c), fixesFile(a, b), fixesFile(a), ""},
		{"both change one entry", fixesFile(a), fixesFile(strings.Replace(a, `:title "a"`, `:title "one"`, 1)),
			fixesFile(strings.Replace(a, `:title "a"`, `:title "two"`, 1)), "", "both sides change line"},
		{"both add one id", fixesFile(a), fixesFile(a, b), fixesFile(a, b), "", "the merged data does not decode"},
		{"an addition inside a removed entry", fixesFile(x, fixItemLines("a"), c), fixesFile(x, c),
			fixesFile(x, strings.Replace(fixItemLines("a"), "\n    :origin", "\n    :text \"more\"\n    :origin", 1), c), "", "the card adds lines inside a stretch the tip removes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := unionSexp("fixes", "docs/fixes.sexp", []byte(tc.base), []byte(tc.ours), []byte(tc.theirs))
			if tc.want == "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.why)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

// The real docs/fixes.sexp, two cards each adding their entry at its end as the cards of
// 2026-10-10 did: both are kept, and the file still decodes.
func TestUnionSexpOnTheRealFixesFile(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "fixes.sexp"))
	require.NoError(t, err)
	base := string(raw)
	require.True(t, strings.HasSuffix(base, ")))\n"), "the file ends its last entry, the list and the form on one line")
	release := "v1.2.6"
	add := func(id string) string {
		return strings.TrimSuffix(base, "))\n") + "\n\n   (fix \"" + id + "\" :release \"" + release + "\" :status \"shipped\"\n    :title \"" + id + "\"\n    :origin \"a card\")))\n"
	}
	got, err := unionSexp("fixes", "docs/fixes.sexp", raw, []byte(add("card-one")), []byte(add("card-two")))
	require.NoError(t, err)
	e, err := sexpEntries("fixes", "docs/fixes.sexp", got)
	require.NoError(t, err)
	assert.Contains(t, e, "fix:card-one")
	assert.Contains(t, e, "fix:card-two")
	assert.Less(t, strings.Index(string(got), `"card-one"`), strings.Index(string(got), `"card-two"`), "the tip's entry first")
}
