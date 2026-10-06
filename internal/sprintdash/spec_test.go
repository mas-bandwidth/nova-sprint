package sprintdash

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// specPath is the page's specification.
const specPath = "../../docs/SPEC-SPRINT-DASHBOARD.md"

// The specification carries the live page's own lines (the live SPEC.md) and names the pin
// that holds the page to the live one.
func TestTheSpecCarriesTheLivePagesLines(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(specPath)
	require.NoError(t, err)
	spec := string(b)
	for _, line := range []string{
		"## The page is the live page",
		"TestThePageIsTheLivePageByteForByte",
		"- Landings chart (the owner 2026-10-04 4:20 PM ET:",
		"### Freshness: once per second, end to end (hard requirement)",
		"- The machine pill says RUNNING, STALE or STOPPED and nothing more;",
	} {
		assert.Contains(t, spec, line)
	}
	assert.NotContains(t, spec, "Glenn")
	assert.False(t, strings.Contains(spec, "TestDashboardPageIsTheSpec (internal/sprintdash) holds"), "the drift check is retired: the pin holds the page")
}

// The page loads nothing from anywhere else: its face is the embedded one.
func TestDashboardPageLoadsNothingFromElsewhere(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"index.html", "app.js"} {
		assert.NotRegexp(t, `https?://(?:[^"\s]*)\.(?:css|js|woff2?|png|webp|svg)|fonts\.googleapis|fonts\.gstatic`, string(file(name)), "%s loads an asset from elsewhere", name)
	}
	assert.Contains(t, string(file("index.html")), `src: url("/nunito-800.woff2")`)
	assert.Equal(t, []byte("wOF2"), file("nunito-800.woff2")[:4], "the face is a woff2")
	assert.Contains(t, string(file("OFL.txt")), "SIL Open Font License", "the face's licence is embedded beside it")
}
