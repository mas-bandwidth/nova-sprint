package verbs

import (
	"regexp"
	"strings"
)

// The retired mechanics (CARD-CONTRACT "Retired mechanics"): what v1's
// briefs carried that v2 does for itself. A brief naming one is refused at
// add, naming the line, because a worker that follows it does the wrong
// thing (writes a JOB.md nobody reads, waits for a lock nobody holds, ssh's
// to a bench the deal never picked).
const retiredFix = "remove it: the deal names the row, the take receipt is the job, a gate names the facts it needs (an OS, a tool) and never a host, and nothing is locked by hand; then nova-sprint add again"

var (
	retiredLock = regexp.MustCompile(`(?i)\b(winlock|flock|lockfile|captain lock|bench lock)\b`)
	retiredSSH  = regexp.MustCompile(`(?i)\bssh\s+(to|into|on)\s+[A-Za-z0-9][A-Za-z0-9.-]*`)
	gateSSH     = regexp.MustCompile(`(?i)(^|[\s;&|(])ssh\s+(-\S+\s+)*[A-Za-z0-9_.@-]+`)
)

// retiredMechanic is the first line of a brief that names a retired
// mechanic: its number, what it names, and the fix; what is "" for none.
// A gate (DONE-WHEN, TEST) that runs ssh is refused as well as prose that
// sends the worker to a host by name.
func retiredMechanic(lines []string) (int, string, string) {
	for i, line := range lines {
		key, value, _ := strings.Cut(strings.TrimSpace(line), ":")
		gate := key == "DONE-WHEN" || key == "TEST"
		switch {
		case strings.Contains(line, "JOB.md"):
			return i + 1, "JOB.md", retiredFix
		case strings.HasPrefix(strings.TrimSpace(line), "WHO:"):
			return i + 1, "a WHO line", retiredFix
		case retiredLock.MatchString(line):
			return i + 1, "a lock (" + retiredLock.FindString(line) + ")", retiredFix
		case retiredSSH.MatchString(line):
			return i + 1, "a host to ssh to (" + retiredSSH.FindString(line) + ")", retiredFix
		case gate && gateSSH.MatchString(value):
			return i + 1, "a gate that runs on a named host (" + strings.TrimSpace(gateSSH.FindString(value)) + ")", retiredFix
		}
	}
	return 0, "", ""
}

// shellProblem is why cmd does not parse as one POSIX shell command line,
// or "": a quote, a parenthesis, a $( or a backquote left open, a dangling
// backslash, or a line that starts or ends with an operator. It checks the
// form only; what the command does is the gate's own business.
func shellProblem(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "the command is empty"
	}
	if open := shellOpen(cmd); open != "" {
		return open
	}
	for _, op := range []string{"|", "&", ";"} {
		if strings.HasPrefix(cmd, op) {
			return "it starts with " + op
		}
	}
	for _, op := range []string{"|", "&&", "||", "\\"} {
		if strings.HasSuffix(cmd, op) && !strings.HasSuffix(cmd, "\\"+op) {
			return "it ends with " + op
		}
	}
	return ""
}

// shellOpen is what cmd leaves open (a quote, a backquote, a parenthesis),
// or "".
func shellOpen(cmd string) string {
	var quote byte // ', " or `
	depth := 0
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case c == '\\' && quote != '\'':
			i++ // the next byte is escaped
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"' || c == '`':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth--; depth < 0 {
				return "a ) closes nothing"
			}
		}
	}
	switch {
	case quote != 0:
		return "a " + string(quote) + " is left open"
	case depth > 0:
		return "a ( is left open"
	}
	return ""
}
