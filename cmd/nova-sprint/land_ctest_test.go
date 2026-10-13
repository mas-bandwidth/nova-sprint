package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A C++ (CMake) clone has a tree gate too: with no go.mod and a CMakeLists.txt, treeGate
// runs cmake configure, cmake build and the batch's ctest tests through the bench seam, and
// a red ctest is the finding its output carries (gateWhy). A clone with neither go.mod nor
// CMakeLists.txt keeps today's no gate, said on the batch instead of passing silently.
func TestACtestCardLandsOnItsOwnTest(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte("cmake_minimum_required(VERSION 3.16)\nproject(demo CXX)\nadd_test(NAME DecodeAll COMMAND demo)\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "main.cpp"), []byte("int main() { return 0; }\n"), 0o644))

	var runs [][]string
	b := r.a.landState()
	b.mu.Lock()
	b.gateBench = func(_ context.Context, _ string, _ string, commands [][]string, _ bool) (string, int, error) {
		runs = append(runs, commands...)
		return "", 0, nil
	}
	b.mu.Unlock()

	l := &lander{a: r.a, ctestTests: []string{"DecodeAll"}}
	require.Empty(t, l.treeGate(t.Context(), dir, true, nil), "a green CMake gate")
	require.Len(t, runs, 3, "the configure, the build and the ctest run")
	assert.Equal(t, []string{"cmake", "-S", ".", "-B", "build"}, runs[0])
	assert.Equal(t, []string{"cmake", "--build", "build", "-j"}, runs[1])
	assert.Equal(t, []string{"ctest", "--test-dir", "build", "-R", "^(DecodeAll)$", "--output-on-failure"}, runs[2], "the card's name is the -R pattern")

	// A red ctest is the card's finding, its output carried (gateWhy).
	runs = nil
	b.mu.Lock()
	b.gateBench = func(_ context.Context, _ string, _ string, commands [][]string, _ bool) (string, int, error) {
		runs = append(runs, commands...)
		return gateMark + "ctest --test-dir build -R ^(DecodeAll)$ --output-on-failure\n" +
			"1/1 Test #1: DecodeAll .......................***Failed    0.01 sec\n", 1, nil
	}
	b.mu.Unlock()
	red := l.treeGate(t.Context(), dir, true, nil)
	require.NotEmpty(t, red, "a red ctest refuses the tree")
	assert.Contains(t, red, "ctest")
	assert.Contains(t, red, "DecodeAll")
	assert.Contains(t, red, "***Failed")
	require.Len(t, runs, 3)

	// Neither go.mod nor CMakeLists.txt: no gate, said on the batch.
	bare := t.TempDir()
	b.mu.Lock()
	b.gateBench = func(context.Context, string, string, [][]string, bool) (string, int, error) {
		t.Fatal("the no-gate clone must run no gate")
		return "", 0, nil
	}
	b.mu.Unlock()
	l2 := &lander{a: r.a}
	require.Empty(t, l2.treeGate(t.Context(), bare, true, nil))
	assert.Contains(t, l2.ledgerLog, "no gate: no go.mod and no CMakeLists.txt")
}
