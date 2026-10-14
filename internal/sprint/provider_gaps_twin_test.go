package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestProviderGapsOnATwinSayTheTwinHasNone pins that an empty provider set on a
// mem twin is expected, not a warning: a twin has no config store for
// nova-config to apply provider rows from (docs/SPEC-SPRINT.md, "The twin
// (`mem:<file>`) holds no routes"). A live store keeps the warning.
func TestProviderGapsOnATwinSayTheTwinHasNone(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		"providers: the twin holds no provider rows (expected; a live store gets them from nova-config apply)",
		ProvidersLine(nil, ProviderGapReport{}, true))
	assert.Equal(t,
		"providers: none applied WARN no provider rows applied (nova-config apply --kind provider)",
		ProvidersLine(nil, ProviderGapReport{}, false))
}
