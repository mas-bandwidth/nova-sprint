package sprintdash

import (
	"bytes"
	"path"
	"strings"
)

// RenderedForm is a locked file as a browser runs it, every comment taken out: a .js file's
// line and block comments; an .html file's comments (but the logo and favicon slots the
// server fills), the CSS comments of its <style> blocks and the JS comments of its inline
// <script> blocks. A comment's place keeps one blank, so tokens never join. Strings,
// template literals and regular-expression literals are read as such, so a "//" in a URL or
// a quote in a regex is code, never a comment's start. Any other file is as it is: a change
// to it is never comment-only. The page lock holds a comment-only change to this form
// (pagelock.go).
func RenderedForm(name string, b []byte) []byte {
	switch strings.ToLower(path.Ext(name)) {
	case ".js":
		return stripJS(b)
	case ".html":
		return stripHTML(b)
	}
	return b
}

// RenderDigest is PageDigest over the files' rendered forms.
func RenderDigest(files map[string][]byte) string {
	r := make(map[string][]byte, len(files))
	for n, b := range files {
		r[n] = RenderedForm(n, b)
	}
	return PageDigest(r)
}

// htmlSlots are the HTML comments the server fills: code, never comments.
var htmlSlots = []string{"<!--LOGO-->", "<!--FAVICON-->"}

// stripHTML takes an HTML document's comments out, and the comments of its <style> and inline
// <script> blocks.
func stripHTML(b []byte) []byte {
	var out bytes.Buffer
	s := string(b)
	for len(s) > 0 {
		switch {
		case strings.HasPrefix(s, "<!--"):
			if slot := slotAt(s); slot != "" {
				out.WriteString(slot)
				s = s[len(slot):]
				continue
			}
			end := strings.Index(s[4:], "-->")
			if end < 0 {
				return append(out.Bytes(), ' ') // an open comment runs to the end
			}
			out.WriteByte(' ')
			s = s[4+end+3:]
		case strings.HasPrefix(s, "<style"):
			open, body, rest := block(s, "</style>")
			out.WriteString(open)
			out.Write(stripCSS([]byte(body)))
			s = rest
		case strings.HasPrefix(s, "<script") && !strings.Contains(s[:strings.IndexByte(s+">", '>')], "src="):
			open, body, rest := block(s, "</script>")
			out.WriteString(open)
			out.Write(stripJS([]byte(body)))
			s = rest
		default:
			out.WriteByte(s[0])
			s = s[1:]
		}
	}
	return out.Bytes()
}

// slotAt is the fill slot s starts with, "" for none.
func slotAt(s string) string {
	for _, slot := range htmlSlots {
		if strings.HasPrefix(s, slot) {
			return slot
		}
	}
	return ""
}

// block splits s, which starts with an element's open tag, into the open tag, the element's
// body up to the close tag, and the rest from the close tag on.
func block(s, close string) (open, body, rest string) {
	gt := strings.IndexByte(s, '>')
	if gt < 0 {
		return s, "", ""
	}
	open, s = s[:gt+1], s[gt+1:]
	end := strings.Index(s, close)
	if end < 0 {
		return open, s, ""
	}
	return open, s[:end], s[end:]
}

// stripCSS takes CSS's /* */ comments out, its strings kept.
func stripCSS(b []byte) []byte {
	var out bytes.Buffer
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == '"' || c == '\'':
			j := stringEnd(b, i)
			out.Write(b[i:j])
			i = j
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			end := bytes.Index(b[i+2:], []byte("*/"))
			out.WriteByte(' ')
			if end < 0 {
				return out.Bytes()
			}
			i += 2 + end + 2
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.Bytes()
}

// stringEnd is the index just past the string literal that opens at b[i] (its quote), a
// backslash escaping the next byte; the end of b when it never closes.
func stringEnd(b []byte, i int) int {
	q := b[i]
	for j := i + 1; j < len(b); j++ {
		switch b[j] {
		case '\\':
			j++
		case q:
			return j + 1
		case '\n':
			if q != '`' {
				return j // an unclosed string ends at its line
			}
		}
	}
	return len(b)
}

// regexAfterWord is the words after which a slash opens a regular expression, never a division.
var regexAfterWord = map[string]bool{"return": true, "typeof": true, "case": true, "do": true, "else": true,
	"in": true, "of": true, "new": true, "delete": true, "void": true, "throw": true, "instanceof": true, "yield": true, "await": true}

// stripJS takes JavaScript's line and block comments out, its strings, template literals and
// regular-expression literals kept. A slash opens a regular expression where an expression
// may start: at the start, after an operator or punctuation other than ) and ], or after a
// keyword such as return; else it is a division.
func stripJS(b []byte) []byte {
	var out bytes.Buffer
	prev := byte(0) // the last significant byte written: 0 at the start
	word := ""      // the identifier or keyword that byte ends, if it ends one
	adj := false    // the byte just before was part of an identifier
	for i := 0; i < len(b); {
		c := b[i]
		wasAdj := adj
		adj = false
		switch {
		case c == '"' || c == '\'' || c == '`':
			j := stringEnd(b, i)
			out.Write(b[i:j])
			i, prev, word = j, c, ""
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			end := bytes.IndexByte(b[i:], '\n')
			out.WriteByte(' ')
			if end < 0 {
				return out.Bytes()
			}
			i += end // the newline stays
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			end := bytes.Index(b[i+2:], []byte("*/"))
			out.WriteByte(' ')
			if end < 0 {
				return out.Bytes()
			}
			i += 2 + end + 2
		case c == '/' && regexMayStart(prev, word):
			j := regexEnd(b, i)
			out.Write(b[i:j])
			i, prev, word = j, '/', ""
		default:
			out.WriteByte(c)
			i++
			switch {
			case isIdent(c):
				if wasAdj {
					word += string(c)
				} else {
					word = string(c)
				}
				prev, adj = c, true
			case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			default:
				prev, word = c, ""
			}
		}
	}
	return out.Bytes()
}

func isIdent(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// regexMayStart is whether a slash after prev (and the word it ends) opens a regular expression.
func regexMayStart(prev byte, word string) bool {
	switch {
	case prev == 0:
		return true
	case isIdent(prev):
		return regexAfterWord[word]
	case prev == ')' || prev == ']' || prev == '"' || prev == '\'' || prev == '`' || prev == '/':
		return false
	}
	return true
}

// regexEnd is the index just past the regular-expression literal that opens at b[i], its flags
// included: a backslash escapes the next byte, and a slash inside a [ ] class is no end.
func regexEnd(b []byte, i int) int {
	class := false
	for j := i + 1; j < len(b); j++ {
		switch c := b[j]; {
		case c == '\\':
			j++
		case c == '[':
			class = true
		case c == ']':
			class = false
		case c == '/' && !class:
			j++
			for j < len(b) && isIdent(b[j]) {
				j++
			}
			return j
		case c == '\n':
			return j
		}
	}
	return len(b)
}
