package sprint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seat uninstall unloads and removes the retired push loop's unit where an older build
// installed it (docs/SPEC-SPRINT.md, "Handing over the seat"; v1.3.6). The unit is looked
// for only in the directory the installer is given, and the loader is the test's: nothing
// is unloaded on the machine running the test.
func TestSeatUninstallRemovesTheRetiredPushLoop(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ goos, file string }{{"darwin", SeatLabel + ".plist"}, {"linux", SeatService}} {
		t.Run(tc.goos, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			var calls []string
			in := SeatInstaller{Dir: dir,
				Load:   func(p string) error { calls = append(calls, "load "+p); return nil },
				Unload: func(p string) error { calls = append(calls, "unload "+p); return nil }}
			path := filepath.Join(dir, tc.file)
			assert.Equal(t, tc.file, SeatUnitFile(tc.goos))
			require.NoError(t, os.WriteFile(path, []byte("an older build's unit"), 0o644))

			r, err := in.Uninstall(tc.goos)
			require.NoError(t, err)
			assert.Equal(t, SeatResult{Path: path, Changed: true}, r)
			assert.NoFileExists(t, path)
			assert.Equal(t, []string{"unload " + path}, calls, "uninstall unloads the unit before it removes it")

			r, err = in.Uninstall(tc.goos)
			require.NoError(t, err)
			assert.Equal(t, SeatResult{Path: path}, r, "a second uninstall finds nothing and does nothing")
			assert.Equal(t, []string{"unload " + path}, calls)
		})
	}
	_, err := (SeatInstaller{Dir: t.TempDir()}).Uninstall("windows")
	assert.Error(t, err)
	assert.Empty(t, SeatUnitFile("windows"))
}
