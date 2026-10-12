package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/diffcheck"
)

// toolCallRE is a call of a repository tool with a verb on a line: `nova-<tool>
// <verb>`, the tool a lower-case word (hyphens allowed) and the verb the next token.
// A shell built-in (ls, cd) is not a `nova-` call, and a verb of another tool is a
// call of that tool, checked against that tool's own table. The line a card change adds
// or edits that runs one of the repository's own tools with a verb sits in a play, a
// workflow, a script or a doc command block.
var toolCallRE = regexp.MustCompile(`\bnova-([a-z0-9]+(?:-[a-z0-9]+)*)[ \t]+([a-z][a-z0-9]*)`)

// calledVerbs checks the repository tool calls of a diff against each tool's verb table at
// the head (docs/SPEC-SPRINT.md section 7, the lander's checks): a call of a tool with a
// verb the tool does not register refuses the head, naming the file, the line, the tool
// and its known verbs. A call of a tool the repository does not contain, or whose table
// has no entry (a shell built-in, a verb of another tool), is not checked. verbs names the
// registered verbs of the tool; nil or empty means the tool is not one the repository
// checks.
func calledVerbs(diff string, verbs func(tool string) []string) []diffcheck.Finding {
	var out []diffcheck.Finding
	known := map[string][]string{}
	for _, f := range diffcheck.Parse(diff) {
		for _, h := range f.Hunks {
			line := h.NewStart
			for _, l := range h.Lines {
				if l[0] == '+' {
					if m := toolCallRE.FindStringSubmatch(l[1:]); m != nil {
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
