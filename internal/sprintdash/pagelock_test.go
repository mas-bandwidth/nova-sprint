package sprintdash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lockedFiles is the files the page lock covers, as they are: every file of page/ and
// PageLockFiles.
func lockedFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	entries, err := os.ReadDir("page")
	require.NoError(t, err)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join("page", e.Name()))
		require.NoError(t, err)
		files["page/"+e.Name()] = b
	}
	for _, n := range PageLockFiles {
		b, err := os.ReadFile(n)
		require.NoError(t, err)
		files[n] = b
	}
	return files
}

// The page is locked (pagelock.go): its files digest to pagelock.txt's last line, and every
// line of the lock quotes an ask of the owner's that the spec's page lock section carries.
func TestThePageIsLocked(t *testing.T) {
	t.Parallel()
	lock, err := os.ReadFile("pagelock.txt")
	require.NoError(t, err)
	spec, err := os.ReadFile(filepath.Join("..", "..", "docs", "SPEC-SPRINT-DASHBOARD.md"))
	require.NoError(t, err)
	if err := CheckPageLock(string(lock), string(spec), PageDigest(lockedFiles(t))); err != nil {
		t.Fatal(err)
	}
}

// The lock on known cases, both ways: a page unchanged holds; a byte added, a byte removed, a
// file taken away or a file added is red until a new lock line carries the owner's words and
// the spec carries them on a dated line; a reused ask, a lock line with no spec line and a
// malformed line are red.
func TestThePageLockOnKnownCases(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{"page/index.html": []byte("<h1>nova-sprint</h1>"), "page/app.js": []byte("render()"), "pageview.go": []byte("package x")}
	base := PageDigest(files)
	spec := "# spec\n\n" + PageLockSection + "\n\n- 2026-10-11 the owner: \"this is what it is.\"\n\n## Next\n- 2026-10-12 the owner: \"outside the section\"\n"
	lock := "# the lock\n2026-10-11 " + base + " \"this is what it is.\"\n"

	require.NoError(t, CheckPageLock(lock, spec, base), "known yes: the page unchanged")

	changed := func(edit func(map[string][]byte)) string {
		c := map[string][]byte{}
		for k, v := range files {
			c[k] = append([]byte(nil), v...)
		}
		edit(c)
		return PageDigest(c)
	}
	added := changed(func(c map[string][]byte) { c["page/app.js"] = append(c["page/app.js"], []byte(";seatChip()")...) })
	removed := changed(func(c map[string][]byte) { c["page/index.html"] = []byte("<h1></h1>") })
	gone := changed(func(c map[string][]byte) { delete(c, "page/app.js") })
	extra := changed(func(c map[string][]byte) { c["page/release.js"] = []byte("picker()") })
	for name, d := range map[string]string{"a thing added": added, "a thing removed": removed, "a file taken away": gone, "a file added": extra} {
		err := CheckPageLock(lock, spec, d)
		require.Error(t, err, "known no: %s with no ask", name)
		assert.Contains(t, err.Error(), "the dashboard page changed")
		assert.Contains(t, err.Error(), "NEVER REMOVE THINGS FROM THE DASHBOARD UNLESS I ASK")
	}

	// the same change with the owner's ask, on the lock and in the spec: green
	asked := spec + "" // the ask goes in the page lock section
	asked = strings.Replace(asked, "\n\n## Next", "\n- 2026-10-12 the owner: \"show the reads in orange\"\n\n## Next", 1)
	lock2 := lock + "2026-10-12 " + added + " \"show the reads in orange\"\n"
	require.NoError(t, CheckPageLock(lock2, asked, added), "known yes: a change with the owner's ask")

	// a lock line whose words the spec's section does not carry (a line outside it does not count)
	err := CheckPageLock(lock+"2026-10-12 "+added+" \"outside the section\"\n", spec, added)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no line of 2026-10-12")
	// the same ask twice
	err = CheckPageLock(lock2+"2026-10-12 "+removed+" \"show the reads in orange\"\n", asked, removed)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "twice")
	// a malformed line, and an empty lock
	require.Error(t, CheckPageLock(lock+"2026-10-12 nodigest \"x\"\n", spec, base))
	require.Error(t, CheckPageLock("# nothing\n", spec, base))
}
