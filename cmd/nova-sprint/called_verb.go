package main

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-tools/pkg/diffcheck"
)

// toolCallRE matches a line that runs one of the repository's own tools with a verb:
// `nova-<tool> <verb>`, the tool a lower-case word (hyphens allowed) and the verb the
// next token. The line runs the tool only where the tool begins the command — after
// indentation, a play's list dash, a shell prompt, a workflow's `run:`, `env` and any
// leading `VAR=` assignments — so a tool merely named later in a sentence (prose or a Go
// string) is not a call. A shell built-in (ls, cd) is not a `nova-` call, and a verb of
// another tool is a call of that tool, checked against that tool's own table. The line a
// card change adds or edits that runs one of the repository's own tools with a verb sits
// in a play, a workflow, a script or a doc command block (docs/SPEC-SPRINT.md section 7,
// the lander's checks).
var toolCallRE = regexp.MustCompile(`^[ \t]*(?:-[ \t]+|[$>][ \t]+|run:[ \t]+)?(?:env[ \t]+)?(?:[A-Za-z_][A-Za-z0-9_]*=[^ \t]+[ \t]+)*nova-([a-z0-9]+(?:-[a-z0-9]+)*)[ \t]+([a-z][a-z0-9]*)`)

// calledVerbs checks the repository tool calls of a diff against each tool's verb table at
// the head (docs/SPEC-SPRINT.md section 7, the lander's checks): a line that runs a tool
// with a verb the tool does not register refuses the head, naming the file, the line, the
// tool and its known verbs. A tool with no table (a shell built-in, a tool the repository
// does not hold) is not checked. verbs names the registered verbs of the tool; nil or
// empty means the tool is not one the repository checks.
//
// The line is read only where it runs the tool: a play, a workflow, a script or a doc
// command block (docs/SPEC-SPRINT.md section 7, the lander's checks). A Markdown file's
// prose is not a command block, so a line of it is read only inside a fenced code block
// (fenceLine); a prose paragraph whose first token is a tool name is not a call. A
// non-Markdown file (a play, a workflow, a script) is read by toolCallRE's command shapes
// alone.
func calledVerbs(diff string, verbs func(tool string) []string) []diffcheck.Finding {
	var out []diffcheck.Finding
	known := map[string][]string{}
	for _, f := range diffcheck.Parse(diff) {
		md := path.Ext(f.New) == ".md"
		inFence := false
		for _, h := range f.Hunks {
			line := h.NewStart
			for _, l := range h.Lines {
				body := l[1:]
				if md && fenceLine(body) {
					inFence = !inFence
				}
				if l[0] == '+' && (!md || inFence) {
					if m := toolCallRE.FindStringSubmatch(body); m != nil {
						tool, verb := m[1], m[2]
						table, ok := known[tool]
						if !ok {
							table = verbs(tool)
							slices.Sort(table)
							known[tool] = table
						}
						if len(table) == 0 || slices.Contains(table, verb) {
							continue
						}
						out = append(out, diffcheck.Finding{File: f.New, Line: line, Why: fmt.Sprintf(
							"runs %s with the verb %q, which the tool does not register; %s's verbs are %s",
							tool, verb, tool, strings.Join(table, ", "))})
					}
				}
				if l[0] != '-' {
					line++
				}
			}
		}
	}
	return out
}

// fenceLine says a Markdown line opens or closes a fenced code block: it begins with
// three or more backticks (a tilde fence is not read, matching prose's fence in
// pkg/diffcheck).
func fenceLine(l string) bool {
	return strings.HasPrefix(strings.TrimSpace(l), "```")
}

// registeredVerbs is the verb table of the tools this running binary carries: nova-sprint's
// own (verbNames(), read from verbs.go) and, for the tools nova-sprint does not build
// (nova-friend, nova-swarm, nova-config and the rest of foreignVerbs), the runbook's list.
// It is not the table the lander's E6 check holds a head to when the head carries
// cmd/nova-sprint: that is the candidate tree's, built and read by candidateVerbs
// (lander-checks-docs-against-the-candidate-verbs). This one answers for a tree without
// cmd/nova-sprint (nova-tools and every other repository), whose docs call the installed
// tools, and for the tests. A tool of neither is not checked.
func registeredVerbs(tool string) []string {
	if tool == "sprint" {
		return verbNames()
	}
	verbs := foreignVerbs["nova-"+tool]
	out := make([]string, 0, len(verbs))
	for v := range verbs {
		out = append(out, v)
	}
	return out
}
