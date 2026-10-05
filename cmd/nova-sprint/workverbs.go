package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/work"
)

// workVerbNames are nova-sprint's work verbs. repos, issues and roadmap name
// the program. export and import are the round-trip. None is built
// (internal/work). They are reads until export and import exist: those two
// write, and become the coordinator's then.
var workVerbNames = []string{"work repos", "work issues", "work roadmap", "work export", "work import"}

func init() {
	if !strings.Contains(work.Note, work.Unbuilt) {
		panic("internal/work.Note does not state the work-verb refusal")
	}
	for _, name := range workVerbNames {
		verbClasses[name] = classRead
		verbEffect[name] = "inspection: reads nothing and writes nothing; " + work.Unbuilt
		verbExit[name] = "exit codes: 1 the work-tree round-trip is not built, nothing read and nothing written, 2 usage"
	}
}

const workVerbWords = `work repos, work issues and work roadmap name the sprint's program in the work tree.
work export writes that program and its state; work import loads a program.
The round-trip is not built: each verb reads nothing and writes nothing (internal/work).
nova-work import and nova-work verify are the GitHub issue mirror, not these verbs.
`

func (a *app) cmdWorkRepos(args []string, stdout, stderr io.Writer) int {
	return a.workNotBuilt("work repos", args, stdout, stderr)
}

func (a *app) cmdWorkIssues(args []string, stdout, stderr io.Writer) int {
	return a.workNotBuilt("work issues", args, stdout, stderr)
}

func (a *app) cmdWorkRoadmap(args []string, stdout, stderr io.Writer) int {
	return a.workNotBuilt("work roadmap", args, stdout, stderr)
}

func (a *app) cmdWorkExport(args []string, stdout, stderr io.Writer) int {
	return a.workNotBuilt("work export", args, stdout, stderr)
}

func (a *app) cmdWorkImport(args []string, stdout, stderr io.Writer) int {
	return a.workNotBuilt("work import", args, stdout, stderr)
}

// workNotBuilt is every work verb until the round-trip exists. -h stops in
// parse. A run that parses exits 1 and touches no store and no file.
func (a *app) workNotBuilt(name string, args []string, stdout, stderr io.Writer) int {
	fs, _ := a.verbSetup(name)
	fs.String("tree", "", "the sprint program file, when the round-trip exists (internal/work); this verb does not read it")
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, name, argErr("takes no words ", err, pos...))
	}
	fmt.Fprintf(stderr, "nova-sprint %s REFUSED: %s; run: nova-sprint help %s\n", name, work.Unbuilt, name)
	return 1
}
