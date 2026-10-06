package sprintdash

import (
	"bytes"
	"path/filepath"
)

// rendered is a page file with its comments taken out: what the browser runs and shows.
// app.js loses its // and /* */ comments; index.html its <!-- --> comments, the /* */
// comments of its <style> blocks and the comments of its <script> blocks; any other file
// is kept whole. Strings, template literals and regular expression literals are kept as
// they are, so a "//" inside one is no comment.
func rendered(name string, b []byte) []byte {
	switch filepath.Ext(name) {
	case ".js":
		return stripJS(b)
	case ".html":
		return stripHTML(b)
	}
	return b
}

// stripHTML drops the markup's comments and the comments inside its style and script blocks.
func stripHTML(b []byte) []byte {
	var out []byte
	for i := 0; i < len(b); {
		switch {
		case bytes.HasPrefix(b[i:], []byte("<!--")):
			end := bytes.Index(b[i+4:], []byte("-->"))
			if end < 0 {
				return out
			}
			i += 4 + end + 3
		case hasTag(b[i:], "<style"), hasTag(b[i:], "<script"):
			tag := "style"
			if hasTag(b[i:], "<script") {
				tag = "script"
			}
			open := bytes.IndexByte(b[i:], '>')
			if open < 0 {
				return append(out, b[i:]...)
			}
			out = append(out, b[i:i+open+1]...)
			i += open + 1
			end := bytes.Index(bytes.ToLower(b[i:]), []byte("</"+tag))
			if end < 0 {
				end = len(b) - i
			}
			if tag == "style" {
				out = append(out, stripCSS(b[i:i+end])...)
			} else {
				out = append(out, stripJS(b[i:i+end])...)
			}
			i += end
		default:
			out = append(out, b[i])
			i++
		}
	}
	return out
}

// hasTag is whether b opens with the tag (case aside) followed by '>' or a space.
func hasTag(b []byte, tag string) bool {
	if len(b) <= len(tag) || !bytes.EqualFold(b[:len(tag)], []byte(tag)) {
		return false
	}
	c := b[len(tag)]
	return c == '>' || c == ' ' || c == '\t' || c == '\n'
}

// stripCSS drops /* */ comments outside strings.
func stripCSS(b []byte) []byte {
	var out []byte
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == '"' || c == '\'':
			j := quoted(b, i)
			out = append(out, b[i:j]...)
			i = j
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			i = blockEnd(b, i)
		default:
			out = append(out, c)
			i++
		}
	}
	return out
}

// stripJS drops // and /* */ comments outside strings, template literals and regular
// expression literals; a line comment's newline is kept.
func stripJS(b []byte) []byte {
	var out []byte
	var prev byte // the last byte kept that is not space: whether a '/' opens a regex
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == '"' || c == '\'' || c == '`':
			j := quoted(b, i)
			out = append(out, b[i:j]...)
			i, prev = j, c
			continue
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			for i < len(b) && b[i] != '\n' {
				i++
			}
			continue
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			i = blockEnd(b, i)
			continue
		case c == '/' && opensRegex(prev):
			j := regexEnd(b, i)
			out = append(out, b[i:j]...)
			i, prev = j, '/'
			continue
		}
		out = append(out, c)
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			prev = c
		}
		i++
	}
	return out
}

// opensRegex is whether a '/' after prev begins a regular expression literal rather than a division.
func opensRegex(prev byte) bool {
	return prev == 0 || bytes.IndexByte([]byte("(,=:[!&|?{};+-*%<>~^"), prev) >= 0
}

// quoted is the index just past the string or template literal opening at i.
func quoted(b []byte, i int) int {
	q := b[i]
	for j := i + 1; j < len(b); j++ {
		switch b[j] {
		case '\\':
			j++
		case q:
			return j + 1
		}
	}
	return len(b)
}

// blockEnd is the index just past the /* */ comment opening at i.
func blockEnd(b []byte, i int) int {
	end := bytes.Index(b[i+2:], []byte("*/"))
	if end < 0 {
		return len(b)
	}
	return i + 2 + end + 2
}

// regexEnd is the index just past the regular expression literal opening at i, its flags with it.
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
		case c == '\n':
			return j
		case c == '/' && !class:
			j++
			for j < len(b) && (b[j] >= 'a' && b[j] <= 'z') {
				j++
			}
			return j
		}
	}
	return len(b)
}
