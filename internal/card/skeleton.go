package card

import (
	"fmt"
	"os"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// Skeleton is the parts of one hand-written brief: what nova-card new reads off
// its flags, or off one row of a --batch table.
type Skeleton struct {
	ID      string
	Repo    string // owner/name
	Base    string // the branch
	Task    string // THE TASK.'s text
	Paths   string
	Shared  string // "" or "-" writes no SHARED line
	Test    string // `<package> <TestName>` or `none <why>`
	Gate    string // the packages STEP 4 vets and tests, space separated
	Tier    string
	Needs   string // the DEPENDS-ON value; "" is "-"
	Kind    string // "" is fix-red
	Start   string // "" is the first PATHS entry and the test's package
	Stop    string // "" is the test red before and green after
	Minutes int    // 0 takes the tier's default
}

// Missing is the parts a brief cannot be written without, as the flags that
// carry them, in flag order; empty means every part is there.
func (s Skeleton) Missing() []string {
	var out []string
	for _, p := range []struct{ flag, v string }{
		{"<id>", s.ID}, {"--repo", s.Repo}, {"--base", s.Base}, {"--task-file", s.Task},
		{"--paths", s.Paths}, {"--test", s.Test}, {"--gate", s.Gate}, {"--tier", s.Tier},
	} {
		if strings.TrimSpace(p.v) == "" {
			out = append(out, p.flag)
		}
	}
	return out
}

// Check is the first part that is present and wrong, or "": a tier that is not a
// route, a TEST line the add would refuse, a gate that is a command and not packages.
func (s Skeleton) Check() string {
	if !cardhdr.IsRoute(s.Tier) {
		return fmt.Sprintf("--tier %q; want %s", s.Tier, cardhdr.RouteList)
	}
	if _, why := cardhdr.ParseTest(s.Test); why != "" {
		return "--test: " + why
	}
	if f := strings.Fields(s.Gate); len(f) > 0 && f[0] == "go" {
		return fmt.Sprintf("--gate %q is a command; name the packages it vets and tests, e.g. --gate './cmd/x/ ./internal/x/'", s.Gate)
	}
	return ""
}

// RenderSkeleton writes the brief: the header nova-sprint add reads, the child
// paragraph, the rules, the attribution sentence, the task, and STEP 1 to STEP 6
// in the shape cardgen.Render writes, so the two generators do not drift. The
// gate is go vet and go test over Gate, plus gofmt on every changed Go file.
func RenderSkeleton(s Skeleton) string {
	minutes := s.Minutes
	if minutes == 0 {
		minutes = cardgen.Deadline(s.Tier)
	}
	kind := nonEmpty(s.Kind, "fix-red")
	needs := nonEmpty(s.Needs, "-")
	pkgs := strings.Join(strings.Fields(s.Gate), " ")
	vet := "go vet " + pkgs
	test := "go test -count=1 -timeout 600s " + pkgs
	tl, _ := cardhdr.ParseTest(s.Test)
	start := s.Start
	if start == "" {
		start = strings.TrimSpace(strings.Split(s.Paths, ",")[0])
		if tl.Package != "" && tl.Package != start {
			start += ", " + tl.Package
		}
	}
	stop := s.Stop
	if stop == "" {
		if tl.None {
			stop = "the task is done and the STEP 4 gate passes"
		} else {
			stop = fmt.Sprintf("the test %s is red before the change and green after it, and the STEP 4 gate passes", tl.Name)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "RESULT: %s tier: %s\n", s.ID, s.Tier)
	fmt.Fprintf(&b, "REPO: %s\n", s.Repo)
	fmt.Fprintf(&b, "BASE: %s\n", s.Base)
	fmt.Fprintf(&b, "KIND: %s\n", kind)
	fmt.Fprintf(&b, "DEPENDS-ON: %s\n", needs)
	fmt.Fprintf(&b, "PATHS: %s\n", s.Paths)
	if sh := strings.TrimSpace(s.Shared); sh != "" && sh != "-" {
		fmt.Fprintf(&b, "SHARED: %s\n", sh)
	}
	fmt.Fprintf(&b, "TEST: %s\n", s.Test)
	fmt.Fprintf(&b, "START: %s\n", start)
	fmt.Fprintf(&b, "STOP: %s\n", stop)
	fmt.Fprintf(&b, "Deadline: finish within %d minutes.\n", minutes)
	b.WriteString("You are a child of the coordinator: one task, one staged checkout, one branch, unattended. This card is the whole task. Read $JOB/JOB.md first. Start at the current BASE tip. One change, one test that is red before and green after.\n")
	b.WriteString("Libraries considered: the Go standard library and testify, already in the tree; the package's own seams and helpers; no new dependency, and no helper over thirty lines without first searching the package for one.\n\n")
	b.WriteString(swarm.ChildRulesParagraph())
	b.WriteString("\n")
	b.WriteString("ATTRIBUTION. Commits and reports name the model and harness actually running; never claim one you are not running, and write a Co-Authored-By trailer only for the model that is.\n\n")
	fmt.Fprintf(&b, "THE TASK. %s The files this card may touch are its PATHS line and no other, in the staged checkout JOB.md names, on the card's own branch, from BASE %s.\n\n", strings.TrimSpace(s.Task), s.Base)
	b.WriteString("STEP 1. Enter the staged checkout JOB.md names with cd $JOB/repo && git log --oneline -1, no clone; work only on its own branch. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set to the machine's shared build cache (JOB.md names it): keep it. Scratch belongs under $JOB/scratch.\n")
	if tl.None {
		fmt.Fprintf(&b, "STEP 2. The card has no test (%s); say in the report what shows the change works.\n", tl.Why)
	} else {
		fmt.Fprintf(&b, "STEP 2. Make it red first, as the task says, with the test %s: run go test -count=1 -timeout 600s ./%s/ -run %s and keep the failing line as evidence.\n", tl.Name, strings.TrimPrefix(tl.Package, "./"), tl.Name)
	}
	b.WriteString("STEP 3. Make it pass in the files this card names, and only those. Commit the draft on your own branch as soon as the test is green, before any further probe; a later commit may refine it. A change any other file needs goes in the report as a proposed diff, never a commit.\n")
	fmt.Fprintf(&b, "STEP 4. Run the gate: %s and %s, and read the last line of each. Run gofmt -l on every changed Go file; it must print nothing. %s\n", vet, test, swarm.GateNamesWhoseFile)
	fmt.Fprintf(&b, "STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6), and the member makes both, against %s, from outside the wall when the card finishes. The pull request body states the diff stat, what was deleted, the tests with what each pins, and what was not done.\n", s.Base)
	b.WriteString("STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): where JOB.md ends the card with its pull request, that is the end and there is nothing else to write, the gate's lines in the pull request body; where it asks for RESULT.md, write it in JOB.md's shape (head, branch, verdict, gate, output, report).\n")
	return b.String()
}

// BatchRow is one card read off a --batch table, and how its task was read:
// "task_file <path>", "task", or for a table with no header "column 2 file <path>"
// or "column 2 inline", so the line the verb prints says which.
type BatchRow struct {
	Skeleton
	TaskFrom string
}

// batchColumns is each part's header names; the first is the documented one.
var batchColumns = map[string][]string{
	"id":        {"id", "card", "card_id"},
	"repo":      {"repo"},
	"base":      {"base"},
	"task_file": {"task_file"},
	"task":      {"task"},
	"paths":     {"paths"},
	"shared":    {"shared"},
	"test":      {"test"},
	"gate":      {"gate"},
	"tier":      {"tier"},
	"needs":     {"needs", "depends_on"},
}

// ParseBatch reads a --batch table, tab separated; blank lines and lines opening
// with # are skipped. A first row whose first cell is id (or card, card_id) is a
// header and names the columns; a task_file cell is a file read as the task, and
// one that cannot be read refuses, naming the row and the path; a task cell is the
// task's text. With no header the columns are id, task, paths, test, gate, tier,
// shared, needs, and the task cell is read as a file when one is there and as the
// text otherwise. A row's empty cell takes def's part. Readfile reads a task file.
func ParseBatch(text string, def Skeleton, readFile func(string) ([]byte, error)) ([]BatchRow, error) {
	if readFile == nil {
		readFile = os.ReadFile
	}
	var rows []BatchRow
	var col map[string]int
	first := true
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		if t := strings.TrimSpace(line); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		cells := strings.Split(line, "\t")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		row := n + 1
		if first {
			first = false
			if isIDHeader(cells[0]) {
				var err error
				if col, err = headerColumns(cells, row); err != nil {
					return nil, err
				}
				continue
			}
		}
		r := BatchRow{Skeleton: def}
		set := func(dst *string, v string) {
			if v != "" {
				*dst = v
			}
		}
		if col != nil {
			cell := func(name string) string {
				if i, ok := col[name]; ok && i < len(cells) {
					return cells[i]
				}
				return ""
			}
			set(&r.ID, cell("id"))
			set(&r.Repo, cell("repo"))
			set(&r.Base, cell("base"))
			tf, task := cell("task_file"), cell("task")
			switch {
			case tf != "" && task != "":
				return nil, fmt.Errorf("row %d (%s): both task_file and task are filled; fill one", row, r.ID)
			case tf != "":
				raw, err := readFile(tf)
				if err != nil {
					return nil, fmt.Errorf("row %d (%s): cannot read task_file %s: %v", row, r.ID, tf, err)
				}
				r.Task, r.TaskFrom = string(raw), "task_file "+tf
			case task != "":
				r.Task, r.TaskFrom = task, "task"
			}
			set(&r.Paths, cell("paths"))
			set(&r.Shared, cell("shared"))
			set(&r.Test, cell("test"))
			set(&r.Gate, cell("gate"))
			set(&r.Tier, cell("tier"))
			set(&r.Needs, cell("needs"))
		} else {
			at := func(i int) string {
				if i < len(cells) {
					return cells[i]
				}
				return ""
			}
			set(&r.ID, at(0))
			if v := at(1); v != "" {
				if raw, err := readFile(v); err == nil {
					r.Task, r.TaskFrom = string(raw), "column 2 file "+v
				} else {
					r.Task, r.TaskFrom = v, "column 2 inline"
				}
			}
			set(&r.Paths, at(2))
			set(&r.Test, at(3))
			set(&r.Gate, at(4))
			set(&r.Tier, at(5))
			set(&r.Shared, at(6))
			set(&r.Needs, at(7))
		}
		if r.ID == "" {
			return nil, fmt.Errorf("row %d: no card id", row)
		}
		rows = append(rows, r)
	}
	return rows, nil
}

func isIDHeader(cell string) bool {
	for _, name := range batchColumns["id"] {
		if strings.EqualFold(cell, name) {
			return true
		}
	}
	return false
}

// headerColumns maps each part to its column, and refuses a column it does not
// know, so a misspelt header is never a part silently left at its default.
func headerColumns(cells []string, row int) (map[string]int, error) {
	col := map[string]int{}
	for i, c := range cells {
		name := strings.ToLower(strings.ReplaceAll(c, "-", "_"))
		part := ""
		for p, names := range batchColumns {
			for _, n := range names {
				if n == name {
					part = p
				}
			}
		}
		if part == "" {
			return nil, fmt.Errorf("row %d: header column %q is not one of id, repo, base, task_file, task, paths, shared, test, gate, tier, needs", row, c)
		}
		col[part] = i
	}
	return col, nil
}
