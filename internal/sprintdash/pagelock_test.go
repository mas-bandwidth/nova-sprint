package sprintdash

import (
	"os"
	"os/exec"
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

// The lock only grows: against the base a change is merged into (PAGELOCK_BASE, a git revision:
// CI names the pull request's base), every line of the base's pagelock.txt is still there, in
// order, at the head of this one, so a change cannot rewrite an old line's digest under an ask
// already given; a new ask is a new line. A base with no lock yet (its first change) passes.
// Unset, it is not checked (a bench's run with no base to compare).
func TestThePageLockOnlyGrows(t *testing.T) {
	t.Parallel()
	base := os.Getenv("PAGELOCK_BASE")
	if base == "" {
		t.Skip("PAGELOCK_BASE names no base revision; CI sets it to the pull request's base")
	}
	// the base must be a commit git can read: a misspelled rev, no git, a refused repository
	// or a broken fetch is red, never a pass
	if out, err := exec.Command("git", "rev-parse", "--verify", "--quiet", base+"^{commit}").CombinedOutput(); err != nil {
		t.Fatalf("PAGELOCK_BASE %s is no commit git can read here (%v: %s): the lock's growth is unchecked", base, err, strings.TrimSpace(string(out)))
	}
	const path = "internal/sprintdash/pagelock.txt"
	// a commit that holds no pagelock.txt is the one case of the lock's first
	if exec.Command("git", "cat-file", "-e", base+":"+path).Run() != nil {
		t.Logf("%s holds no pagelock.txt: this change is the lock's first", base)
		return
	}
	out, err := exec.Command("git", "show", base+":"+path).Output()
	require.NoError(t, err, "git show %s:%s", base, path)
	lock, err := os.ReadFile("pagelock.txt")
	require.NoError(t, err)
	if err := LockGrows(string(out), string(lock)); err != nil {
		t.Fatal(err)
	}
	// a comment-only change renders the same at the base and here
	if err := CommentOnlyHolds(string(out), string(lock), filesAt(t, base), lockedFiles(t)); err != nil {
		t.Fatal(err)
	}
}

// filesAt is the locked files at the git revision base, keyed as lockedFiles keys them.
func filesAt(t *testing.T, base string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	list, err := exec.Command("git", "ls-tree", "--full-tree", "--name-only", base, "internal/sprintdash/page/").Output()
	require.NoError(t, err, "git ls-tree %s", base)
	for _, p := range strings.Fields(string(list)) {
		b, err := exec.Command("git", "show", base+":"+p).Output()
		require.NoError(t, err, "git show %s:%s", base, p)
		files["page/"+strings.TrimPrefix(p, "internal/sprintdash/page/")] = b
	}
	require.NotEmpty(t, files, "%s holds no page files under internal/sprintdash/page/", base)
	for _, n := range PageLockFiles {
		b, err := exec.Command("git", "show", base+":internal/sprintdash/"+n).Output()
		require.NoError(t, err, "git show %s:%s", base, n)
		files[n] = b
	}
	return files
}

// The comment-only rule on known cases: comments changed under a comment-only line hold; a
// string, a regular expression, a CSS value or the page's text changed under one is red; a
// change with an ask of the owner's is held by the ask, not here.
func TestACommentOnlyLineHoldsOnlyACommentChange(t *testing.T) {
	t.Parallel()
	js := "var u = \"http://x\"; // the owner's words\nvar r = s.replace(/[&<>\"']/g, f); /* a note */\nvar d = a / b; // c\n"
	html := "<style>a{color:red} /* why */ b{content:\"/* kept */\"}</style><!-- note --><!--LOGO--><p>text</p><script>var x=1; // c\n</script>"
	base := map[string][]byte{"page/app.js": []byte(js), "page/index.html": []byte(html), "pageview.go": []byte("package x")}
	with := func(name, from, to string) map[string][]byte {
		c := map[string][]byte{}
		for k, v := range base {
			c[k] = v
		}
		require.Contains(t, string(c[name]), from)
		c[name] = []byte(strings.Replace(string(c[name]), from, to, 1))
		return c
	}
	a := "2026-10-11 " + strings.Repeat("a", 64) + " \"this is what it is.\"\n"
	co := a + "2026-10-11 " + strings.Repeat("b", 64) + " comment-only \"privacy\"\n"
	asked := a + "2026-10-12 " + strings.Repeat("b", 64) + " \"show the reads\"\n"
	for name, c := range map[string]map[string][]byte{
		"a js line comment":    with("page/app.js", "the owner's words", "other words"),
		"a js block comment":   with("page/app.js", "a note", "another note"),
		"a css comment":        with("page/index.html", "why", "because"),
		"an html comment":      with("page/index.html", "note", "other note"),
		"an inline js comment": with("page/index.html", "// c", "// d"),
	} {
		require.NoError(t, CommentOnlyHolds(a, co, base, c), "known yes: %s", name)
	}
	for name, c := range map[string]map[string][]byte{
		"a string with //":      with("page/app.js", "http://x", "http://y"),
		"a regex with quotes":   with("page/app.js", "/[&<>\"']/g", "/[&<>\"]/g"),
		"code after a division": with("page/app.js", "a / b", "a / c"),
		"a css value":           with("page/index.html", "color:red", "color:blue"),
		"a css string with /*":  with("page/index.html", "/* kept */", "/* gone */"),
		"the page's text":       with("page/index.html", "<p>text</p>", "<p>more</p>"),
		"a fill slot":           with("page/index.html", "<!--LOGO-->", "<!--LOGO2-->"),
		"inline js code":        with("page/index.html", "var x=1", "var x=2"),
		"pageview.go":           with("pageview.go", "package x", "package y"),
	} {
		err := CommentOnlyHolds(a, co, base, c)
		require.Error(t, err, "known no: %s under a comment-only line", name)
		assert.Contains(t, err.Error(), "renders differently")
		require.NoError(t, CommentOnlyHolds(a, asked, base, c), "%s with the owner's ask is the ask's to hold", name)
	}
	require.NoError(t, CommentOnlyHolds(a, a, base, with("page/app.js", "http://x", "http://y")), "no new line: the lock check's to hold")
}

// The growth rule on known cases: lines appended pass; an old line rewritten, dropped or
// reordered is red.
func TestThePageLockGrowthOnKnownCases(t *testing.T) {
	t.Parallel()
	a := "2026-10-11 " + strings.Repeat("a", 64) + " \"this is what it is.\"\n"
	b := "2026-10-12 " + strings.Repeat("b", 64) + " \"show the reads\"\n"
	require.NoError(t, LockGrows("# head\n"+a, "# head\n"+a+b), "known yes: a line appended")
	require.NoError(t, LockGrows(a, a), "known yes: unchanged")
	require.Error(t, LockGrows(a, strings.Replace(a, "aaaa", "cccc", 1)), "known no: the old line's digest rewritten under the same ask")
	require.Error(t, LockGrows(a+b, b), "known no: an old line dropped")
	require.Error(t, LockGrows(a+b, b+a), "known no: lines reordered")
}
