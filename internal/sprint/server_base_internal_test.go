package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/buildinfo"
)

// TestCommitOfReadsTheCommitOffTheVersionLine: the build commit a version line names, read
// from the line alone, in the order SPEC-SPRINT section 14 names it: the `commit=` extra, the
// revision of a whole source, the 12 hex of the vcs stamp; a dirty build and a line that names
// none say so in why (docs/SPEC-SPRINT.md section 14, "server-from-base-only-w-ns-bb.w1").
func TestCommitOfReadsTheCommitOffTheVersionLine(t *testing.T) {
	t.Parallel()
	full := strings.Repeat("0123456789", 4)
	cases := []struct{ line, commit, why string }{
		{"nova-sprint v1.0.0 linux/amd64 go1.26 commit=" + full, full, ""},
		{"nova-sprint 20261005000000-0123456789ab linux/amd64 go1.26", "0123456789ab", ""},
		{"nova-sprint v1.0.0 linux/amd64 go1.26 repo=x revision=" + full + " dirty=false build_host=h", full, ""},
		{"nova-sprint v1.0.0 linux/amd64 go1.26 repo=x revision=" + full + " dirty=true build_host=h", "", "dirty=true"},
		{"nova-sprint v1.0.0 linux/amd64 go1.26 commit=" + full + "-dirty", "", "edited tree"},
		{"nova-sprint v1.0.0 linux/amd64 go1.26 commit=main", "", "is not a commit"},
		{"nova-sprint 20261005000000-0123456789ab-dirty linux/amd64 go1.26", "", "edited tree"},
		{"nova-sprint devel linux/amd64 go1.26", "", "names no source commit"},
	}
	for _, tc := range cases {
		f, ok := buildinfo.Parse(tc.line)
		require.True(t, ok, tc.line)
		commit, why := commitOf(f)
		assert.Equal(t, tc.commit, commit, tc.line)
		if tc.why == "" {
			assert.Empty(t, why, tc.line)
		} else {
			assert.Contains(t, why, tc.why, tc.line)
		}
	}
}
