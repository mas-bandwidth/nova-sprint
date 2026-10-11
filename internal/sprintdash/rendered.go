package sprintdash

import (
	"bytes"
	"path"
	"strings"
)

// RenderedForm is a locked file as a browser runs it, every comment taken out: a .js file's
// line and block comments; an .html file's comments (but the logo and favicon slots the
// server fills), the CSS comments of its <style> blocks and the JS comments of its inline
// <script> blocks. A comment's place keeps one blank in JS and CSS, where it stops two tokens
// joining; an HTML comment is taken out and nothing written, as a browser renders it. Strings,
// template literals and regular-expression literals are read as such, so a "//" in a URL or
// a quote in a regex is code, never a comment's start; an attribute value is part of its tag,
// so a "<!--" in one is no comment either. Any other file is as it is: a change to it is never
// comment-only. The page lock holds a comment-only change to this form (pagelock.go and
// docs/SPEC-SPRINT-DASHBOARD.md, The page lock).
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
// <script> blocks. A comment found in text content is taken out and nothing written, as a
// browser renders it; a "<!--" inside a tag's quoted attribute value is part of the tag, never
// a comment. An element's open tag is read as such, so an inline <script> is told from an
// external one by the attribute named src exactly, not by any "src=" in it. It is a byte
// walker, not a parser: what it cannot read it leaves as code. Spec: The page lock.
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
				return out.Bytes() // an open comment runs to the end
			}
			s = s[4+end+3:]
		case strings.HasPrefix(s, "<style"):
			open, body, rest := block(s, "</style>")
			out.WriteString(open)
			out.Write(stripCSS([]byte(body)))
			s = rest
		case strings.HasPrefix(s, "<script"):
			open, body, rest := block(s, "</script>")
			out.WriteString(open)
			if !tagHasAttr(open, "src") {
				out.Write(stripJS([]byte(body)))
			}
			s = rest
		case len(s) > 1 && s[0] == '<' && isTagStart(s[1]):
			j := tagEnd(s, 0)
			out.WriteString(s[:j])
			s = s[j:]
		default:
			out.WriteByte(s[0])
			s = s[1:]
		}
	}
	return out.Bytes()
}

// isTagStart is whether c, the byte after a '<' in text content, can begin an element's tag.
func isTagStart(c byte) bool {
	return c == '/' || c == '!' || c == '?' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// tagEnd is the index just past the '>' that closes the tag s starts at i, a quoted attribute
// value run over as such, so a '>' inside one is no tag's end.
func tagEnd(s string, i int) int {
	var q byte
	for j := i + 1; j < len(s); j++ {
		c := s[j]
		if q != 0 {
			if c == q {
				q = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			q = c
		case '>':
			return j + 1
		}
	}
	return len(s)
}

// tagHasAttr is whether the element's open tag carries the attribute named name (case
// insensitive), matching the attribute's own name exactly: data-src is not src.
func tagHasAttr(tag, name string) bool {
	i := strings.IndexByte(tag, '<')
	if i < 0 {
		return false
	}
	i++
	for i < len(tag) && !isSpace(tag[i]) && tag[i] != '>' && tag[i] != '/' {
		i++
	}
	for i < len(tag) {
		for i < len(tag) && isSpace(tag[i]) {
			i++
		}
		if i >= len(tag) || tag[i] == '>' {
			break
		}
		if tag[i] == '/' {
			i++
			continue
		}
		start := i
		for i < len(tag) && !isSpace(tag[i]) && tag[i] != '=' && tag[i] != '>' && tag[i] != '/' {
			i++
		}
		attr := strings.ToLower(tag[start:i])
		for i < len(tag) && isSpace(tag[i]) {
			i++
		}
		if i < len(tag) && tag[i] == '=' {
			i++
			for i < len(tag) && isSpace(tag[i]) {
				i++
			}
			if i < len(tag) && (tag[i] == '"' || tag[i] == '\'') {
				q := tag[i]
				i++
				for i < len(tag) && tag[i] != q {
					i++
				}
				if i < len(tag) {
					i++
				}
			} else {
				for i < len(tag) && !isSpace(tag[i]) && tag[i] != '>' {
					i++
				}
			}
		}
		if attr == strings.ToLower(name) {
			return true
		}
	}
	return false
}

// isSpace is whether c is HTML's whitespace.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
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

// condBeforeParen is the words whose '(' opens the condition of a statement, so the ')' that
// closes it is followed by an expression: a slash after it opens a regular expression.
var condBeforeParen = map[string]bool{"if": true, "while": true, "for": true}

// stripJS takes JavaScript's line and block comments out, its strings, template literals and
// regular-expression literals kept. A slash opens a regular expression where an expression
// may start: at the start, after an operator or punctuation other than ) and ], or after a
// keyword such as return; else it is a division. A ')' that closes the condition of if, while
// or for counts as an expression's start, and after any other ')' a slash whose line carries
// a comment's mark is left as code, never a comment's start: the walker fails closed, reading
// what it cannot tell as code. Spec: The page lock.
func stripJS(b []byte) []byte {
	var out bytes.Buffer
	prev := byte(0) // the last significant byte written: 0 at the start
	word := ""      // the identifier or keyword that byte ends, if it ends one
	adj := false    // the byte just before was part of an identifier
	var parens []bool
	condClose := false // prev is a ')' that closed an if/while/for condition
	for i := 0; i < len(b); {
		c := b[i]
		wasAdj := adj
		adj = false
		switch {
		case c == '"' || c == '\'' || c == '`':
			j := stringEnd(b, i)
			out.Write(b[i:j])
			i, prev, word, condClose = j, c, "", false
		case c == '/' && prev == ')' && (condClose || commentMarkOnLine(b, i)):
			j := regexEnd(b, i)
			out.Write(b[i:j])
			i, prev, word = j, '/', ""
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
				prev, adj, condClose = c, true, false
			case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			default:
				switch c {
				case '(':
					parens = append(parens, condBeforeParen[word])
				case ')':
					cond := false
					if n := len(parens); n > 0 {
						cond = parens[n-1]
						parens = parens[:n-1]
					}
					condClose = cond
				default:
					condClose = false
				}
				prev, word = c, ""
			}
		}
	}
	return out.Bytes()
}

// commentMarkOnLine is whether the rest of the line at b[i] carries a comment's mark, "//" or
// "/*": after a ')' the walker fails closed and leaves such a line as code.
func commentMarkOnLine(b []byte, i int) bool {
	line := b[i:]
	if end := bytes.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	return bytes.Contains(line, []byte("//")) || bytes.Contains(line, []byte("/*"))
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
