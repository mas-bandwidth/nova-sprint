package main

import (
	"io"
	"os"

	"github.com/mas-bandwidth/nova-sprint/internal/issues"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/verbflag"
)

// issues import, issues verify and issues roadmap are the folded-in work verbs,
// gathered into nova-sprint (one tool) as the issues group. Each delegates to
// the issues sub-tool (internal/issues), so every flag, output line, exit code,
// refusal and GitHub access is unchanged. Their -h is the app's own help (the
// concrete synopsis from the verb table, not the sub-tool's [flags]
// placeholder), so the group meets nova-sprint's help like every other verb.

// cmdIssuesImport is nova-sprint issues import.
func (a *app) cmdIssuesImport(args []string, stdout, stderr io.Writer) int {
	if verbflag.Asked(args) {
		panic(verbflag.Help{FS: issues.FlagSet("import")})
	}
	return issues.Main(append([]string{"import"}, args...), os.Stdin, stdout, stderr)
}

// cmdIssuesVerify is nova-sprint issues verify.
func (a *app) cmdIssuesVerify(args []string, stdout, stderr io.Writer) int {
	if verbflag.Asked(args) {
		panic(verbflag.Help{FS: issues.FlagSet("verify")})
	}
	return issues.Main(append([]string{"verify"}, args...), os.Stdin, stdout, stderr)
}

// cmdIssuesRoadmap is nova-sprint issues roadmap.
func (a *app) cmdIssuesRoadmap(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && verbflag.IsHelp(args[0]) {
		panic(verbflag.Help{FS: verbflag.New("issues roadmap")})
	}
	return issues.Main(append([]string{"roadmap"}, args...), os.Stdin, stdout, stderr)
}
