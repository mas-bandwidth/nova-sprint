package main

import (
	"io"

	"github.com/mas-bandwidth/nova-sprint/internal/card"
)

func init() {
	// These verbs write briefs on disk, or read them, and never the sprint
	// store, so they need no actor and the server does not run them (notServed).
	// classRead is that: a class, not a claim that generate writes nothing.
	verbClasses["card generate"] = classRead
	verbClasses["card lint"] = classRead
	verbClasses["card template"] = classRead
}

func (a *app) cmdCardGenerate(args []string, stdout, stderr io.Writer) int {
	return card.Generate(args, stdout, stderr)
}

func (a *app) cmdCardLint(args []string, stdout, stderr io.Writer) int {
	return card.Lint(args, stdout, stderr)
}

func (a *app) cmdCardTemplate(args []string, stdout, stderr io.Writer) int {
	return card.Template(args, stdout, stderr)
}

// movedCardVerb is the nova-sprint verb a retired nova-card verb name now is,
// or "" when the word was not one of those verbs. nova-sprint generate,
// template and lint refuse with it rather than running as an unknown verb.
func movedCardVerb(name string) string {
	switch name {
	case "generate", "template", "lint":
		return "card " + name
	default:
		return ""
	}
}
