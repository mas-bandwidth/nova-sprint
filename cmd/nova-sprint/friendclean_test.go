package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Every refusal names what it wants and removes nothing.
func TestFriendCleanRefusals(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.a.friends = friendRows()
	dir := t.TempDir()
	for _, tc := range []struct {
		line string
		code int
		says string
	}{
		{"friend clean --root " + dir + " --days 0", 2, "--days is at least 1"},
		{"friend clean --root " + filepath.Join(dir, "nope"), 2, "--root wants the directory"},
		{"friend clean --root " + dir + " ada", 2, "takes no words"},
		{"friend clean --root " + dir, exitCannotRead, "holds no friend row"},
		{"friend clean --root " + dir + " --pg postgres://x@h/db --file f.json", 2, "exclusive"},
		{"friend clean --root " + dir + " --file " + filepath.Join(dir, "absent.json"), exitCannotRead, "the config cannot be read"},
	} {
		code, _, errs := ta.do(tc.line)
		assert.Equal(t, tc.code, code, "%s: %s", tc.line, errs)
		assert.Contains(t, errs, tc.says, tc.line)
	}
}
