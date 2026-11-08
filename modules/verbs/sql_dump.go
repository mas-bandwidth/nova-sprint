package verbs

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// ParseDump parses one statement at a time and accepts only matched pg_dump wrappers.
func ParseDump(r io.Reader, emit func(string) error) error {
	state := dumpReader{split: sqlSplitter{emit: emit}}
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			state.line++
			if problem := state.consume(line); problem != nil {
				return fmt.Errorf("line %d (byte %d): %w; the line starts %q", state.line, state.offset, problem, edge.FirstBytes([]byte(line)))
			}
			state.offset += len(line)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return edge.Cause(fmt.Sprintf("line %d (byte %d): read", state.line+1, state.offset), err)
		}
	}
	if state.token != "" && !state.closed {
		return fmt.Errorf("line %d (byte %d, the end): unclosed restrict wrapper", state.line, state.offset)
	}
	if state.split.mode != sqlNormal && state.split.mode != sqlLineComment {
		return fmt.Errorf("line %d (byte %d, the end): unterminated SQL construct; it starts %q", state.line, state.offset, edge.FirstBytes([]byte(state.split.buf.String())))
	}
	if hasSQL(state.split.buf.String()) {
		return fmt.Errorf("line %d (byte %d, the end): unterminated SQL statement; it starts %q", state.line, state.offset, edge.FirstBytes([]byte(state.split.buf.String())))
	}
	return nil
}

type dumpReader struct {
	split  sqlSplitter
	token  string
	closed bool
	line   int
	offset int // bytes before the current line
}

func (d *dumpReader) consume(line string) error {
	if d.split.mode == sqlNormal && strings.HasPrefix(strings.TrimSpace(line), "\\") {
		return d.directive(line)
	}
	if d.closed && strings.TrimSpace(line) != "" {
		return fmt.Errorf("content follows unrestrict")
	}
	return d.split.feed(line)
}
func (d *dumpReader) directive(line string) error {
	cmd := strings.Fields(line)
	if len(cmd) != 2 || !validRestrictToken(cmd[1]) {
		return fmt.Errorf("unsupported psql directive (want \\restrict or \\unrestrict and one alphanumeric token)")
	}
	switch cmd[0] {
	case `\restrict`:
		if d.token != "" || d.split.statements > 0 || d.closed {
			return fmt.Errorf("invalid restrict wrapper")
		}
		d.token = cmd[1]
	case `\unrestrict`:
		if d.token == "" || d.token != cmd[1] || d.closed || hasSQL(d.split.buf.String()) {
			return fmt.Errorf("invalid unrestrict wrapper")
		}
		d.closed = true
	default:
		return fmt.Errorf("unsupported psql directive %q", cmd[0])
	}
	return nil
}
func validRestrictToken(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

const (
	sqlNormal = iota
	sqlSingle
	sqlEscape
	sqlDouble
	sqlDollar
	sqlLineComment
	sqlBlockComment
)

type sqlSplitter struct {
	mode, depth, statements int
	dollar                  string
	buf                     strings.Builder
	emit                    func(string) error
}

func (s *sqlSplitter) feed(line string) error {
	for i := 0; i < len(line); i++ {
		var err error
		switch s.mode {
		case sqlNormal:
			i, err = s.normal(line, i)
		case sqlSingle, sqlEscape, sqlDouble:
			i = s.quoted(line, i)
		case sqlDollar:
			i = s.dollarText(line, i)
		case sqlLineComment:
			s.buf.WriteByte(line[i])
			if line[i] == '\n' {
				s.mode = sqlNormal
			}
		case sqlBlockComment:
			i = s.comment(line, i)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *sqlSplitter) normal(line string, i int) (int, error) {
	c := line[i]
	if c == '\\' {
		return i, fmt.Errorf("unsupported psql directive")
	}
	if strings.HasPrefix(line[i:], "--") {
		s.mode = sqlLineComment
		s.buf.WriteString("--")
		return i + 1, nil
	}
	if strings.HasPrefix(line[i:], "/*") {
		s.mode = sqlBlockComment
		s.depth = 1
		s.buf.WriteString("/*")
		return i + 1, nil
	}
	if c == '\'' {
		s.mode = sqlSingle
		if escapeQuote(s.buf.String()) {
			s.mode = sqlEscape
		}
		s.buf.WriteByte(c)
		return i, nil
	}
	if c == '"' {
		s.mode = sqlDouble
		s.buf.WriteByte(c)
		return i, nil
	}
	if c == '$' {
		if tag := dollarTag(line[i:]); tag != "" {
			s.mode = sqlDollar
			s.dollar = tag
			s.buf.WriteString(tag)
			return i + len(tag) - 1, nil
		}
	}
	s.buf.WriteByte(c)
	if c == ';' {
		return i, s.statement()
	}
	return i, nil
}
func (s *sqlSplitter) quoted(line string, i int) int {
	c := line[i]
	s.buf.WriteByte(c)
	if s.mode == sqlEscape && c == '\\' && i+1 < len(line) {
		i++
		s.buf.WriteByte(line[i])
		return i
	}
	quote := byte('\'')
	if s.mode == sqlDouble {
		quote = '"'
	}
	if c == quote {
		if i+1 < len(line) && line[i+1] == quote {
			i++
			s.buf.WriteByte(line[i])
		} else {
			s.mode = sqlNormal
		}
	}
	return i
}
func (s *sqlSplitter) dollarText(line string, i int) int {
	if strings.HasPrefix(line[i:], s.dollar) {
		s.buf.WriteString(s.dollar)
		s.mode = sqlNormal
		return i + len(s.dollar) - 1
	}
	s.buf.WriteByte(line[i])
	return i
}
func (s *sqlSplitter) comment(line string, i int) int {
	if strings.HasPrefix(line[i:], "/*") {
		s.depth++
		s.buf.WriteString("/*")
		return i + 1
	}
	if strings.HasPrefix(line[i:], "*/") {
		s.depth--
		s.buf.WriteString("*/")
		if s.depth == 0 {
			s.mode = sqlNormal
		}
		return i + 1
	}
	s.buf.WriteByte(line[i])
	return i
}

func escapeQuote(prefix string) bool {
	n := len(prefix)
	if n == 0 || (prefix[n-1] != 'E' && prefix[n-1] != 'e') {
		return false
	}
	if n == 1 {
		return true
	}
	c := prefix[n-2]
	return !isIdent(c)
}
func isIdent(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
func dollarTag(s string) string {
	if len(s) < 2 || s[0] != '$' {
		return ""
	}
	if s[1] == '$' {
		return "$$"
	}
	if !(s[1] == '_' || s[1] >= 'a' && s[1] <= 'z' || s[1] >= 'A' && s[1] <= 'Z') {
		return ""
	}
	for i := 2; i < len(s); i++ {
		if s[i] == '$' {
			return s[:i+1]
		}
		if !isIdent(s[i]) {
			return ""
		}
	}
	return ""
}
func (s *sqlSplitter) statement() error {
	statement := s.buf.String()
	s.buf.Reset()
	head := stripComments(statement)
	if head == "" || head == ";" {
		return nil
	}
	words := sqlWords(head, 2)
	first := ""
	if len(words) > 0 {
		first = words[0]
	}
	switch first {
	case "BEGIN", "COMMIT", "END", "ROLLBACK", "ABORT", "SAVEPOINT", "RELEASE", "COPY":
		return fmt.Errorf("unsupported transaction or COPY statement %s", first)
	}
	if len(words) > 1 && ((first == "START" && words[1] == "TRANSACTION") || (first == "PREPARE" && words[1] == "TRANSACTION") || (first == "SET" && (words[1] == "TRANSACTION" || words[1] == "SESSION"))) {
		return fmt.Errorf("unsupported transaction statement %s %s", first, words[1])
	}

	s.statements++
	if s.emit != nil {
		return s.emit(statement)
	}
	return nil
}
func hasSQL(s string) bool { return stripComments(s) != "" }
func stripComments(s string) string {
	for {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "--") {
			i := strings.IndexByte(s, '\n')
			if i < 0 {
				return ""
			}
			s = s[i+1:]
			continue
		}
		if strings.HasPrefix(s, "/*") {
			end := commentEnd(s)
			if end < 0 {
				return s
			}
			s = s[end:]
			continue
		}
		return s
	}
}
func commentEnd(s string) int {
	depth := 0
	for i := 0; i+1 < len(s); i++ {
		switch s[i : i+2] {
		case "/*":
			depth++
			i++
		case "*/":
			depth--
			i++
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func sqlWords(s string, n int) []string {
	words := []string{}
	for len(words) < n {
		s = stripComments(s)
		i := 0
		for i < len(s) && (s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z' || s[i] == '_') {
			i++
		}
		if i == 0 {
			break
		}
		words = append(words, strings.ToUpper(s[:i]))
		s = s[i:]
	}
	return words
}
