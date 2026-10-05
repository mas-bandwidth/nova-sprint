package sprintdash

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The page is the live page (the owner, 2026-10-05: "the one I'm watching is the one I
// want"): the files of the dashboard the owner watches, copied into page/ unchanged but
// for the owner's name in comments, which reads "the owner" here. What renders is the
// file with its comments stripped (rendered), and its sha256 is the live file's after the
// same stripping; the face and its licence are the live bytes whole. Each repository
// file's own sha256 is pinned too, so a change to any byte of the page, a comment's
// included, is a named change to this file.

// livePage is each page file: the sha256 of the live file (its SHA256SUMS line), of what
// it renders (the live file and the repository's, comments stripped), and of the
// repository's file as it is.
var livePage = map[string]struct{ live, rendered, repo string }{
	"index.html": {
		live:     "ea5b523657384900f853343d087c893da0a4ef874b38b5682880481189f49ee9",
		rendered: "179d0c73be74d76c5ae7a18ecc73d5bea532eebb59d00e7a1204de67464dabf1",
		repo:     "43982828c8341ccd92d5a17961ae4e8e5803d617a3993ad4bb3180499c884a29",
	},
	"app.js": {
		live:     "e3e75f918614879f5639131b1a6111ae27ba654a683cb9239b460853bbd90ac7",
		rendered: "94c70da2242b6b5e754bd59eb02bcf70be56b9e134cc042a1ad5777b9da9acf8",
		repo:     "89be61bdf99f0206f405ac6c9c7118c7b873ac6b70d45407fa211446f293b1b7",
	},
	"OFL.txt": {
		live:     "580df76c95a1ec5ab878ceb25bb3d85c6a076804e9c970c8c6972aea775fdf65",
		rendered: "580df76c95a1ec5ab878ceb25bb3d85c6a076804e9c970c8c6972aea775fdf65",
		repo:     "580df76c95a1ec5ab878ceb25bb3d85c6a076804e9c970c8c6972aea775fdf65",
	},
	"nunito-800.woff2": {
		live:     "b42be94a8cf3d5fc7877216cdb8bbfb10d57291b06356b6f3b9e7fbf9742b8da",
		rendered: "b42be94a8cf3d5fc7877216cdb8bbfb10d57291b06356b6f3b9e7fbf9742b8da",
		repo:     "b42be94a8cf3d5fc7877216cdb8bbfb10d57291b06356b6f3b9e7fbf9742b8da",
	},
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// TestThePageIsTheLivePageByteForByte: every file the page is made of renders exactly what
// the live file renders, and is the bytes pinned here. With NOVA_LIVE_PAGE naming a
// directory of the live files, those are checked too: each is the file its SHA256SUMS
// lists, it renders what the repository's does, and the repository's file is the live
// file byte for byte once the one word the owner's name is in its comments reads "the
// owner" (the name is found in the live file, never written here).
func TestThePageIsTheLivePageByteForByte(t *testing.T) {
	t.Parallel()
	live := os.Getenv("NOVA_LIVE_PAGE")
	var sums map[string]string
	if live != "" {
		sums = liveSums(t, filepath.Join(live, "SHA256SUMS"))
	}
	for name, want := range livePage {
		b := file(name)
		assert.Equal(t, want.rendered, sum(rendered(name, b)), "%s: what it renders is not the live page's", name)
		assert.Equal(t, want.repo, sum(b), "%s: its bytes changed; a change to the page is named here", name)
		if live == "" {
			continue
		}
		lb, err := os.ReadFile(filepath.Join(live, name))
		listed, ok := sums[name]
		if !ok && errors.Is(err, os.ErrNotExist) {
			continue // a file the live directory does not carry (the face): its pin above holds it
		}
		require.NoError(t, err)
		if ok {
			assert.Equal(t, listed, sum(lb), "%s: the live file is not the one its SHA256SUMS lists", name)
		}
		assert.Equal(t, want.live, sum(lb), "%s: the live file is not the one pinned here", name)
		assert.Equal(t, want.rendered, sum(rendered(name, lb)), "%s: the live file renders something else", name)
		named := lb
		if word := ownerWord(lb, b); word != "" {
			named = bytes.ReplaceAll(lb, []byte(word), []byte(theOwner))
		}
		assert.Equal(t, want.repo, sum(named), "%s: the repository's file is not the live file with the owner's name read as %q", name, theOwner)
	}
	for _, name := range []string{"app.js", "index.html", "OFL.txt"} {
		if live != "" {
			assert.Contains(t, sums, name, "the live SHA256SUMS lists %s", name)
		}
	}
}

// theOwner is what the owner's name reads as in the repository's comments.
const theOwner = "the owner"

// ownerWord is the word of live that reads theOwner in repo: the word at the first byte
// the two differ, "" when they do not.
func ownerWord(live, repo []byte) string {
	d := 0
	for d < len(live) && d < len(repo) && live[d] == repo[d] {
		d++
	}
	if d == len(live) && d == len(repo) {
		return ""
	}
	letter := func(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }
	for d > 0 && letter(live[d-1]) {
		d--
	}
	e := d
	for e < len(live) && letter(live[e]) {
		e++
	}
	return string(live[d:e])
}

// liveSums is a SHA256SUMS file: each line's sum by its file name.
func liveSums(t *testing.T, path string) map[string]string {
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Fields(line)
		require.Len(t, f, 2, "a SHA256SUMS line is a sum and a name: %q", line)
		out[strings.TrimPrefix(f[1], "*")] = f[0]
	}
	return out
}

// rendered is what a page file renders: a script with its comments stripped, the markup
// with its comments, its style sheets' comments and its inline scripts' comments
// stripped; any other file whole.
func rendered(name string, b []byte) []byte {
	switch filepath.Ext(name) {
	case ".js":
		return stripJS(b)
	case ".html":
		return stripHTML(b)
	}
	return b
}

// stripHTML is the markup without its <!-- --> comments, the /* */ comments of its <style>
// blocks and the comments of its inline <script> blocks.
func stripHTML(src []byte) []byte {
	var out []byte
	s := string(src)
	for {
		i := strings.IndexAny(s, "<")
		if i < 0 {
			return append(out, s...)
		}
		out = append(out, s[:i]...)
		s = s[i:]
		switch {
		case strings.HasPrefix(s, "<!--"):
			end := strings.Index(s, "-->")
			if end < 0 {
				return out
			}
			s = s[end+3:]
		case strings.HasPrefix(s, "<style"), strings.HasPrefix(s, "<script"):
			closer, strip := "</style>", stripCSS
			if strings.HasPrefix(s, "<script") {
				closer, strip = "</script>", stripJS
			}
			open := strings.IndexByte(s, '>') + 1
			end := strings.Index(s[open:], closer)
			if open == 0 || end < 0 {
				return append(out, s...)
			}
			out = append(out, s[:open]...)
			out = append(out, strip([]byte(s[open:open+end]))...)
			out = append(out, closer...)
			s = s[open+end+len(closer):]
		default:
			out = append(out, '<')
			s = s[1:]
		}
	}
}

// stripCSS is a style sheet without its /* */ comments; quoted strings are kept whole.
func stripCSS(src []byte) []byte {
	var out []byte
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '"' || c == '\'':
			j := endQuoted(src, i)
			out = append(out, src[i:j]...)
			i = j
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i = endBlock(src, i)
		default:
			out = append(out, c)
			i++
		}
	}
	return out
}

// stripJS is a script without its // and /* */ comments; strings, template literals and
// regular expression literals are kept whole.
func stripJS(src []byte) []byte {
	var out []byte
	for i := 0; i < len(src); {
		c := src[i]
		var next byte
		if i+1 < len(src) {
			next = src[i+1]
		}
		switch {
		case c == '"' || c == '\'' || c == '`':
			j := endQuoted(src, i)
			out = append(out, src[i:j]...)
			i = j
		case c == '/' && next == '/':
			j := bytes.IndexByte(src[i:], '\n')
			if j < 0 {
				return out
			}
			i += j
		case c == '/' && next == '*':
			i = endBlock(src, i)
		case c == '/' && regexStarts(out):
			j := endRegex(src, i)
			out = append(out, src[i:j]...)
			i = j
		default:
			out = append(out, c)
			i++
		}
	}
	return out
}

// endQuoted is the index after the string that opens at i, escapes honoured.
func endQuoted(src []byte, i int) int {
	q := src[i]
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case q:
			return j + 1
		}
	}
	return len(src)
}

// endBlock is the index after the /* */ comment that opens at i.
func endBlock(src []byte, i int) int {
	if j := bytes.Index(src[i+2:], []byte("*/")); j >= 0 {
		return i + 2 + j + 2
	}
	return len(src)
}

// regexStarts is whether a / after out begins a regular expression literal, not a
// division: after an operator, an opening bracket, a separator or a keyword that takes
// an expression, or at the start.
func regexStarts(out []byte) bool {
	t := bytes.TrimRight(out, " \t\r\n")
	if len(t) == 0 {
		return true
	}
	last := t[len(t)-1]
	if strings.IndexByte("(,=:[!&|?{};+-*%<>~^", last) >= 0 {
		return true
	}
	j := len(t)
	for j > 0 && (t[j-1] == '_' || t[j-1] == '$' || t[j-1] >= 'a' && t[j-1] <= 'z' || t[j-1] >= 'A' && t[j-1] <= 'Z' || t[j-1] >= '0' && t[j-1] <= '9') {
		j--
	}
	switch string(t[j:]) {
	case "return", "typeof", "case", "in", "of", "new", "delete", "void", "throw", "else", "do":
		return true
	}
	return false
}

// endRegex is the index after the regular expression literal that opens at i, its
// classes and escapes honoured, its flags included.
func endRegex(src []byte, i int) int {
	class := false
	j := i + 1
	for ; j < len(src) && src[j] != '\n'; j++ {
		switch c := src[j]; {
		case c == '\\':
			j++
		case c == '[':
			class = true
		case c == ']':
			class = false
		case c == '/' && !class:
			j++
			for j < len(src) && src[j] >= 'a' && src[j] <= 'z' {
				j++
			}
			return j
		}
	}
	return j
}

// The stripping keeps what renders: strings, template literals and regular expressions
// that hold comment marks are code, a division is no regular expression, and only the
// comments go.
func TestRenderedStripsCommentsOnly(t *testing.T) {
	t.Parallel()
	js := "var a = \"// not\", b = '/* not */', c = `// nor`; // gone\n" +
		"var r = /[/]\\/x/g.test(s); /* gone */ var d = a / b / c;\n" +
		"if (x) return /y\\//.exec(z); // gone too\n"
	assert.Equal(t, "var a = \"// not\", b = '/* not */', c = `// nor`; \n"+
		"var r = /[/]\\/x/g.test(s);  var d = a / b / c;\n"+
		"if (x) return /y\\//.exec(z); \n", string(stripJS([]byte(js))))

	html := "<!doctype html>\n<!-- the owner -->\n<style>a { content: \"/* kept */\"; } /* gone */</style>\n" +
		"<script>var u = \"http://x\"; // gone\n</script>\n<p>a < b <!--SLOT--></p>\n"
	assert.Equal(t, "<!doctype html>\n\n<style>a { content: \"/* kept */\"; } </style>\n"+
		"<script>var u = \"http://x\"; \n</script>\n<p>a < b </p>\n", string(stripHTML([]byte(html))))
	assert.Equal(t, []byte("x"), rendered("OFL.txt", []byte("x")))

	// the owner's word is found where the two files first differ, from the word's start
	assert.Equal(t, "Ann", ownerWord([]byte("// Ann 2026"), []byte("// the owner 2026")))
	assert.Equal(t, "Tess", ownerWord([]byte("x // Tess said"), []byte("x // the owner said")))
	assert.Equal(t, "", ownerWord([]byte("same"), []byte("same")))
}
