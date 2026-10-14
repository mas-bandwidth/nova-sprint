package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/decide"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
)

// brief check runs every check add runs, over every brief, and collects every
// finding instead of stopping at the first (docs/CLI.md; docs/SPEC-SPRINT.md
// section 7, the add). It reads only: nothing is written and the brief decision
// (briefdecide.go, briefGate) is never asked.
func init() {
	notServed = append(notServed, "brief check")
	verbClasses["brief check"] = classRead
}

// briefFinding is one line brief check prints and one object it carries under
// --json.
type briefFinding struct {
	File  string `json:"file"`
	ID    string `json:"id"`
	Check string `json:"check"`
	Why   string `json:"why"`
	// order is add's position for the check the finding came from, so the
	// findings sort in file order then add's check order.
	order int
}

// briefItem is one brief brief check holds, under the id its file names.
type briefItem struct {
	file, id, text string
	card           sprint.CardAdd
}

// add's check order, as cmdAdd and cmdAddMany run it: the id, the size bound,
// shared PATHS, the card lint, the WHO and base holds, the TLA+ records, the
// card checks, the brief checks at the base, the PATHS admission, the sprint
// branch, then the add step's own plan.
const (
	chkID = iota + 1
	chkSize
	chkShared
	chkLint
	chkWho
	chkBase
	chkTla
	chkCardChecks
	chkBriefBase
	chkPathsAdmit
	chkSprintBase
	chkPlan
)

func (a *app) cmdBriefCheck(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("brief check")
	stream := fs.String("stream", "", "the stream the briefs would be added to: the WHO, base, sprint-branch and plan checks read it")
	rules := fs.String("rules", "", "the rules file the briefs are linted with (else the sprint's, read through the store)")
	allowShared := fs.Bool("allow-shared-paths", false, "do not refuse two briefs that name one PATHS file and need neither the other")
	names, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, "brief check", err.Error())
	}
	if len(names) == 0 {
		return refuse(stderr, "brief check", "wants one or more brief files or directories")
	}
	files, code := expandBriefPaths(names, stderr)
	if code != 0 {
		return code
	}
	fileOrder := map[string]int{}
	for i, p := range files {
		if _, seen := fileOrder[p]; !seen {
			fileOrder[p] = i
		}
	}
	var items []briefItem
	var findings []briefFinding
	for _, p := range files {
		id := strings.TrimSuffix(filepath.Base(p), ".md")
		text, err := readTextFile(p, briefReadCap)
		if err != nil {
			return refuse(stderr, "brief check", fmt.Sprintf("%s: %v", p, err))
		}
		// an empty file is linted, and the lint says it once, naming the file
		briefText := strings.TrimSuffix(text, "\n")
		findings = append(findings, briefIDSizeFindings(p, id, briefText)...)
		cb := swarm.ReadCardBase([]byte(briefText))
		items = append(items, briefItem{file: p, id: id, text: briefText, card: sprint.CardAdd{
			ID: id, Brief: briefText, Needs: uniquify(briefNeeds(briefText)), File: p,
			Base: cb.Ref, Repo: cb.Named,
		}})
	}
	cards := briefCardsOf(items)
	if !*allowShared {
		// sharedPaths is one refusal over the cards, attributed to the first
		// brief it was read with
		if why := sharedPaths(cards); why != "" {
			findings = append(findings, briefFinding{File: items[0].file, ID: items[0].id, Check: "shared-paths", Why: why, order: chkShared})
		}
	}
	// the store is opened read-only: a store that does not answer runs the
	// store-free checks alone and says so
	st, storeErr := a.store(*c)
	var rs ruleSet
	if *rules != "" || storeErr == nil {
		rs, code = a.briefRules("brief check", *rules, c, &st, stderr)
		if code != 0 {
			return code
		}
	} else {
		rs = ruleSet{rules: swarm.DefaultChildRules}
	}
	// the card lint, one buffer per brief as the hold functions are called: a
	// model-line fault in one brief never hides another brief's findings
	for _, card := range cards {
		var lintBuf bytes.Buffer
		lintBriefFiles("brief check", []sprint.CardAdd{card}, rs, c.max, &lintBuf)
		findings = append(findings, lintFindings(lintBuf.String())...)
	}
	if storeErr == nil {
		var storeFindings []briefFinding
		storeFindings, storeErr = a.briefStoreChecks(st, items, *stream, c)
		if storeErr == nil {
			findings = append(findings, storeFindings...)
		}
	}
	if storeErr != nil {
		// the store did not answer: the store-free checks alone run, and the
		// store's checks (who, base, card checks, brief base, sprint base, the
		// plan) are named not run
		findings = append(findings, a.briefStoreFreeChecks(items)...)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if fileOrder[findings[i].File] != fileOrder[findings[j].File] {
			return fileOrder[findings[i].File] < fileOrder[findings[j].File]
		}
		return findings[i].order < findings[j].order
	})
	if c.json {
		out := append([]briefFinding(nil), findings...)
		if storeErr != nil {
			out = append(out, briefFinding{File: "", Check: "store", Why: storeNotReached(storeErr)})
		}
		b, _ := json.Marshal(out)
		fmt.Fprintln(stdout, string(b))
	} else {
		for _, f := range findings {
			fmt.Fprintln(stdout, "CHECK "+oneline.Field(f.File)+" "+f.Check+": "+oneline.Escape(f.Why))
		}
		if storeErr != nil {
			fmt.Fprintln(stdout, "CHECK store: not reached ("+oneline.Escape(storeErr.Error())+"): the store's checks (names, dropped cards, needs, the plan) were not run")
		}
		fmt.Fprintf(stdout, "BRIEF CHECK files=%d findings=%d\n", len(items), len(findings))
	}
	if storeErr != nil {
		if len(findings) > 0 {
			return 1
		}
		return 3
	}
	if len(findings) > 0 {
		return 1
	}
	return 0
}

// briefIDSizeFindings is add's first two per-brief checks, the id its file
// names and the size bound, collected rather than refused: brief check and add
// --partial both hold a brief these fault, so they share the one collector and
// not a second copy (docs/CLI.md; docs/SPEC-SPRINT.md section 7, the add).
func briefIDSizeFindings(file, id, briefText string) []briefFinding {
	var out []briefFinding
	if why := reservedCardID(id); why != "" {
		out = append(out, briefFinding{File: file, ID: id, Check: "id", Why: why, order: chkID})
	} else if !sprint.ValidID(id) {
		out = append(out, briefFinding{File: file, ID: id, Check: "id",
			Why: fmt.Sprintf("%q is not a card id (letters, digits, _ and -): the id is the file's base name without .md", id), order: chkID})
	}
	if len(briefText) > store.MaxBriefBytes {
		out = append(out, briefFinding{File: file, ID: id, Check: "size",
			Why: fmt.Sprintf("the brief is %d bytes, over the %d bytes a brief may be", len(briefText), store.MaxBriefBytes), order: chkSize})
	}
	return out
}

// briefStoreFreeChecks runs the checks that need no store: the TLA+ records
// (holdTlaRecords, through the base tree briefTlaRecordsHold reads) and the
// PATHS admission (holdPathsAdmit), each once per brief with its own buffer as
// stderr.
func (a *app) briefStoreFreeChecks(items []briefItem) []briefFinding {
	var out []briefFinding
	for _, it := range items {
		b := briefCheck{id: it.id, brief: it.text}
		tree, haveTree := a.briefBaseTree(it)
		out = append(out, collectHold(it.file, it.id, "tla-records", chkTla, func(w io.Writer) int {
			return briefTlaRecordsHold(w, b, tree, haveTree)
		})...)
		out = append(out, collectHold(it.file, it.id, "paths-admit", chkPathsAdmit, func(w io.Writer) int {
			return a.holdPathsAdmit("brief check", w, b)
		})...)
	}
	return out
}

// briefTlaRecordsHold is the brief check's tla-records rule once the card's base
// tree is known: the tree was read and holds no tla/CASES.tsv (tlc.CasesFile,
// the file the lander's run-record check reads, internal/sprint/land_records.go),
// so the repository holds no models and the rule does not apply and prints
// nothing; else holdTlaRecords runs unchanged. A base no tree was read for
// (haveTree false) keeps the rule, never a skip for a tree no one read.
func briefTlaRecordsHold(w io.Writer, b briefCheck, tree []string, haveTree bool) int {
	if haveTree && !slices.Contains(tree, "tla/"+tlc.CasesFile) {
		return 0
	}
	return holdTlaRecords("brief check", w, b)
}

// briefBaseTree is the card's base tree as repo-relative paths and whether one was
// read: the lander's clone of its REPO at the BASE tip (admissionTree, the same
// tree add's PATHS admission reads). A brief that names no REPO or no BASE, or
// whose base cannot be read, is no tree, and the caller keeps the tla-records rule
// rather than skip one it never read.
func (a *app) briefBaseTree(it briefItem) ([]string, bool) {
	cb := swarm.ReadCardBase([]byte(it.text))
	if cb.Named == "" || cb.Ref == "" {
		return nil, false
	}
	tree := a.admissionTree(context.Background(), &lander{a: a}, cb)
	if tree.Missing != "" {
		return nil, false
	}
	return tree.Files, true
}

// briefStoreChecks runs every store check add runs, in add's order, each once
// per brief with its own buffer as stderr, then the add step's own plan read
// read-only: the step is never run and nothing is written. The plan's one
// read-only snapshot is loaded first, so a store that does not answer is
// reported as such (the caller runs the store-free checks alone) rather than
// silently dropping the plan-level checks.
func (a *app) briefStoreChecks(st *store.Store, items []briefItem, stream string, c *common) ([]briefFinding, error) {
	// the add step's plan on one read-only snapshot: its Refused list is the
	// plan-level checks (priority, model tier, bench, sprint branch, an id that
	// exists, needs that exist), store.AddStep's Plan, never the step
	r := sprint.AddReq{Stream: stream, Cards: briefCardsOf(items), Who: c.actor}
	step := store.AddStep(r)
	snap, err := st.Load(context.Background(), step.Load, step.Extras)
	if err != nil {
		// the read did not answer: no store check was run
		return nil, err
	}
	var out []briefFinding
	byID := map[string]string{}
	for _, it := range items {
		byID[it.id] = it.file
	}
	for _, it := range items {
		out = append(out, collectHold(it.file, it.id, "who", chkWho, func(w io.Writer) int {
			return a.holdWho("brief check", st, w, []string{stream}, it.text)
		})...)
		out = append(out, collectHold(it.file, it.id, "base", chkBase, func(w io.Writer) int {
			return a.holdBase("brief check", st, false, w, it.text)
		})...)
	}
	out = append(out, a.briefStoreFreeChecks(items)...)
	for _, it := range items {
		b := briefCheck{id: it.id, brief: it.text}
		out = append(out, collectHold(it.file, it.id, "card-checks", chkCardChecks, func(w io.Writer) int {
			return a.holdCardChecks("brief check", st, w, b)
		})...)
		out = append(out, collectHold(it.file, it.id, "brief-base", chkBriefBase, func(w io.Writer) int {
			return a.holdBriefBase("brief check", st, false, w, b)
		})...)
	}
	for _, it := range items {
		r := sprint.AddReq{Stream: stream, Cards: []sprint.CardAdd{it.card}, Who: c.actor}
		out = append(out, collectHold(it.file, it.id, "sprint-base", chkSprintBase, func(w io.Writer) int {
			return a.holdSprintBase("brief check", st, w, r)
		})...)
	}
	for _, ref := range step.Plan(snap).Refused {
		out = append(out, briefFinding{File: byID[ref.Key], ID: ref.Key, Check: "plan", Why: ref.Why, order: chkPlan})
	}
	return out, nil
}

func briefCardsOf(items []briefItem) []sprint.CardAdd {
	out := make([]sprint.CardAdd, len(items))
	for i := range items {
		out[i] = items[i].card
	}
	return out
}

// collectHold runs one hold function with a buffer as its stderr and keeps its
// findings: each LINT DRIFT line, else its one refusal line.
func collectHold(file, id, check string, order int, fn func(io.Writer) int) []briefFinding {
	var buf bytes.Buffer
	fn(&buf)
	return findingsFromBuffer(buf.String(), file, id, check, order)
}

// findingsFromBuffer keeps a hold's LINT DRIFT lines, or, when it printed none,
// its one refusal line. file is the brief the hold was called over.
func findingsFromBuffer(s, file, id, check string, order int) []briefFinding {
	var drifts, refused []briefFinding
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "LINT DRIFT "):
			drifts = append(drifts, briefFinding{File: file, ID: id, Check: check, Why: strings.TrimPrefix(line, "LINT DRIFT "), order: order})
		case strings.Contains(line, "REFUSED: "):
			why := line[strings.Index(line, "REFUSED: ")+len("REFUSED: "):]
			if j := strings.Index(why, "; run: "); j >= 0 {
				why = why[:j]
			}
			refused = append(refused, briefFinding{File: file, ID: id, Check: check, Why: why, order: order})
		}
	}
	if len(drifts) > 0 {
		return drifts
	}
	return refused
}

// lintFindings keeps lintBriefFiles' LINT DRIFT lines, each attributed to the
// brief file the line names. A refusal with no drift line (a model-line why) is
// attributed to the file its refusal names.
func lintFindings(s string) []briefFinding {
	var out []briefFinding
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "LINT DRIFT brief ") {
			continue
		}
		rest := strings.TrimPrefix(line, "LINT DRIFT brief ")
		file, rest, ok := strings.Cut(rest, ": ")
		if !ok {
			continue
		}
		check, _, _ := strings.Cut(rest, ":")
		out = append(out, briefFinding{File: file, Check: check, Why: rest, order: chkLint})
	}
	if len(out) == 0 && strings.Contains(s, "REFUSED: ") {
		line := s[strings.Index(s, "REFUSED: ")+len("REFUSED: "):]
		line = strings.TrimSpace(strings.SplitN(line, "\n", 2)[0])
		if j := strings.Index(line, "; run: "); j >= 0 {
			line = line[:j]
		}
		file := line
		if j := strings.Index(line, ": "); j >= 0 {
			file = line[:j]
		}
		out = append(out, briefFinding{File: file, Check: "lint", Why: line, order: chkLint})
	}
	return out
}

// expandBriefPaths is the files brief check reads: each named file as itself,
// each named directory's *.md files in byte order (decide.CardFilePaths, as
// briefFiles reads them). A file that cannot be read, or a directory with no
// *.md file, is a usage refusal, exit 2.
func expandBriefPaths(names []string, stderr io.Writer) ([]string, int) {
	var files []string
	for _, name := range names {
		fi, err := os.Stat(name)
		if err != nil {
			return nil, refuse(stderr, "brief check", name+": "+err.Error())
		}
		if !fi.IsDir() {
			files = append(files, name)
			continue
		}
		out, err := decide.CardFilePaths(name)
		if err != nil {
			return nil, refuse(stderr, "brief check", name+": "+err.Error())
		}
		if len(out) == 0 {
			return nil, refuse(stderr, "brief check", name+" holds no *.md file")
		}
		files = append(files, out...)
	}
	return files, 0
}

// storeNotReached is the line brief check says when the store did not answer.
func storeNotReached(err error) string {
	return "not reached (" + err.Error() + "): the store's checks (names, dropped cards, needs, the plan) were not run"
}
