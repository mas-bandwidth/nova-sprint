package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// usesBrief writes a passing brief whose header names the DEPENDS-ON line given, the PATHS and
// NEW globs, and a task text that mentions task (docs/SPEC-CARD-CONTRACT.md section 2, the
// DEPENDS-ON grammar): the symbols a later card may name under `uses:`.
func usesBrief(t *testing.T, dir, id, depends, paths, news, task string) string {
	t.Helper()
	lead := "RESULT: " + id + " sha=000000000000 tier: flash\nKIND: fix\nDEPENDS-ON: " + depends + "\nPATHS: " + paths + "\n"
	if news != "" {
		lead += "NEW: " + news + "\n"
	}
	lead += "\n" + task
	path := filepath.Join(dir, id+".md")
	require.NoError(t, os.WriteFile(path, []byte(passingBrief(lead)), 0o600))
	return path
}

// lintLines are the LINT DRIFT lines of a verb's stderr that carry the token, each on its own
// line.
func lintLines(errs, token string) []string {
	var out []string
	for _, l := range strings.Split(errs, "\n") {
		if strings.HasPrefix(l, "LINT DRIFT ") && strings.Contains(l, "check="+token) {
			out = append(out, l)
		}
	}
	return out
}

// add refuses every DEPENDS-ON entry that does not name what it consumes (docs/SPEC-CARD-CONTRACT.md
// section 2): a bare id, a uses the needed card does not produce, an overlap with no why or a file
// the two cards do not both name, rank words in place of a reason, and a need that is no card; the
// call exits 2, writes nothing, and prints each finding on its own LINT DRIFT line with its remedy.
// The passing forms are admitted with their needs recorded as the ids alone; a kept overlap is
// counted on OVERLAP NEEDS.
func TestAddRefusesANeedWithoutUses(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	ta.ok("add --one --stream s n-1 --brief-file " + usesBrief(t, dir, "n-1", "-", "internal/n1.go, docs/CLI.md", "internal/n1_new.go", "Add the Frobnicate verb."))
	applied := ta.applies()

	refused := []struct{ name, depends, task, want string }{
		{"a bare id", "n-1", "Fix n-2.", "names neither `uses:` nor `overlap:`"},
		{"a note is no reason", "n-1 (it goes first)", "Fix n-2.", "names neither `uses:` nor `overlap:`"},
		{"a symbol the needed card does not produce", "n-1 (uses: Defrobnicate)", "Fix n-2.", "appears nowhere in the NEW, PATHS or task text of n-1"},
		{"a path the needed card does not name", "n-1 (uses: internal/other.go)", "Fix n-2.", "no PATHS or NEW entry of n-1 names"},
		{"an overlap with no why", "n-1 (overlap: docs/CLI.md)", "Fix n-2.", "gives no `why:`"},
		{"an overlap on a file the other card does not name", "n-1 (overlap: docs/SPEC-SPRINT.md; why: both edit the page and a conflict is a redo)", "Fix n-2.", "not in both cards' PATHS or NEW"},
		{"a rank word on the line", "n-1 (uses: Frobnicate) independent", "Fix n-2.", "rank word"},
		{"a rank sentence naming the need", "n-1 (uses: Frobnicate)", "Best after n-1 lands.", "rank word"},
		{"a need that is no card", "zz-9 (uses: Frobnicate)", "Fix n-2.", "zz-9"},
	}
	for _, c := range refused {
		path := usesBrief(t, dir, "n-2", c.depends, "internal/n2.go, docs/CLI.md", "", c.task)
		code, out, errs := ta.do("add --one --stream s n-2 --brief-file " + path)
		assert.Equal(t, 2, code, c.name+"\n"+errs)
		lines := append(lintLines(errs, "depends-uses"), lintLines(errs, "depends-unknown")...)
		require.NotEmpty(t, lines, c.name+"\n"+errs)
		assert.Contains(t, strings.Join(lines, "\n"), c.want, c.name)
		if !strings.Contains(c.want, "zz-9") {
			assert.Contains(t, lines[0], "(uses:", c.name+": the remedy names the uses form")
			assert.Contains(t, lines[0], "(overlap:", c.name+": the remedy names the overlap form")
		}
		assert.NotContains(t, out, "OVERLAP NEEDS")
		assert.Equal(t, applied, ta.applies(), c.name+": nothing was written")
	}

	// the passing forms: the needs recorded are the ids alone, the brief keeps the entry whole
	ok := []struct{ name, depends string }{
		{"a symbol", "n-1 (uses: Frobnicate)"},
		{"a verb spelling and a path", "n-1 (uses: Frobnicate, internal/n1_new.go)"},
		{"a glob of the needed card", "n-1 (uses: internal/n1.go)"},
	}
	for i, c := range ok {
		id := "ok-" + string(rune('a'+i))
		path := usesBrief(t, dir, id, c.depends, "internal/"+id+".go", "", "Fix "+id+".")
		out := ta.ok("add --one --stream s " + id + " --brief-file " + path)
		assert.Contains(t, out, "MOVED "+id+" -> waiting", c.name)
		assert.NotContains(t, out, "OVERLAP NEEDS", c.name)
		assert.Equal(t, "n-1", ta.primary(id).F("needs"), c.name)
		assert.Contains(t, ta.primary(id).F("brief"), "DEPENDS-ON: "+c.depends+"\n", c.name)
	}

	// a kept overlap is admitted with its why and counted
	path := usesBrief(t, dir, "ov-1", "n-1 (overlap: docs/CLI.md; why: both rewrite the verb table and a conflict is a full redo)", "docs/CLI.md", "", "Fix ov-1.")
	out := ta.ok("add --one --stream s ov-1 --brief-file " + path)
	assert.Contains(t, out, "MOVED ov-1 -> waiting")
	assert.Contains(t, out, "\nOVERLAP NEEDS 1\n")
	assert.Equal(t, "n-1", ta.primary("ov-1").F("needs"))

	// the needed card of the same add is found there: --brief-dir resolves a need among its own
	// briefs before the table, and holds the pair whole
	many := t.TempDir()
	usesBrief(t, many, "p-1", "-", "internal/p1.go", "internal/p1_new.go", "Add the Quux verb.")
	usesBrief(t, many, "p-2", "p-1 (uses: internal/p1_new.go, Quux)", "internal/p2.go", "", "Fix p-2.")
	out = ta.ok("add --stream t --brief-dir " + many)
	assert.Contains(t, out, "MOVED p-2 -> waiting")
	assert.Equal(t, "p-1", ta.primary("p-2").F("needs"))
	usesBrief(t, many, "q-1", "-", "internal/q1.go", "", "Fix q-1.")
	usesBrief(t, many, "q-2", "q-1", "internal/q2.go", "", "Fix q-2.")
	before := ta.applies()
	code, _, errs := ta.do("add --stream u --brief-dir " + many)
	assert.Equal(t, 2, code, errs)
	assert.NotEmpty(t, lintLines(errs, "depends-uses"), errs)
	assert.Equal(t, before, ta.applies(), "one red brief refuses the whole directory")
}

// needs set takes the DEPENDS-ON grammar on each --needs entry: `--needs 'a (uses: pkg/x/y.go), b
// (overlap: docs/CLI.md; why: ...)'`; a bare id is refused with the same finding, one red entry
// refuses the whole call, exit 2, nothing written; --clear is never refused. The card records the
// ids alone and its brief's needs line shows the entries whole; a kept overlap is counted.
func TestNeedsSetRefusesANeedWithoutUses(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	ta.ok("add --one --stream s n-1 --brief-file " + usesBrief(t, dir, "n-1", "-", "internal/n1.go, docs/CLI.md", "pkg/x/y.go", "Add the Frobnicate verb."))
	ta.ok("add --one --stream s n-2 --brief-file " + usesBrief(t, dir, "n-2", "-", "internal/n2.go, docs/CLI.md", "", "Add the Quux verb."))
	ta.ok("add --one --stream s n-3 --brief-file " + usesBrief(t, dir, "n-3", "-", "internal/n3.go, docs/CLI.md", "", "Fix n-3."))
	applied := ta.applies()

	for _, c := range []struct{ name, needs, want string }{
		{"a bare id", "n-1", "names neither `uses:` nor `overlap:`"},
		{"one bare id among good entries", "n-1 (uses: pkg/x/y.go), n-2", "names neither `uses:` nor `overlap:`"},
		{"a bare list", "n-1,n-2", "names neither `uses:` nor `overlap:`"},
		{"a thing not produced", "n-1 (uses: Defrobnicate)", "appears nowhere in the NEW, PATHS or task text of n-1"},
		{"an overlap with no why", "n-1 (overlap: docs/CLI.md)", "gives no `why:`"},
		{"an overlap on a file not in both cards", "n-1 (overlap: internal/n1.go; why: both change the file and a conflict is a redo)", "not in both cards' PATHS or NEW"},
	} {
		code, out, errs := ta.do("needs set n-3 --needs '" + c.needs + "'")
		assert.Equal(t, 2, code, c.name+"\n"+errs)
		lines := lintLines(errs, "depends-uses")
		require.NotEmpty(t, lines, c.name+"\n"+errs)
		assert.Contains(t, strings.Join(lines, "\n"), c.want, c.name)
		assert.Contains(t, lines[0], "(uses:", c.name)
		assert.Contains(t, lines[0], "(overlap:", c.name)
		assert.NotContains(t, out, "OVERLAP NEEDS", c.name)
		assert.Equal(t, applied, ta.applies(), c.name+": nothing was written")
		assert.Equal(t, "", ta.primary("n-3").F("needs"), c.name)
	}

	// the passing forms: ids alone recorded, the entries whole in the brief, the overlap counted
	out := ta.ok("needs set n-3 --needs 'n-1 (uses: pkg/x/y.go, Frobnicate), n-2 (overlap: docs/CLI.md; why: both rewrite the verb table and a conflict is a full redo)'")
	assert.Contains(t, out, "MOVED n-3 needs n-1,n-2")
	assert.Contains(t, out, "\nOVERLAP NEEDS 1\n")
	assert.Equal(t, "n-1,n-2", ta.primary("n-3").F("needs"))
	assert.Contains(t, ta.primary("n-3").F("brief"), "DEPENDS-ON: n-1 (uses: pkg/x/y.go, Frobnicate), n-2 (overlap: docs/CLI.md; why: both rewrite the verb table and a conflict is a full redo)\n")

	// the edit keeps a retained need's reason: n-3 re-pointed to n-1 alone is n-1's entry whole
	out = ta.ok("needs set n-3 --needs 'n-1 (uses: Frobnicate)'")
	assert.NotContains(t, out, "OVERLAP NEEDS")
	assert.Equal(t, "n-1", ta.primary("n-3").F("needs"))
	assert.Contains(t, ta.primary("n-3").F("brief"), "DEPENDS-ON: n-1 (uses: Frobnicate)\n")

	// --clear is never refused
	ta.ok("needs set n-3 --clear")
	assert.Equal(t, "", ta.primary("n-3").F("needs"))
}

// The example needs set's -h shows (a uses entry and an overlap entry) is a line the verb admits,
// run against cards whose briefs hold what it names: it exits 0 and counts the overlap.
func TestNeedsSetHelpExampleIsAdmitted(t *testing.T) {
	t.Parallel()
	example := ""
	for _, v := range verbs {
		if v.name == "needs" {
			example = v.example
		}
	}
	require.NotEmpty(t, example)
	assert.Contains(t, example, "(uses:", "the example shows a uses entry")
	assert.Contains(t, example, "(overlap:", "the example shows an overlap entry")
	var out, errb bytes.Buffer
	code := runInto(newApp(func(string) string { return "" }), []string{"needs", "-h"}, &out, &errb)
	assert.Equal(t, 0, code)
	assert.Contains(t, out.String(), "example:\n  "+prog+" "+example+"\n")
	assert.Contains(t, out.String(), "(uses: ", "the help prints the grammar")
	assert.Contains(t, out.String(), "OVERLAP NEEDS")

	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	ta.ok("add --one --stream s1 s1-1 --brief-file " + usesBrief(t, dir, "s1-1", "-", "internal/sprint/steps_edit.go, docs/CLI.md", "", "Edit the needs steps."))
	ta.ok("add --one --stream s1 s1-2 --brief-file " + usesBrief(t, dir, "s1-2", "-", "docs/CLI.md", "", "Edit the verb table."))
	ta.ok("add --one --stream s1 s1-4 --brief-file " + usesBrief(t, dir, "s1-4", "-", "docs/CLI.md, internal/s14.go", "", "Fix s1-4."))
	out2 := ta.ok(example)
	assert.Contains(t, out2, "MOVED s1-4 needs s1-1,s1-2")
	assert.Contains(t, out2, "\nOVERLAP NEEDS 1\n")
}

// brief holds the new text to the DEPENDS-ON grammar, a DEPENDS-ON-only edit too (the commonest
// way an edge is written): a bare id is refused, exit 2, nothing written, the needed brief looked
// for among the call's briefs before the table; a text that carries a CARRY: head= line (what
// --widen writes from the card's own) is not held again; a kept overlap is counted.
func TestBriefHoldsTheDependsOnGrammar(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	ta.ok("add --one --stream s b-1 --brief-file " + usesBrief(t, dir, "b-1", "-", "internal/b1.go, docs/CLI.md", "", "Add the Frobnicate verb."))
	ta.ok("add --one --stream s b-0 --brief-file " + usesBrief(t, dir, "b-0", "-", "internal/b0.go", "", "Add the Zed verb."))
	ta.ok("add --one --stream s b-2 --brief-file " + usesBrief(t, dir, "b-2", "b-0 (uses: Zed)", "internal/b2.go, docs/CLI.md", "", "Fix b-2."))
	applied := ta.applies()

	for _, c := range []struct{ name, depends, want string }{
		{"a bare id", "b-1", "names neither `uses:` nor `overlap:`"},
		{"a thing not produced", "b-1 (uses: Defrobnicate)", "appears nowhere in the NEW, PATHS or task text of b-1"},
		{"an overlap with no why", "b-1 (overlap: docs/CLI.md)", "gives no `why:`"},
	} {
		code, _, errs := ta.do("brief b-2 --brief-file " + usesBrief(t, t.TempDir(), "b-2", c.depends, "internal/b2.go, docs/CLI.md", "", "Fix b-2."))
		assert.Equal(t, 2, code, c.name+"\n"+errs)
		assert.Contains(t, strings.Join(lintLines(errs, "depends-uses"), "\n"), c.want, c.name)
		assert.Equal(t, applied, ta.applies(), c.name+": nothing was written")
		assert.Equal(t, "b-0", ta.primary("b-2").F("needs"), c.name)
	}

	out := ta.ok("brief b-2 --brief-file " + usesBrief(t, t.TempDir(), "b-2", "b-1 (uses: Frobnicate)", "internal/b2.go, docs/CLI.md", "", "Fix b-2."))
	assert.NotContains(t, out, "OVERLAP NEEDS")
	assert.Equal(t, "b-0,b-1", ta.primary("b-2").F("needs"), "the card keeps its need b-0 and gains b-1")
	assert.Contains(t, ta.primary("b-2").F("brief"), "DEPENDS-ON: b-1 (uses: Frobnicate)\n")

	// the same edge as an overlap, counted
	out = ta.ok("brief b-2 --brief-file " + usesBrief(t, t.TempDir(), "b-2", "b-1 (overlap: docs/CLI.md; why: both rewrite the verb table and a conflict is a full redo)", "internal/b2.go, docs/CLI.md", "", "Fix b-2."))
	assert.Contains(t, out, "\nOVERLAP NEEDS 1\n")

	// a text of the call is the needed brief before the table: b-1's new text drops Frobnicate
	many := t.TempDir()
	usesBrief(t, many, "b-1", "-", "internal/b1.go, docs/CLI.md", "", "Add the Quux verb.")
	usesBrief(t, many, "b-2", "b-1 (uses: Frobnicate)", "internal/b2.go, docs/CLI.md", "", "Fix b-2.")
	before := ta.applies()
	code, _, errs := ta.do("brief --dir " + many)
	assert.Equal(t, 2, code, errs)
	assert.Contains(t, strings.Join(lintLines(errs, "depends-uses"), "\n"), "appears nowhere in the NEW, PATHS or task text of b-1")
	assert.Equal(t, before, ta.applies(), "one red brief refuses the whole directory")
	usesBrief(t, many, "b-2", "b-1 (uses: Quux)", "internal/b2.go, docs/CLI.md", "", "Fix b-2.")
	ta.ok("brief --dir " + many)
}

// brief check runs the DEPENDS-ON grammar with the checked directory's briefs as the needed ones
// and the table behind them, and collects every finding with the others.
func TestBriefCheckHoldsTheDependsOnGrammar(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	usesBrief(t, dir, "c-1", "-", "internal/c1.go", "internal/c1_new.go", "Add the Frobnicate verb.")
	usesBrief(t, dir, "c-2", "c-1 (uses: Frobnicate)", "internal/c2.go", "", "Fix c-2.")
	usesBrief(t, dir, "c-3", "c-1", "internal/c3.go", "", "Fix c-3.")
	usesBrief(t, dir, "c-4", "zz-9 (uses: Frobnicate)", "internal/c4.go", "", "Fix c-4.")
	code, out, errs := ta.do("brief check --stream s " + dir)
	assert.Equal(t, 1, code, out+errs)
	assert.Contains(t, out, "CHECK "+filepath.Join(dir, "c-3.md")+" card-checks: card=c-3 check=depends-uses")
	assert.Contains(t, out, "card=c-4 check=depends-unknown")
	assert.NotContains(t, out, "card=c-2 check=depends")
	assert.NotContains(t, out, "card=c-1 check=depends")
	assert.Equal(t, 2, strings.Count(out, "check=depends"), "one finding for c-3 and one for c-4; the plan's own findings come with them")
	assert.Contains(t, out, "BRIEF CHECK files=4 findings=")

	// a need already on the table is found there
	ta.ok("add --one --stream s t-1 --brief-file " + usesBrief(t, t.TempDir(), "t-1", "-", "internal/t1.go", "", "Add the Quux verb."))
	one := t.TempDir()
	usesBrief(t, one, "t-2", "t-1 (uses: Quux)", "internal/t2.go", "", "Fix t-2.")
	code, out, errs = ta.do("brief check --stream s " + one)
	assert.Equal(t, 0, code, out+errs)
}

// A text carrying a CARRY: head= line is the card's own with its needs line unchanged (what --widen
// writes), and is not held to the grammar again; the same text without the line is held. With no
// table read, an id the call's briefs lack is not judged, and one they hold is.
func TestDependsFindingsSkipsACarriedTextAndJudgesWhatItCanWithoutATable(t *testing.T) {
	t.Parallel()
	bare := "RESULT: w-2 sha=000000000000 tier: flash\nDEPENDS-ON: w-1\nPATHS: f.go\n\nFix w-2.\n"
	carried := "RESULT: w-2 sha=000000000000 tier: flash\nDEPENDS-ON: w-1\nCARRY: head=0123456789abcdef0123456789abcdef01234567\nPATHS: f.go\n\nFix w-2.\n"
	sib := map[string]string{"w-1": "RESULT: w-1 sha=000000000000 tier: flash\nPATHS: f.go\n\nAdd Frobnicate.\n"}
	assert.Empty(t, dependsFindings(nil, []briefCheck{{id: "w-2", brief: carried, siblings: sib}}))
	got := dependsFindings(nil, []briefCheck{{id: "w-2", brief: bare, siblings: sib}})
	require.Len(t, got, 1)
	assert.Equal(t, "depends-uses", got[0].Check)
	// an id neither the call nor a table holds: left out with no table, a finding with one
	unknown := "RESULT: w-3 sha=000000000000 tier: flash\nDEPENDS-ON: nowhere (uses: Frobnicate)\nPATHS: f.go\n\nFix w-3.\n"
	assert.Empty(t, dependsFindings(nil, []briefCheck{{id: "w-3", brief: unknown}}))
}
