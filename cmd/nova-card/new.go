package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/card"
	"github.com/mas-bandwidth/nova-sprint/internal/nsprint/verbflag"
	"github.com/mas-bandwidth/nova-sprint/internal/oneline"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// newFlags are the parts of one brief as flags, and the lint inputs; the batch form
// reads the same parts off a TSV row (batchColumns).
type newFlags struct {
	repo, base, taskFile, test, tier, kind, start, stop, libraries, rules *string
	paths, shared, gate, needs                                            *string
	minutes                                                               *int
}

// batchColumns are the columns of a --batch TSV, in order; a first row whose first cell
// is "id" is the header.
var batchColumns = []string{"id", "repo", "base", "task-file", "paths", "shared", "test", "gate", "tier", "needs"}

// partFlag is the flag (or argument) each part of card.Parts is given by.
var partFlag = map[string]string{"id": "<id>", "repo": "--repo", "base": "--base", "task": "--task-file", "paths": "--paths", "test": "--test", "gate": "--gate", "tier": "--tier"}

// cmdNew writes one brief from its parts to stdout or --out, or one brief per row of
// --batch into the directory --out names; every brief is held to card.NewLint first and
// nothing is written while one is red (docs/CLI.md, nova-card new).
func cmdNew(args []string, stdout, stderr io.Writer) int {
	fs := verbflag.New("new")
	f := newFlags{
		repo:      fs.String("repo", "", "the `owner/name` the REPO: line carries"),
		base:      fs.String("base", "", "the `branch` the BASE: line carries: the card starts from it and lands on it"),
		taskFile:  fs.String("task-file", "", "the `file` whose text is THE TASK paragraph"),
		paths:     fs.String("paths", "", "the files the card may touch, comma separated `globs` (the PATHS: line)"),
		shared:    fs.String("shared", "", "the PATHS entries another card may touch at once, comma separated `globs` (the SHARED: line)"),
		test:      fs.String("test", "", "the test the card lands with, `\"<package> <TestName>\"` (the TEST: line)"),
		gate:      fs.String("gate", "", "the `packages` STEP 4 runs go vet and go test on, comma separated"),
		tier:      fs.String("tier", "", "flash, pro, heavy or frontier: line 1's tier"),
		needs:     fs.String("needs", "", "the `ids` of the cards this one waits for, comma separated (the DEPENDS-ON: line)"),
		kind:      fs.String("kind", "", "the KIND: line (default "+card.DefaultKind+")"),
		start:     fs.String("start", "", "the START: line (default: the TEST package)"),
		stop:      fs.String("stop", "", "the STOP: line (default: the test red before the change and green after it, the gate passing)"),
		libraries: fs.String("libraries", "", "the Libraries considered line (default: \""+card.DefaultLibraries+"\")"),
		rules:     fs.String("rules", "", "a child rules `file` (one sentence per line, nova-sprint add --rules): the RULES paragraph quotes its rules after the default ones, and the brief is held to it"),
		minutes:   fs.Int("minutes", 0, "the Deadline line's `minutes` (default: 45 flash, 60 pro, 150 heavy or frontier)"),
	}
	batch := fs.String("batch", "", "a TSV `file`, one brief per row ("+strings.Join(batchColumns, ", ")+"; - for none; a task-file relative to the TSV's directory), written into --out")
	out := fs.String("out", "", "with one brief: the `file` it is written to (default stdout); with --batch: the directory the briefs are written into")
	opts := lintFlags(fs)
	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if err := verbflag.Parse(fs, args); err != nil {
		return refuse(stderr, "new", verbflag.Explain(fs, err))
	}
	if fs.NArg() > 0 {
		return refuse(stderr, "new", fmt.Sprintf("unexpected argument %q; the id comes first, every part is a flag", fs.Arg(0)))
	}
	var rules []swarm.ChildRule
	if *f.rules != "" {
		var err error
		if rules, err = swarm.ReadChildRules(*f.rules); err != nil {
			return refuse(stderr, "new", "--rules: "+err.Error())
		}
	}
	if *batch != "" {
		if id != "" {
			return refuse(stderr, "new", "--batch takes no <id>: each row names its own")
		}
		return newBatch(fs, *batch, *out, rules, opts(), stdout, stderr)
	}
	p, why := f.parts(id)
	if why != "" {
		return refuse(stderr, "new", why)
	}
	p.Rules = rules
	brief, why := writeBrief(p, opts(), stdout)
	switch {
	case why == "red":
		fmt.Fprintf(stderr, "nova-card new FAILED: the brief of %s is red, named on the lines above; nothing written\n", oneline.Field(p.ID))
		return 1
	case why != "":
		return refuse(stderr, "new", why)
	case *out == "":
		fmt.Fprint(stdout, brief)
		return 0
	}
	if err := os.WriteFile(*out, []byte(brief), 0o644); err != nil {
		return refuse(stderr, "new", "cannot write --out "+*out+": "+err.Error())
	}
	fmt.Fprintf(stdout, "CARD OK file=%s\n", oneline.Field(*out))
	return 0
}

// parts are the flags' card.Parts, the task read from --task-file; why names what is
// missing or cannot be read.
func (f newFlags) parts(id string) (card.Parts, string) {
	p := card.Parts{
		ID: id, Repo: *f.repo, Base: *f.base, Test: *f.test, Tier: *f.tier, Kind: *f.kind,
		Start: *f.start, Stop: *f.stop, Libraries: *f.libraries, Minutes: *f.minutes,
		Paths: list(*f.paths), Shared: list(*f.shared), Gate: list(*f.gate), Needs: list(*f.needs),
	}
	return readTask(p, *f.taskFile, "")
}

// readTask fills p.Task from file (relative to dir when dir is given); a missing part
// is named first, by its flag, so a writer with no --task-file hears that and not a
// read error.
func readTask(p card.Parts, file, dir string) (card.Parts, string) {
	if file != "" {
		if dir != "" && !filepath.IsAbs(file) {
			file = filepath.Join(dir, file)
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return p, "cannot read --task-file " + file + ": " + err.Error()
		}
		p.Task = string(raw)
	}
	if missing := p.Missing(); len(missing) > 0 {
		var flags []string
		for _, m := range missing {
			flags = append(flags, partFlag[m])
		}
		if file != "" && p.Task == "" {
			flags = append(flags, "a --task-file with text in it")
		}
		return p, "missing " + strings.Join(flags, ", ") + "; a brief is written from <id> --repo --base --task-file --paths --test --gate --tier"
	}
	return p, ""
}

// writeBrief is p's brief, held to card.NewLint: why is "red" when a LINT DRIFT line was
// printed on stdout, or the refusal of parts New cannot write.
func writeBrief(p card.Parts, o card.Options, stdout io.Writer) (brief, why string) {
	brief, err := card.New(p)
	if err != nil {
		return "", err.Error()
	}
	red := card.NewLint(p.ID, brief, o, p.Rules)
	for _, f := range red {
		fmt.Fprintln(stdout, oneline.Escape(f.String()))
	}
	if len(red) > 0 {
		return "", "red"
	}
	return brief, ""
}

// newBatch writes one brief per row of the TSV file into dir; one red or incomplete row
// refuses the whole batch and nothing is written. A part given as a flag beside --batch
// is refused: a row is the whole of its card; --rules holds for every row.
func newBatch(fs *flag.FlagSet, file, dir string, rules []swarm.ChildRule, o card.Options, stdout, stderr io.Writer) int {
	if dir == "" {
		return refuse(stderr, "new", "--batch wants --out <dir>, the directory the briefs are written into")
	}
	given := ""
	fs.Visit(func(fl *flag.Flag) {
		if given == "" && fl.Name != "batch" && fl.Name != "out" && fl.Name != "rules" && fl.Name != "name" && fl.Name != "dropped" {
			given = fl.Name
		}
	})
	if given != "" {
		return refuse(stderr, "new", "--"+given+" with --batch: each row of the TSV carries every part of its card")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return refuse(stderr, "new", "cannot read --batch "+file+": "+err.Error())
	}
	var ids, briefs []string
	red := 0
	for n, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		cells := strings.Split(line, "\t")
		if strings.TrimSpace(line) == "" || n == 0 && strings.TrimSpace(cells[0]) == "id" {
			continue
		}
		if len(cells) != len(batchColumns) {
			return refuse(stderr, "new", fmt.Sprintf("row %d has %d cells; want %d: %s", n+1, len(cells), len(batchColumns), strings.Join(batchColumns, ", ")))
		}
		cell := func(i int) string {
			if v := strings.TrimSpace(cells[i]); v != "-" {
				return v
			}
			return ""
		}
		p := card.Parts{ID: cell(0), Repo: cell(1), Base: cell(2), Paths: list(cell(4)), Shared: list(cell(5)),
			Test: cell(6), Gate: list(cell(7)), Tier: cell(8), Needs: list(cell(9)), Rules: rules}
		p, why := readTask(p, cell(3), filepath.Dir(file))
		if why == "" {
			var brief string
			if brief, why = writeBrief(p, o, stdout); why == "" {
				ids, briefs = append(ids, p.ID), append(briefs, brief)
				continue
			}
		}
		if why == "red" {
			red++
			continue
		}
		return refuse(stderr, "new", fmt.Sprintf("row %d (%s): %s", n+1, cmp(p.ID, "no id"), why))
	}
	if red > 0 {
		fmt.Fprintf(stderr, "nova-card new FAILED: %d red brief(s) above; nothing written to %s\n", red, oneline.Field(dir))
		return 1
	}
	if len(briefs) == 0 {
		return refuse(stderr, "new", "--batch "+file+" holds no row")
	}
	for i, id := range ids {
		for _, other := range ids[:i] {
			if other == id {
				return refuse(stderr, "new", fmt.Sprintf("card %s is named on two rows; a card is its file %s.md and a second one would overwrite it", id, id))
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return refuse(stderr, "new", "cannot create --out "+dir+": "+err.Error())
	}
	if held, _ := filepath.Glob(filepath.Join(dir, "*.md")); len(held) > 0 {
		return refuse(stderr, "new", fmt.Sprintf("--out %s already holds %d brief(s); name an empty directory", dir, len(held)))
	}
	for i, id := range ids {
		if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(briefs[i]), 0o644); err != nil {
			return refuse(stderr, "new", "cannot write "+id+".md: "+err.Error())
		}
	}
	fmt.Fprintf(stdout, "CARDS OK dir=%s cards=%d\n", oneline.Field(dir), len(ids))
	return 0
}

// list is a comma separated flag or cell, blanks dropped.
func list(v string) []string { return splitList(multi{v}) }
