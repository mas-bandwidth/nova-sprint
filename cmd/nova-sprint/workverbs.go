package main

import (
	"fmt"
	"io"
)

// workVerbNames are nova-sprint's work verbs. repos, issues and roadmap name
// the program. export and import are the round-trip with the work tree.
// None is built (docs/SPEC-WORK-STORE.md). They are reads until export and
// import exist: those two write, and become the coordinator's then.
var workVerbNames = []string{"work repos", "work issues", "work roadmap", "work export", "work import"}

func init() {
	for _, name := range workVerbNames {
		verbClasses[name] = classRead
		verbEffect[name] = "inspection: reads nothing and writes nothing; the work-tree round-trip is not built (docs/SPEC-WORK-STORE.md)"
		verbExit[name] = "exit codes: 1 the work-tree round-trip is not built, nothing read and nothing written, 2 usage"
	}
}

const workVerbWords = `work repos, work issues and work roadmap name the sprint's program in the work tree.
work export writes that program and its state; work import loads a program.
The round-trip is not built: each verb reads nothing and writes nothing (docs/SPEC-WORK-STORE.md).
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
	fs.String("tree", "", "the sprint program `file`, when the round-trip exists (docs/SPEC-WORK-STORE.md); this verb does not read it")
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, name, argErr("takes no words ", err, pos...))
	}
	fmt.Fprintf(stderr, "nova-sprint %s REFUSED: the work-tree round-trip is not built; nothing was read and nothing was written; run: nova-sprint help %s\n", name, name)
	return 1
}
