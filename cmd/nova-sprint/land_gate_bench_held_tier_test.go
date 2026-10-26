package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The land gate's held-member bench tests run a real git through the land rig (switch,
// push, fetch) and spin on runtime.Gosched, so they belong to the functional tier: the
// file is behind //go:build functional, and no unit-tier test of this package declares
// them.
func TestTheHeldGateLandTestsAreFunctional(t *testing.T) {
	t.Parallel()
	const functional = "land_gate_bench_held_functional_test.go"
	body, err := os.ReadFile(functional)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(body), "//go:build functional\n"), "%s starts with //go:build functional", functional)

	files, err := filepath.Glob("*_test.go")
	require.NoError(t, err)
	names := []string{"TestAHeldMemberRunsLandGates", "TestADownMemberRunsNoLandGate", "TestLandDryRunNamesTheGateBench"}
	for _, file := range files {
		src, err := os.ReadFile(file)
		require.NoError(t, err)
		if strings.HasPrefix(string(src), "//go:build functional") {
			continue
		}
		for _, name := range names {
			require.NotContains(t, string(src), "func "+name+"(", "%s is unit tier and declares %s", file, name)
		}
	}
}
