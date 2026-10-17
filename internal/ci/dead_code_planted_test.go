//go:build functional

package ci

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeadCodeFindsPlanted runs the measure TestDeadCode uses over a tiny module
// in a temporary directory: a function nothing reaches is reported dead, and a
// function reached only from a functional-tagged test of the main is live.
func TestDeadCodeFindsPlanted(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Second)
	defer cancel()

	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	bin := deadcodeBinary(t, ctx, repo)

	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/planted\n\ngo 1.26\n",
		"cmd/x/main.go": `package main

import "example.com/planted/lib"

func main() { lib.Live() }
`,
		"cmd/x/x_functional_test.go": `//go:build functional

package main

import (
	"testing"

	"example.com/planted/lib"
)

func TestFunctionalOnly(t *testing.T) { lib.FunctionalOnly() }
`,
		"lib/lib.go": `package lib

var sink int

// Live is called from the main.
func Live() { sink++ }

// Planted is called from nowhere.
func Planted() { sink-- }

// FunctionalOnly is called only from a functional-tagged test.
func FunctionalOnly() { sink += 2 }
`,
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}

	measured := measureDeadCode(t, ctx, bin, dir, "example.com/planted/")
	assert.Equal(t, map[string][]string{"lib": {"Planted"}}, measured,
		"the planted function is dead; Live (reached from main) and FunctionalOnly (reached from a functional test) are live")
}
