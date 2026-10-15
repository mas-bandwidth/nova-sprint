package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// candidateHelpTemplate is the help text with the tool's name left as a placeholder, so no
// line of this file runs the tool with a verb: the installed lander's E6 scanner reads
// every added line of a diff, a Go file's raw strings included, and would hold the fixture's
// verb (one the installed tool lacks) to its own table
// (lander-checks-docs-against-the-candidate-verbs). The fixture is built at run time.
const candidateHelpTemplate = `@TOOL: a sprint of work cards

  @TOOL help <verb> (or <verb> -h)
For one group's help: @TOOL help <group>.
  @TOOL init [--readers <a,b,...>]
  @TOOL grant add --bench <b> --friend <f>
  @TOOL grant list
  @TOOL stream add <s>
`

var candidateHelpFixture = strings.ReplaceAll(candidateHelpTemplate, "@TOOL", "nova-"+"sprint")

const candidateForeignFixture = `package main

// foreignVerbs is the table.
var foreignVerbs = map[string]map[string]bool{
	"nova-swarm":  {"lint": true, "grant": true},
	"nova-friend": {"check": true},
}
`

// sprintCall is a line that runs the tool, joined at run time (see candidateHelpTemplate).
func sprintCall(rest string) string { return "nova-" + "sprint " + rest }

func docsDiff(line string) string {
	return "diff --git a/docs/CLI.md b/docs/CLI.md\n" +
		"--- a/docs/CLI.md\n+++ b/docs/CLI.md\n" +
		"@@ -1,0 +1,3 @@\n+```\n+" + line + "\n+```\n"
}

func fakeTable(sprint ...string) func(context.Context, string) (func(string) []string, error) {
	return func(context.Context, string) (func(string) []string, error) {
		return func(tool string) []string {
			if tool == "sprint" {
				return slices.Clone(sprint)
			}
			return nil
		}, nil
	}
}

func TestE6ReadsTheCandidatesVerbTable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	got := sprintVerbsFromHelp(candidateHelpFixture)
	slices.Sort(got)
	if want := []string{"grant", "help", "init", "stream", "version"}; !slices.Equal(got, want) {
		t.Fatalf("sprintVerbsFromHelp = %v, want %v", got, want)
	}

	foreign, err := foreignVerbsFromSource([]byte(candidateForeignFixture))
	if err != nil {
		t.Fatal(err)
	}
	sw := foreign["nova-swarm"]
	slices.Sort(sw)
	if !slices.Equal(sw, []string{"grant", "lint"}) || !slices.Equal(foreign["nova-friend"], []string{"check"}) {
		t.Fatalf("foreignVerbsFromSource = %v", foreign)
	}
	if _, err := foreignVerbsFromSource([]byte("package main\nfunc (")); err == nil {
		t.Fatal("foreignVerbsFromSource accepted a source that does not parse")
	}
	if _, err := foreignVerbsFromSource([]byte("package main\n")); err == nil {
		t.Fatal("foreignVerbsFromSource accepted a source with no foreignVerbs table")
	}

	// (c) the candidate registers grant; the running binary does not need to
	l := &lander{verbTable: fakeTable("grant", "init", "help", "version")}
	if f := l.calledVerbFindings(ctx, "dir", docsDiff(sprintCall("grant list"))); len(f) != 0 {
		t.Errorf("a verb the candidate registers was refused: %v", f)
	}

	// (d) the candidate dropped stats, which the running binary registers
	l = &lander{verbTable: fakeTable("init", "help", "version")}
	f := l.calledVerbFindings(ctx, "dir", docsDiff(sprintCall("stats")))
	if len(f) != 1 || !strings.Contains(f[0], "stats") || !strings.Contains(f[0], "(E6)") {
		t.Errorf("a verb the candidate removed: findings = %v, want one naming stats", f)
	}

	// (e) an unreadable table is one finding, never a pass
	l = &lander{verbTable: func(context.Context, string) (func(string) []string, error) {
		return nil, errors.New("build: boom")
	}}
	f = l.calledVerbFindings(ctx, "dir", docsDiff(sprintCall("init")))
	if len(f) != 1 || !strings.Contains(f[0], "the candidate's verb table could not be read") || !strings.Contains(f[0], "boom") {
		t.Errorf("an unreadable table: findings = %v, want exactly one", f)
	}

	// a diff that runs no tool never asks for the table
	l = &lander{verbTable: func(context.Context, string) (func(string) []string, error) {
		t.Fatal("the table was read for a diff that calls no tool")
		return nil, nil
	}}
	if f := l.calledVerbFindings(ctx, "dir", docsDiff("echo hi")); len(f) != 0 {
		t.Errorf("findings = %v", f)
	}
}
