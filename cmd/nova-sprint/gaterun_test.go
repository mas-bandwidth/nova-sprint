package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGateBenchHostsReadsTheEnvList pins the bench list the gate is configured with:
// GateBenchEnv is trimmed, blanks dropped and duplicates collapsed, in order (the first
// the host, the second the fallback the bench-run verb tries).
func TestGateBenchHostsReadsTheEnvList(t *testing.T) {
	t.Parallel()
	assert.Empty(t, gateBenchHosts(""))
	assert.Empty(t, gateBenchHosts(" , ,"))
	assert.Equal(t, []string{"bench-a", "bench-b"}, gateBenchHosts(" bench-a , bench-b "))
	assert.Equal(t, []string{"bench-a"}, gateBenchHosts("bench-a,bench-a,,"))
}
