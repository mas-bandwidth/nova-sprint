package ci

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 2026-10-17 (Glenn, restated): no real sockets in unit tests, ever. The
// unit_rule_allowed.txt allowance is by file, so a listed file could still open
// a socket; a socket has no allowance at all: it is a functional test
// (//go:build functional) or a test through a seam (net.Pipe,
// httptest.NewRecorder, a RoundTripper).
func TestNoUnitTestOpensASocketEvenWhenAllowed(t *testing.T) {
	files := moduleTestFiles(t, filepath.Join("..", ".."))
	require.NotEmpty(t, files, "the scan read no test files")
	findings, err := scanUnitRule(files)
	require.NoError(t, err)
	var sockets []string
	for _, f := range findings {
		if f.Kind == "socket" {
			sockets = append(sockets, f.String())
		}
	}
	assert.Empty(t, sockets, "%d real sockets in the unit tier:\n%s", len(sockets), strings.Join(sockets, "\n"))
}
