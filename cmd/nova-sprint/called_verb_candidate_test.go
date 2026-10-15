package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

const candidateHelpFixture = `nova-sprint: a sprint of work cards

  nova-sprint help <verb> (or <verb> -h)
For one group's help: nova-sprint help <group>.
  nova-sprint init [--readers <a,b,...>]
  nova-sprint grant add --bench <b> --friend <f>
  nova-sprint grant list
  nova-sprint stream add <s>
`

const candidateForeignFixture = `package main

// foreignVerbs is the table.
var foreignVerbs = map[string]map[string]bool{
	"nova-swarm":  {"lint": true, "grant": true},
	"nova-friend": {"check": true},
}
`

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
	if f := l.calledVerbFindings(ctx, "dir", docsDiff("nova-sprint grant list")); len(f) != 0 {
		t.Errorf("a verb the candidate registers was refused: %v", f)
	}

	// (d) the candidate dropped stats, which the running binary registers
	l = &lander{verbTable: fakeTable("init", "help", "version")}
	f := l.calledVerbFindings(ctx, "dir", docsDiff("nova-sprint stats"))
	if len(f) != 1 || !strings.Contains(f[0], "stats") || !strings.Contains(f[0], "(E6)") {
		t.Errorf("a verb the candidate removed: findings = %v, want one naming stats", f)
	}

	// (e) an unreadable table is one finding, never a pass
	l = &lander{verbTable: func(context.Context, string) (func(string) []string, error) {
		return nil, errors.New("build: boom")
	}}
	f = l.calledVerbFindings(ctx, "dir", docsDiff("nova-sprint init"))
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
