package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBannerLeadsWithTheExampleVerb pins that the banner's verb list and its
// first-run line lead with example, so a cold caller meets the offline workflow
// before import can spend GitHub budget, and that both import -h and the banner
// warn that even --dry-run reads GitHub unless --repo narrows it.
func TestBannerLeadsWithTheExampleVerb(t *testing.T) {
	t.Parallel()
	banner := workMain(unreachable(t)).Run("help")
	require.Equal(t, 0, banner.Code, banner.Stderr)

	require.Contains(t, banner.Stdout, "usage:\n", "the banner has no usage block:\n%s", banner.Stdout)
	usage := strings.SplitN(banner.Stdout, "usage:\n", 2)[1]
	first := strings.Split(usage, "\n")[0]
	require.Contains(t, first, "nova-work example --dir", "the banner's verb list does not lead with example:\n%s", banner.Stdout)

	fr := strings.Index(banner.Stdout, "first run:")
	require.Greater(t, fr, -1, "the banner has no first-run line:\n%s", banner.Stdout)
	require.True(t, strings.HasPrefix(banner.Stdout[fr:], "first run: nova-work example"), "the first-run line does not lead with example:\n%s", banner.Stdout)

	const warn = "reads GitHub (every issue of the repo) even with --dry-run; give --repo explicitly"
	require.Contains(t, banner.Stdout, warn, "the banner does not warn that --dry-run reads GitHub:\n%s", banner.Stdout)

	imp := workMain(unreachable(t)).Run("import", "-h")
	require.Equal(t, 0, imp.Code, imp.Stderr)
	require.Contains(t, imp.Stdout, warn, "import -h does not warn that --dry-run reads GitHub:\n%s", imp.Stdout)
}
