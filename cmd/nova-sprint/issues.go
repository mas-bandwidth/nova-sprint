package main

import (
	"io"
	"os"

	"github.com/mas-bandwidth/nova-sprint/internal/issues"
)

// issues import, issues verify and issues roadmap are the folded-in work verbs,
// gathered into nova-sprint (one tool) as the issues group. Each delegates to
// the issues sub-tool (internal/issues), so every flag, output line, exit code,
// refusal and GitHub access is unchanged.

// cmdIssuesImport is nova-sprint issues import.
func (a *app) cmdIssuesImport(args []string, stdout, stderr io.Writer) int {
	return issues.Main(append([]string{"import"}, args...), os.Stdin, stdout, stderr)
}

// cmdIssuesVerify is nova-sprint issues verify.
func (a *app) cmdIssuesVerify(args []string, stdout, stderr io.Writer) int {
	return issues.Main(append([]string{"verify"}, args...), os.Stdin, stdout, stderr)
}

// cmdIssuesRoadmap is nova-sprint issues roadmap.
func (a *app) cmdIssuesRoadmap(args []string, stdout, stderr io.Writer) int {
	return issues.Main(append([]string{"roadmap"}, args...), os.Stdin, stdout, stderr)
}
