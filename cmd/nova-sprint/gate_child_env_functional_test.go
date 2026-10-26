//go:build functional

package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gctraceLander(t *testing.T) *lander {
	t.Helper()
	a := &app{gitEnv: append(slices.Clone(os.Environ()),
		"GODEBUG=gctrace=1,inittrace=1",
		"GOCACHE="+t.TempDir(),
	)}
	return &lander{a: a}
}

func TestGateUnderGctraceParentJudgesCleanTreeGreen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clean.go"), []byte("package p\n\nfunc F() {}\n"), 0o644))
	l := gctraceLander(t)
	out, err := l.goRun(context.Background(), dir, []string{"gofmt", "-l", "."})
	require.NoError(t, err)
	assert.NoError(t, gateRunError([]string{"gofmt", "-l", "."}, out, err))
	assert.Empty(t, out)
}

func TestGateUnderGctraceParentCatchesGofmtDirtyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.go"), []byte("package p\n\nfunc F() {\n  x := 1\n  _ = x\n}\n"), 0o644))
	l := gctraceLander(t)
	out, err := l.goRun(context.Background(), dir, []string{"gofmt", "-l", "."})
	require.NoError(t, err)
	assert.Error(t, gateRunError([]string{"gofmt", "-l", "."}, out, err))
	assert.Contains(t, out, "bad.go")
}
