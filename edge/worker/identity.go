package worker

import (
	"fmt"
	"strings"
)

// The sprint's commit identity (store.Identity) is configuration, never a
// literal here: the coordinator sets it once (nova-sprint identity), the take
// receipt carries it, and the worker writes it into each job's clone. A
// repository's contributor agreement keys off the forge account, so every
// agent, friend and bench worker commits as that one account; a
// contributor's own name goes only in the message.

// CommitConfig is the two git config lines that make a clone commit as name
// <email>, one per line, each value double-quoted (the identity verb refuses
// a value a shell would read inside double quotes).
func CommitConfig(name, email string) string {
	return fmt.Sprintf("git config user.name %q\ngit config user.email %q", name, email)
}

// CommitRules is the whole rule, word for word the same in the hook's
// output, take's text, JOB.md and the prompt: the identity, the two lines,
// where a contributor's own name goes, and what finish refuses.
func CommitRules(name, email string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Every commit is authored and committed as %s <%s>. In each clone you commit from, before the first commit:\n", name, email)
	for _, l := range strings.Split(CommitConfig(name, email), "\n") {
		b.WriteString("  " + l + "\n")
	}
	b.WriteString("Your own name goes only in the message: a \"By: <your name>\" line and a \"Co-Authored-By: <your name> <your email>\" trailer. finish refuses a head with a commit by any other author or committer email.\n")
	return b.String()
}

// ValidIdentity is "" when name and email are an identity the rules can
// print and git can take, else what is wrong with them.
func ValidIdentity(name, email string) string {
	switch {
	case strings.TrimSpace(name) == "" || strings.TrimSpace(email) == "":
		return "an identity needs a name and an email"
	case strings.ContainsAny(name+email, "\"\\$`<>\n\r\t\x00"):
		return "a name or email holds a quote, backslash, $, backtick, angle bracket or control character"
	case strings.ContainsAny(email, " ") || strings.Count(email, "@") != 1 || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@"):
		return "the email " + email + " is not one address (local@domain, no spaces)"
	case len(name) > 128 || len(email) > 128:
		return "a name or email over 128 bytes"
	}
	return ""
}
