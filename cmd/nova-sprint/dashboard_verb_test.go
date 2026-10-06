package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The sprint dashboard server is a nova-sprint verb and nothing else: the help lists it, the
// verb is dispatched by nova-sprint and refuses a stray word in its own name, and the module
// carries no separate dashboard program and no service file for one.
func TestDashboardIsANovaSprintVerb(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)

	_, out, errs := ta.raw("help")
	assert.Regexp(t, `(?m)^\s*dashboard\b`, out+errs, "the help lists the dashboard verb")

	code, _, errs := ta.raw("dashboard stray")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "dashboard", "the verb is nova-sprint's own")
	assert.Contains(t, errs, "takes no words")

	root := filepath.Join("..", "..")
	cmds, err := os.ReadDir(filepath.Join(root, "cmd"))
	require.NoError(t, err)
	for _, e := range cmds {
		assert.NotContains(t, strings.ToLower(e.Name()), "dash", "cmd/%s: no separate dashboard binary", e.Name())
	}
	require.NoError(t, filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".plist") {
			assert.NotContains(t, strings.ToLower(filepath.Base(path)), "dash", "%s: no dashboard plist", path)
		}
		return nil
	}))
}
