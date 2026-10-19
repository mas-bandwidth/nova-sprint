package cardgen

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ParseSharedRegistries is the pure parser for a repository's shared registry
// (.nova/always-allowed): one repository-relative path or glob per line, '#' comments
// and blank lines ignored, and an entry that climbs out of the repository with .. (or an
// absolute path) is dropped (cardgen.SharedRegistries; docs/SPEC-LANDING.md,
// docs/SPEC-SPRINT.md section 7).
func TestSharedRegistriesParse(t *testing.T) {
	t.Parallel()
	data := []byte("# shared registries\n\n" +
		"make/negative-controls.json\n" +
		"pkg/**/*.txt\n" +
		"docs/specs/*.md\n" +
		"\n" +
		"../escape.go\n" +
		"/abs/path.go\n" +
		"a/../../up.go\n" +
		"# a trailing comment\n")
	assert.Equal(t, []string{
		"make/negative-controls.json",
		"pkg/**/*.txt",
		"docs/specs/*.md",
	}, ParseSharedRegistries(data))
	assert.Nil(t, SharedRegistries(t.TempDir()), "absent file reads nil")
}
