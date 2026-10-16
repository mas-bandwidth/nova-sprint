package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The top-level help is an index into the tool (helpindex.go): every verb on one line, by
// situation, each line within helpIndexWidth columns, the text byte for byte its golden. No
// test here opens a store, a socket or a clock: the index is a string, and a verb's flags are
// read the way its -h reads them (verbFlags), before it opens anything.

var updateHelpGolden = flag.Bool("update-golden-help", false, "write the top-level help index to testdata/help_index.golden")

// indexVerbs is every name the index must carry: the verb table's names, and help and
// version, which main dispatches itself (verbNames).
func indexVerbs() map[string]bool {
	want := map[string]bool{"help": true, "version": true}
	for _, v := range verbs {
		want[v.name] = true
	}
	return want
}

// indexCoverage is every problem of the groups against the verbs they must name: a verb on
// no line, a verb on two, a line naming no verb of the table, a line with no purpose or more
// than one clause of it.
func indexCoverage(groups []indexGroup, want map[string]bool) []string {
	var problems []string
	seen := map[string]int{}
	for _, g := range groups {
		if len(g.rows) == 0 {
			problems = append(problems, "group "+g.title+" has no verb")
		}
		for _, r := range g.rows {
			seen[r.verb]++
			if !want[r.verb] {
				problems = append(problems, "the index names "+r.verb+", which is no verb")
			}
			if strings.TrimSpace(r.purpose) == "" {
				problems = append(problems, r.verb+" has no purpose")
			}
			if strings.Contains(r.purpose, ". ") || strings.HasSuffix(r.purpose, ".") {
				problems = append(problems, r.verb+"'s purpose is more than one clause: "+r.purpose)
			}
		}
	}
	for name := range want {
		switch n := seen[name]; {
		case n == 0:
			problems = append(problems, "the index has no line for "+name)
		case n > 1:
			problems = append(problems, "the index has more than one line for "+name)
		}
	}
	return problems
}

// indexFlagRe is a flag a row's usage names.
var indexFlagRe = regexp.MustCompile(`--[a-z][a-z0-9-]*`)

// indexFlags is every problem of a row's flags: a flag its verb's flag set does not define.
// lookup is the verb's flags (verbFlags); a verb that has none (help, version) is skipped.
func indexFlags(groups []indexGroup, lookup func(verb string) *flag.FlagSet) []string {
	var problems []string
	for _, g := range groups {
		for _, r := range g.rows {
			fs := lookup(r.verb)
			if fs == nil {
				continue
			}
			for _, f := range indexFlagRe.FindAllString(r.usage, -1) {
				if fs.Lookup(strings.TrimPrefix(f, "--")) == nil {
					problems = append(problems, r.verb+" has no flag "+f)
				}
			}
		}
	}
	return problems
}

// overWidth is every line of text wider than width columns.
func overWidth(text string, width int) []string {
	var problems []string
	for i, l := range strings.Split(text, "\n") {
		if n := utf8.RuneCountInString(l); n > width {
			problems = append(problems, fmt.Sprintf("line %d is %d columns: %s", i+1, n, l))
		}
	}
	return problems
}

// Every verb the binary accepts is on exactly one line of the index, and every line names a
// verb the binary accepts, taken from the verb table, never a list kept by hand. The witnesses
// run the same check on groups that must be refused, so a check that passes everything fails.
func TestHelpIndexNamesEveryVerbOnce(t *testing.T) {
	t.Parallel()
	want := indexVerbs()
	for _, p := range indexCoverage(helpIndexGroups, want) {
		t.Error(p)
	}
	// every first word main accepts (verbNames) opens a line of the index
	first := map[string]bool{}
	for _, g := range helpIndexGroups {
		for _, r := range g.rows {
			w, _, _ := strings.Cut(r.verb, " ")
			first[w] = true
		}
	}
	for _, n := range verbNames() {
		assert.True(t, first[n], "the index has no line for the verb word %s", n)
	}

	// the rendered text carries each row as its own line, the command first
	text := helpIndex()
	for _, g := range helpIndexGroups {
		assert.Contains(t, text, "\n"+g.title+":\n", "the index shows the group %s", g.title)
		for _, r := range g.rows {
			assert.Regexp(t, `(?m)^  `+regexp.QuoteMeta(r.command())+` +`+regexp.QuoteMeta(r.purpose)+`$`, text, "the index shows %s on one line", r.verb)
		}
	}

	t.Run("the witnesses", func(t *testing.T) {
		missing := []indexGroup{{"some", []indexRow{{"where", "", "the sprint"}}}}
		assert.Contains(t, strings.Join(indexCoverage(missing, want), "\n"), "the index has no line for inbox")

		madeUp := append([]indexGroup{{"more", []indexRow{{"fly", "", "flies"}}}}, helpIndexGroups...)
		assert.Equal(t, []string{"the index names fly, which is no verb"}, indexCoverage(madeUp, want))

		twice := append([]indexGroup{{"more", []indexRow{{"where", "", "again"}}}}, helpIndexGroups...)
		assert.Equal(t, []string{"the index has more than one line for where"}, indexCoverage(twice, want))

		prose := []indexGroup{{"some", []indexRow{{"where", "", "the sprint. It also explains"}}}}
		assert.Contains(t, strings.Join(indexCoverage(prose, map[string]bool{"where": true}), "\n"), "more than one clause")

		empty := []indexGroup{{"some", []indexRow{{"where", "", " "}}}}
		assert.Contains(t, strings.Join(indexCoverage(empty, map[string]bool{"where": true}), "\n"), "where has no purpose")
	})
}

// Every flag a row of the index names is a flag its verb takes, read from the verb's own
// flag set as its -h reads it.
func TestHelpIndexFlagsAreTheVerbs(t *testing.T) {
	t.Parallel()
	for _, p := range indexFlags(helpIndexGroups, verbFlags) {
		t.Error(p)
	}
	for _, g := range helpIndexGroups {
		for _, r := range g.rows {
			if r.verb != "help" && r.verb != "version" {
				require.NotNil(t, verbFlags(r.verb), "%s gives its flag set to -h", r.verb)
			}
		}
	}

	t.Run("the witness", func(t *testing.T) {
		fly := []indexGroup{{"some", []indexRow{{"where", "[--watch] [--fly]", "the sprint"}}}}
		assert.Equal(t, []string{"where has no flag --fly"}, indexFlags(fly, verbFlags))
	})
}

// Every line of the index is at most helpIndexWidth columns, and it is an index: one line a
// verb, a title and a blank line a group, and the opening and the closing, nothing more.
func TestHelpIndexWidth(t *testing.T) {
	t.Parallel()
	text := helpIndex()
	require.Equal(t, 100, helpIndexWidth, "the index is held to 100 columns")
	for _, p := range overWidth(text, helpIndexWidth) {
		t.Error(p)
	}
	rows := 0
	for _, g := range helpIndexGroups {
		rows += len(g.rows)
	}
	framing := strings.Count(helpIndexOpening, "\n") + 1 + strings.Count(helpIndexClosing, "\n") + 1
	lines := strings.Count(text, "\n")
	assert.Equal(t, rows+2*len(helpIndexGroups)+framing+1, lines, "the index is its rows, its group titles and its frame")
	assert.LessOrEqual(t, strings.Count(helpIndexOpening, "\n")+1, 10, "the orientation is at most 10 lines")

	t.Run("the witness", func(t *testing.T) {
		assert.Empty(t, overWidth(strings.Repeat("x", 100), 100))
		assert.Equal(t, []string{"line 2 is 101 columns: " + strings.Repeat("x", 101)}, overWidth("ok\n"+strings.Repeat("x", 101), 100))
		assert.Equal(t, []string{"line 1 is 101 columns: " + strings.Repeat("é", 101)}, overWidth(strings.Repeat("é", 101), 100), "columns are runes, not bytes")
	})
}

// The index is its golden byte for byte; -update-golden-help writes the golden from it.
func TestHelpIndexGolden(t *testing.T) {
	t.Parallel()
	got := helpIndex()
	path := filepath.Join("testdata", "help_index.golden")
	if *updateHelpGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), got, "nova-sprint help differs from %s (go test -run TestHelpIndexGolden -update-golden-help writes it)", path)
}

// nova-sprint help (and -h, and help with nothing after it) prints the index; help all prints
// the full reference the top-level help printed before; the index says where each detail is,
// the orientation names the first commands and the law, and no verb shadows the topic all.
func TestHelpPrintsTheIndexAndHelpAllTheReference(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		var out, errb bytes.Buffer
		code := runInto(newApp(func(string) string { return "" }), args, &out, &errb)
		require.Equal(t, 0, code, "%v: %s", args, errb.String())
		assert.Equal(t, helpIndex(), out.String(), "%v prints the index", args)
		assert.Empty(t, errb.String(), "%v", args)
	}

	var out, errb bytes.Buffer
	code := runInto(newApp(func(string) string { return "" }), []string{"help", "all"}, &out, &errb)
	require.Equal(t, 0, code, errb.String())
	assert.Equal(t, banner(), out.String(), "help all prints the full reference")

	index := helpIndex()
	assert.True(t, strings.HasPrefix(index, "nova-sprint: a sprint of work cards, dealt to a fleet of workers and read before they land\n"), "the index opens with the README's sentence")
	for _, want := range []string{
		"where (the sprint now)", "inbox (what needs you)", "help playbook",
		"The law: the tools do the mechanical work. Find the verb first",
		"the coordinator, every group", "a worker, \"work rows\"", "a reader, \"reads and\nreview\"",
		"\ndetail:\n", "nova-sprint help <verb>", "nova-sprint help <group>", "nova-sprint help playbook", "nova-sprint help all",
	} {
		assert.Contains(t, index, want, "the index carries %q", want)
	}
	for _, g := range []string{"fleet", "friend", "reader", "stream", "lane", "goal"} {
		require.NotEmpty(t, groupVerbs(g), "help <group> names %s, a group of the verb table", g)
	}
	for _, v := range verbs {
		assert.NotEqual(t, "all", v.name, "a verb named all would shadow the topic")
	}
}
