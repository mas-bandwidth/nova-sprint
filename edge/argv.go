package edge

import (
	"strings"
	"unicode"
)

// GateFix is the one fix for a gate line that is not one command: a
// DONE-WHEN is one argv, run directly with no shell, so anything that needs
// a shell goes inside sh -c.
const GateFix = "write the gate as one command (a program and its arguments); a pipe, &&, ||, ;, a redirection, a backquote or $( needs a shell, so wrap it: sh -c '...'"

// shellBuiltins are the words a shell runs itself: no program of that name
// exists to run, so as the first word of a gate they can only fail.
var shellBuiltins = map[string]bool{
	"cd": true, "export": true, "source": true, ".": true, "eval": true, "exec": true, "set": true,
	"unset": true, "alias": true, "ulimit": true, "umask": true, "exit": true, "read": true, "shift": true, "wait": true,
}

// ParseGate is a gate line (a card's DONE-WHEN) as the argv that is run, or
// the reason it is not one command. It is the one parser: admission (add,
// check, restore) calls it on the line, and the worker calls it on the same
// line to run it, so what is admitted is what runs.
//
// A line is words split at white space; a single or double quote joins what
// it encloses into the word (and a quote may sit inside a word), with no
// escapes and nothing expanded inside. Nothing runs a shell, so shell syntax
// outside a quote would reach the program as a literal argument: the backquote,
// ; & | < > ( ) and backslash are refused outside a quote, as are $( ${ and $
// before a letter or _ (a lone $ is a literal, as in a regular expression's
// end anchor). The first word must be a program: a path (it holds a /), or a
// name that starts with a lower-case letter or digit and holds only letters,
// digits and . _ + - @ and does not end in a dot, and is not a shell builtin
// (cd, export, source and the like). That form is all the coordinator can
// check at add; a name that matches the form but is not on the bench's PATH
// is met where the gate runs, as the wall's own command-not-found.
func ParseGate(line string) ([]string, string) {
	args, why := tokenizeGate_internal(line)
	switch {
	case why != "":
		return nil, why
	case len(args) == 0:
		return nil, "the command is empty"
	}
	if why := firstWordProblem_internal(args[0]); why != "" {
		return nil, why
	}
	return args, ""
}

// tokenizeGate_internal splits line into words, or says what outside a quote
// is shell syntax or what quote is left open.
func tokenizeGate_internal(line string) ([]string, string) {
	var args []string
	var cur strings.Builder
	var quote rune
	inWord := false
	rs := []rune(line)
	for i, r := range rs {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case unicode.IsSpace(r):
			if inWord {
				args, inWord = append(args, cur.String()), false
				cur.Reset()
			}
		default:
			if why := shellSyntax_internal(rs, i); why != "" {
				return nil, why
			}
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, "a " + string(quote) + " is left open"
	}
	if inWord {
		args = append(args, cur.String())
	}
	return args, ""
}

// shellSyntax_internal is what rs[i], outside a quote, is as shell syntax, or
// "".
func shellSyntax_internal(rs []rune, i int) string {
	next := rune(0)
	if i+1 < len(rs) {
		next = rs[i+1]
	}
	switch r := rs[i]; {
	case r == '`':
		return "a backquote is shell syntax (nothing runs a shell)"
	case r == '\\':
		return "a backslash is shell syntax (a gate has no escapes: use quotes)"
	case (r == '&' || r == '|' || r == '<' || r == '>') && next == r:
		return string(r) + string(r) + " is shell syntax (nothing runs a shell)"
	case r == ';' || r == '&' || r == '|' || r == '<' || r == '>' || r == '(' || r == ')':
		return string(r) + " is shell syntax (nothing runs a shell)"
	case r == '$' && (next == '(' || next == '{' || next == '_' || unicode.IsLetter(next)):
		return "$" + string(next) + " is shell syntax (nothing expands a variable)"
	}
	return ""
}

// firstWordProblem_internal is why w is not a program to run, or "".
func firstWordProblem_internal(w string) string {
	switch {
	case strings.HasPrefix(w, "~"):
		return "the first word " + w + " starts with ~, which nothing expands: write the path out"
	case strings.Contains(w, "/"):
		return ""
	case shellBuiltins[w]:
		return "the first word " + w + " is a shell builtin, not a program"
	case strings.Contains(w, "="):
		return "the first word " + w + " is an assignment: set a variable with env, as env NAME=value program"
	case !programName_internal(w):
		return "the first word " + w + " is not a program name (a name starts with a lower-case letter or digit and holds only letters, digits and . _ + - @): a sentence is not a gate"
	}
	return ""
}

// programName_internal is whether w has the form of a bare program name.
func programName_internal(w string) bool {
	if w == "" || strings.HasSuffix(w, ".") {
		return false
	}
	for i, r := range w {
		lower, digit := r >= 'a' && r <= 'z', r >= '0' && r <= '9'
		switch {
		case lower || digit:
		case i > 0 && (r >= 'A' && r <= 'Z' || strings.ContainsRune("._+-@", r)):
		default:
			return false
		}
	}
	return true
}
