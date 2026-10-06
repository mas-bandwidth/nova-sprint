package card

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/internal/hygiene"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// A BRIEF IS WRITTEN FROM ITS PARTS, NEVER FROM A HEREDOC (docs/SPEC-CARD-CONTRACT.md,
// the brief's skeleton; docs/CLI.md, nova-card new). A coordinator who types the child
// header, the six RULES lines, STEP 1 to STEP 6 and the gate line by hand pays for each
// typo with an add refusal or a child that does the wrong thing, and a stranger has no
// template at all. Parts is what only the writer knows (the task, where it lands, what it
// may touch and how it is proved); New fills in every other line the card lint wants, and
// NewLint holds the result to the add's lint before it leaves.

// Parts are what one brief is written from.
type Parts struct {
	ID        string   // the card's id, the brief file's name without .md
	Repo      string   // owner/name
	Base      string   // the branch the card starts from and lands on
	Task      string   // THE TASK paragraph(s), as the writer wrote them
	Paths     []string // the files the card may touch
	Shared    []string // the PATHS entries another card may touch at once
	Test      string   // `<package> <TestName>`, the test the card lands with
	Gate      []string // the packages STEP 4 runs go vet and go test on
	Tier      string   // flash, pro, heavy or frontier
	Needs     []string // the ids of the cards this one waits for
	Kind      string   // the KIND: line; "" is fix-red
	Start     string   // the START: line; "" is the TEST package
	Stop      string   // the STOP: line; "" is the test red before and green after, gate passing
	Libraries string   // the Libraries considered line; "" is DefaultLibraries
	Minutes   int      // the Deadline line; 0 is the tier's (NewDeadline)
	// Rules are a rules file's sentences the RULES paragraph quotes after the default
	// rules (NewRules); nil quotes the default rules alone.
	Rules []swarm.ChildRule
}

// DefaultKind is the KIND: of a brief whose parts name none: one change, red test first.
const DefaultKind = "fix-red"

// DefaultLibraries is the Libraries considered line of a brief whose parts name none.
const DefaultLibraries = "the Go standard library and the modules already in go.mod; no new dependency."

// AttributionSentence closes every brief New writes: the By: line names whoever takes
// the card, and a card is for whoever it is dealt to.
const AttributionSentence = "Every commit body carries a By: line naming you, whoever takes this card, above the Co-Authored-By trailer; never a model name."

// childSentence is the standard child header line, the one the card template carries.
const childSentence = "You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you."

// NewDeadline is the minutes a tier gets when the parts name none.
func NewDeadline(tier string) int {
	switch tier {
	case "frontier", "heavy":
		return 150
	case "pro":
		return 60
	}
	return 45
}

// Missing is the required parts p lacks, by the name the writer gives them: id, repo,
// base, task, paths, test, gate, tier, in that order.
func (p Parts) Missing() []string {
	var out []string
	for _, part := range []struct {
		name  string
		empty bool
	}{
		{"id", strings.TrimSpace(p.ID) == ""},
		{"repo", strings.TrimSpace(p.Repo) == ""},
		{"base", strings.TrimSpace(p.Base) == ""},
		{"task", strings.TrimSpace(p.Task) == ""},
		{"paths", len(p.Paths) == 0},
		{"test", strings.TrimSpace(p.Test) == ""},
		{"gate", len(p.Gate) == 0},
		{"tier", strings.TrimSpace(p.Tier) == ""},
	} {
		if part.empty {
			out = append(out, part.name)
		}
	}
	return out
}

// Invalid is what is wrong with the parts p carries, one sentence each: an id or a need
// that is no card id (sprint.ValidID, what nova-sprint add holds a brief file's name to), a
// tier that is no route, a kind the toolchain does not declare, a TEST that is not `<package> <TestName>`,
// a gate entry that is no package path. Missing parts are Missing's.
func (p Parts) Invalid() []string {
	var out []string
	if p.ID != "" && !sprint.ValidID(p.ID) {
		out = append(out, fmt.Sprintf("id %q is not a card id (letters, digits, _ and -; a dot separates the parts of a card's identity), so nova-sprint add would refuse its file", p.ID))
	}
	for _, n := range p.Needs {
		if !sprint.ValidID(n) {
			out = append(out, fmt.Sprintf("need %q is not a card id (letters, digits, _ and -)", n))
		}
	}
	if p.Tier != "" && !cardhdr.IsRoute(p.Tier) {
		out = append(out, fmt.Sprintf("tier %q is not one of %s", p.Tier, cardhdr.RouteList))
	}
	if p.Kind != "" && !hygiene.KindDeclared(p.Kind) {
		out = append(out, fmt.Sprintf("kind %q is not one of %s", p.Kind, strings.Join(hygiene.Kinds(), ", ")))
	}
	if p.Test != "" {
		if tl, why := cardhdr.ParseTest(p.Test); why != "" {
			out = append(out, why)
		} else if tl.None {
			out = append(out, "TEST "+p.Test+" names no test; a brief from new lands with `<package> <TestName>`, red first")
		}
	}
	for _, g := range p.Gate {
		if gatePackage(g) == "" {
			out = append(out, fmt.Sprintf("gate entry %q is no package path (./cmd/x, internal/y)", g))
		}
	}
	return out
}

// gatePackage is a gate entry as STEP 4 runs it, `./<dir>/`; "" for an entry that is no
// repository-relative package path (absolute, with .., or a ... pattern).
func gatePackage(g string) string {
	g = strings.TrimSpace(g)
	clean := path.Clean(strings.TrimPrefix(g, "./"))
	switch {
	case g == "", strings.HasPrefix(g, "/"), strings.Contains(g, "..."), clean == "..", strings.HasPrefix(clean, "../"):
		return ""
	case clean == ".":
		return "./"
	}
	return "./" + clean + "/"
}

// New is the brief p's parts write: the contract line naming the tier, the typed header
// (REPO, BASE, KIND, DEPENDS-ON, START, STOP, PATHS, SHARED, TEST), the standard child
// header and Deadline line, the RULES paragraph (NewRules) verbatim, THE TASK
// and its Libraries considered line, STEP 1 to STEP 6 with the gate built from p.Gate,
// and the attribution sentence. An error names the missing and invalid parts; a brief
// New returns is held to NewLint by its caller before it leaves.
func New(p Parts) (string, error) {
	if missing := p.Missing(); len(missing) > 0 {
		return "", fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	if bad := p.Invalid(); len(bad) > 0 {
		return "", fmt.Errorf("%s", strings.Join(bad, "; "))
	}
	tl, _ := cardhdr.ParseTest(p.Test)
	pkg := strings.Trim(strings.TrimPrefix(tl.Package, "./"), "/")
	kind := cmpOr(p.Kind, DefaultKind)
	start := cmpOr(p.Start, pkg)
	stop := cmpOr(p.Stop, "the test "+tl.Name+" is red before the change and green after it, and the STEP 4 gate passes")
	libraries := cmpOr(strings.TrimSpace(p.Libraries), DefaultLibraries)
	minutes := p.Minutes
	if minutes <= 0 {
		minutes = NewDeadline(p.Tier)
	}
	needs := "-"
	if len(p.Needs) > 0 {
		needs = strings.Join(p.Needs, ", ")
	}
	var gate []string
	for _, g := range p.Gate {
		if g = gatePackage(g); !slices.Contains(gate, g) {
			gate = append(gate, g)
		}
	}
	gates := strings.Join(gate, " ")
	task := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(p.Task), "THE TASK."))

	var b strings.Builder
	fmt.Fprintf(&b, "RESULT: %s tier: %s\n", p.ID, p.Tier)
	fmt.Fprintf(&b, "REPO: %s\n", p.Repo)
	fmt.Fprintf(&b, "BASE: %s\n", p.Base)
	fmt.Fprintf(&b, "KIND: %s\n", kind)
	fmt.Fprintf(&b, "DEPENDS-ON: %s\n", needs)
	fmt.Fprintf(&b, "START: %s\n", start)
	fmt.Fprintf(&b, "STOP: %s\n", stop)
	fmt.Fprintf(&b, "PATHS: %s\n", strings.Join(p.Paths, ", "))
	if len(p.Shared) > 0 {
		fmt.Fprintf(&b, "SHARED: %s\n", strings.Join(p.Shared, ", "))
	}
	fmt.Fprintf(&b, "TEST: %s\n", strings.Join(strings.Fields(p.Test), " "))
	b.WriteString(childSentence + "\n")
	fmt.Fprintf(&b, "Deadline: finish within %d minutes.\n\n", minutes)
	b.WriteString(swarm.RulesParagraph(NewRules(p.Rules)))
	fmt.Fprintf(&b, "\nTHE TASK. %s\n", task)
	fmt.Fprintf(&b, "Libraries considered: %s\n\n", libraries)
	b.WriteString("STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.\n")
	fmt.Fprintf(&b, "STEP 2. Write the red test first, %s in ./%s/, opening with t.Parallel(); no test sleeps on the wall clock. Run go test -count=1 -timeout 600s ./%s/ -run %s and keep the failing line.\n", tl.Name, pkg, pkg, tl.Name)
	b.WriteString("STEP 3. Make it pass in the files PATHS names, and only those; document it where PATHS names. A change any other file needs goes in the report as a proposed diff, never a commit.\n")
	fmt.Fprintf(&b, "STEP 4. Run the gate: go vet and go test -count=1 -timeout 600s on %s, and read the last line of each. %s\n", gates, swarm.GateNamesWhoseFile)
	fmt.Fprintf(&b, "STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6), and the member makes both, against %s, from outside the wall when the card finishes. The pull request body states the diff stat, what was deleted, the tests with what each pins, and what was not done.\n", p.Base)
	b.WriteString("STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): where JOB.md ends the card with its pull request, that is the end and there is nothing else to write, the gate's lines in the pull request body; where it asks for RESULT.md, write it in JOB.md's shape (head, branch, verdict, gate, output, report).\n\n")
	b.WriteString(AttributionSentence + "\n")
	return b.String(), nil
}

// NewRules is the rule set the RULES paragraph of a brief from New quotes: the default
// rules, then each rule of file whose sentence they do not already carry. So the brief
// passes the add under the default rules and under the rules file both (nova-sprint add
// --rules, or the file init --rules recorded, carried by a brief whose repository the
// members hold no file for).
func NewRules(file []swarm.ChildRule) []swarm.ChildRule {
	out := slices.Clone(swarm.DefaultChildRules)
	for _, r := range file {
		if !slices.ContainsFunc(out, func(d swarm.ChildRule) bool { return d.Sentence == r.Sentence }) {
			out = append(out, r)
		}
	}
	return out
}

// NewLint is everything a brief from New is held to before it leaves: Lint under o (the
// add's model lines, default child rules, typed header and tree steps, the template's
// placeholders, the card checks); the child rules of the held rules file by reference
// (swarm.LintCardChildByReference over swarm.DefaultRulesName), the set a member injects
// at stage time, with its go test timeout scan and its Libraries considered check; and,
// given a rules file's rules, those rules carried. A rule's sentence is quoted verbatim,
// so an angle-bracket word inside the RULES paragraph is the rule's own and no
// placeholder to fill.
func NewLint(id, brief string, o Options, rules []swarm.ChildRule) []cardgen.LintFinding {
	var out []cardgen.LintFinding
	inRules := rulesLines(brief)
	for _, f := range Lint(id, brief, o) {
		if f.Check == "placeholder" && inRules[f.Line] {
			continue
		}
		out = append(out, f)
	}
	held, err := swarm.HeldRules(swarm.DefaultRulesName)
	if err != nil { // this build holds the file (swarm.TestTheHeldRulesAreTheFleetFile)
		return append(out, cardgen.LintFinding{ID: id, Check: "held-rules", Line: 1, Excerpt: err.Error()})
	}
	found := swarm.LintCardChildByReference([]byte(brief), held)
	if len(rules) > 0 {
		found = append(found, swarm.LintCardChildWith([]byte(brief), rules)...)
	}
	for _, f := range found {
		out = append(out, cardgen.LintFinding{ID: id, Check: f.Check, Line: f.Line, Excerpt: f.Excerpt})
	}
	return out
}

// rulesLines is the 1-based lines of a brief's RULES paragraph: its `RULES.` line to the
// first blank line.
func rulesLines(brief string) map[int]bool {
	out := map[int]bool{}
	in := false
	for i, line := range strings.Split(brief, "\n") {
		switch {
		case line == "RULES.":
			in = true
		case strings.TrimSpace(line) == "":
			in = false
		}
		if in {
			out[i+1] = true
		}
	}
	return out
}

func cmpOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}
