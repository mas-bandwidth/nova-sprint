package hygiene

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindDataIsEmbedded(t *testing.T) {
	t.Parallel()
	assert.NotEmpty(t, strings.TrimSpace(kindData),
		"kindData is empty: the //go:embed kinds.txt directive is missing above it")
}

func TestKindsDeclareEveryKindInKindsTxt(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("kinds.txt")
	require.NoError(t, err)
	declared := map[string]bool{}
	for _, name := range Kinds() {
		declared[name] = true
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		name := strings.TrimSpace(fields[0])
		if name == "" {
			continue
		}
		if !declared[name] {
			assert.True(t, declared[name], "kind %q is in kinds.txt but is not declared; declared: %v", name, Kinds())
		}
	}
}
