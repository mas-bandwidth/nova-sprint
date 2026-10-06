package sprint

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"maps"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/cardcost"
	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/internal/ci/shrinkonly"
	"github.com/mas-bandwidth/nova-sprint/internal/diffcheck"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// The work lint (docs/SPEC-SPRINT.md section 6, the work lint): every finished attempt is
// held to the checks decidable from its commit alone, with no model, before any reader is
// asked of it. Measured on epoch 15's log, these classes were 985 of the 3,734 findings a
// reader was paid to make. An attempt that fails is reworked at once with the findings
// (file:line, the token and its remedy) as its fix, and asked of no reader; the lint
// rework counts toward the card's bounds as a broken read does (broken_reads, the brief's
// bound by the finding, and the attempt cap by the attempt it deals).

// The work lint's tokens, each one check (WorkLintRules).
const (
	LintNoHead       = "no-head"
	LintEmpty        = "empty-diff"
	LintPaths        = "outside-paths"
	LintLedger       = "ledger-grows"
	LintTestAbsent   = "test-absent"
	LintGofmt        = "gofmt"
	LintTrailer      = "trailer-usage"
	LintMerge        = "merge-conflict"
	LintVerbDryRun   = "verb-dry-run"
	LintTestClock    = "test-clock"
	FieldLintReworks = "lint_reworks" // the primary's count of attempts the work lint sent back
)

// WorkLintRule is one check of the work lint: its token, what it refuses, and the remedy
// the rework's fix carries.
type WorkLintRule struct {
	Token, Refuses, Remedy string
}

// WorkLintRules is every check of the work lint, in the order it runs them.
var WorkLintRules = []WorkLintRule{
	{LintNoHead, "the finish names no pushed head, or a head the repository does not have", "commit the change, push the branch the brief's STATUS line names, and finish with that head"},
	{LintEmpty, "the diff from the merge-base with the base tip to the head is empty", "make the change the brief asks for and commit it; an attempt with nothing to change says so in its report and finishes --failed"},
	{LintPaths, "the diff changes a file outside the brief's PATHS (the lander's E12, scope amendments allowed)", "take the change out of the file, or HOLD naming the file the card needs"},
	{LintLedger, "the diff adds a row to a shrink-only ledger (internal/ci/shrinkonly: a ledger may only shrink)", "fix the code the class test names instead of listing it; the ledger only loses rows"},
	{LintTestAbsent, "the test the brief's TEST line names is not a func in that package's _test.go files at the head", "write the test under the name the TEST line gives, in the package it names"},
	{LintGofmt, "a changed .go file is not gofmt-clean at the head", "run gofmt -w on the file and commit it"},
	{LintTrailer, "a commit's Co-Authored-By or By line names a model or harness other than the one the attempt's usage records", "name the run's own model in the trailer, and yourself, never a model, on the By line"},
	{LintMerge, "the head does not merge onto the base tip (git merge-tree)", "merge the base tip into the branch, resolve the conflicts, and push"},
	{LintVerbDryRun, "a new or changed verb that writes has no --dry-run in its syntax", "give the verb a --dry-run that says what it would write and writes nothing"},
	{LintTestClock, "an added _test.go line calls time.Sleep or time.Now outside a synctest bubble", "run the test in synctest.Test, or inject the clock"},
}

// WorkLintRuleOf is the rule of a token; ok is false for a token no check has.
func WorkLintRuleOf(tok string) (WorkLintRule, bool) {
	i := slices.IndexFunc(WorkLintRules, func(r WorkLintRule) bool { return r.Token == tok })
	if i < 0 {
		return WorkLintRule{}, false
	}
	return WorkLintRules[i], true
}

// LintFinding is one place the work lint refuses: the file and line (0 for the attempt as
// a whole), the token, and what it found.
type LintFinding struct {
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
	Token string `json:"token"`
	What  string `json:"what"`
}

// String is the finding as a rework's fix carries it: file:line, the token, what, and the
// token's remedy.
func (f LintFinding) String() string {
	at := f.File
	if at != "" && f.Line > 0 {
		at += ":" + strconv.Itoa(f.Line)
	}
	if at != "" {
		at += ": "
	}
	out := at + f.Token + ": " + f.What
	if r, ok := WorkLintRuleOf(f.Token); ok {
		out += " (remedy: " + r.Remedy + ")"
	}
	return out
}

// LintFinds is the findings as one finding, the same words for the same findings at any
// attempt (the brief's bound compares them: SameFinding).
func LintFinds(fs []LintFinding) string {
	parts := make([]string, len(fs))
	for i, f := range fs {
		parts[i] = f.String()
	}
	return "work lint: " + strings.Join(parts, "; ")
}

// LintFix is the findings as one fix: the attempt and head the work lint refused, then
// each finding.
func LintFix(attempt int, head string, fs []LintFinding) string {
	return "the work lint refused attempt " + itoa(attempt) + " at " + orDash(head) + "; " + LintFinds(fs)
}

// WorkView is what the work lint reads of one finished attempt, gathered from its
// repository by the caller (worklint_git.go, ReadWorkView): the facts alone, so every
// judgment here is a pure function of them.
type WorkView struct {
	// Head is the attempt's head; Pushed says the repository has it as a commit.
	Head   string
	Pushed bool
	// Diff is the unified diff from the merge-base of the base tip and the head to the
	// head (git diff -M), Tracked the paths the merge-base tracks (nil: unknown).
	Diff    string
	Tracked []string
	// Messages is the body of every commit from the merge-base to the head.
	Messages []string
	// Conflicts is the files git merge-tree found conflicted merging the head onto the
	// base tip; nil merges clean.
	Conflicts []string
	// Show reads a file at the head; Ls the files of a directory at the head.
	Show func(p string) ([]byte, bool)
	Ls   func(dir string) []string
}

// WorkLintInput is the attempt the lint judges besides its view: the brief (PATHS, TEST),
// and the usage of its work card (the model and harness it ran on).
type WorkLintInput struct {
	Brief   string
	Model   string // provider/model the usage records, else the work card's model
	Harness string // the work card's harness, from its route; "" is not recorded
}

// LintInputOf is the lint's input for a primary: its brief, and its work card's usage.
func LintInputOf(s *Snapshot, pr *Card) WorkLintInput {
	in := WorkLintInput{Brief: pr.F("brief")}
	var wc *Card
	if s.Fleet != nil {
		wc = s.Fleet.Card(pr.F("work"))
	}
	if wc != nil {
		in.Model = cardcost.ParseUsage(wc.F(FieldUsage)).Model
		if in.Model == "" {
			in.Model = wc.F(FieldModel)
		}
		in.Harness = wc.F(FieldHarness)
	}
	return in
}

// WorkLint is the work lint's findings for one attempt, in the order of WorkLintRules; nil
// passes. A head that is not there stops the lint at its first finding: nothing else can
// be read of it.
func WorkLint(in WorkLintInput, v WorkView) []LintFinding {
	if v.Head == "" || !v.Pushed {
		return []LintFinding{{Token: LintNoHead, What: "no pushed commit " + orDash(v.Head)}}
	}
	files := diffcheck.Parse(v.Diff)
	if strings.TrimSpace(v.Diff) == "" || len(files) == 0 {
		return []LintFinding{{Token: LintEmpty, What: "the head " + v.Head + " changes nothing from its merge-base with the base"}}
	}
	var out []LintFinding
	// outside PATHS, as the lander holds it (land.go: diffcheck.Outside, ScopeAmended)
	paths := swarm.CardPaths([]byte(in.Brief))
	var changed []string
	for _, f := range files {
		changed = append(changed, f.New)
	}
	_, refused := ScopeAmended(changed, diffcheck.Outside(paths, v.Diff, v.Tracked))
	for _, p := range refused {
		out = append(out, LintFinding{File: p, Token: LintPaths, What: "not named by PATHS " + strings.Join(paths, ",")})
	}
	// a shrink-only ledger that gains a row
	for _, f := range files {
		if !shrinkonly.ShrinkOnly(f.New) {
			continue
		}
		if n, l := firstAdded(f); n > 0 {
			out = append(out, LintFinding{File: f.New, Line: n, Token: LintLedger, What: "adds the row " + strconv.Quote(l)})
		}
	}
	out = append(out, testAbsent(in.Brief, v)...)
	for _, f := range files {
		if !strings.HasSuffix(f.New, ".go") {
			continue
		}
		src, ok := v.show(f.New)
		if !ok {
			continue // deleted at the head
		}
		if n := unformattedLine(src); n > 0 {
			out = append(out, LintFinding{File: f.New, Line: n, Token: LintGofmt, What: "not gofmt-clean"})
		}
	}
	out = append(out, trailerFindings(in, v.Messages)...)
	if len(v.Conflicts) > 0 {
		out = append(out, LintFinding{File: v.Conflicts[0], Token: LintMerge, What: "the head does not merge onto the base tip: conflicts in " + strings.Join(v.Conflicts, ", ")})
	}
	out = append(out, verbFindings(files, v)...)
	out = append(out, clockFindings(files, v)...)
	return out
}

func (v WorkView) show(p string) ([]byte, bool) {
	if v.Show == nil {
		return nil, false
	}
	return v.Show(p)
}

// added is the new-side line numbers and texts a file's hunks add.
func added(f diffcheck.File) map[int]string {
	out := map[int]string{}
	for _, h := range f.Hunks {
		n := h.NewStart
		for _, l := range h.Lines {
			switch l[0] {
			case '+':
				out[n] = l[1:]
				n++
			case ' ':
				n++
			}
		}
	}
	return out
}

// firstAdded is the first line a file's diff adds, 0 when it adds none.
func firstAdded(f diffcheck.File) (int, string) {
	lines := added(f)
	if len(lines) == 0 {
		return 0, ""
	}
	n := slices.Min(slices.Collect(maps.Keys(lines)))
	return n, lines[n]
}

// unformattedLine is the first line gofmt would change in src, 0 when it is clean or does
// not parse (a file that does not parse is the build's to refuse, and the gate's).
func unformattedLine(src []byte) int {
	got, err := format.Source(src)
	if err != nil || bytes.Equal(got, src) {
		return 0
	}
	a, b := bytes.Split(src, []byte("\n")), bytes.Split(got, []byte("\n"))
	for i := range a {
		if i >= len(b) || !bytes.Equal(a[i], b[i]) {
			return i + 1
		}
	}
	return len(a)
}

// testAbsent is the TEST line's test missing at the head: no func of its name in the
// package's _test.go files. A brief with no TEST line, or TEST: none, is the brief's lint's.
func testAbsent(brief string, v WorkView) []LintFinding {
	raw, ok := swarm.CardHeaderValue([]byte(brief), "TEST")
	if !ok || v.Ls == nil {
		return nil
	}
	tl, why := cardhdr.ParseTest(raw)
	if why != "" || tl.None || tl.Name == "" {
		return nil
	}
	name, _, _ := strings.Cut(tl.Name, "/")
	dir := path.Clean(strings.TrimPrefix(tl.Package, "./"))
	for _, f := range v.Ls(dir) {
		if !strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, ok := v.show(path.Join(dir, path.Base(f)))
		if !ok {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, d := range file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
				return nil
			}
		}
	}
	return []LintFinding{{File: dir, Token: LintTestAbsent, What: "no func " + name + " in " + dir + "/*_test.go at the head (TEST: " + strings.TrimSpace(raw) + ")"}}
}

// The model families and harnesses a trailer may name; a name the run's usage does not
// record contradicts it.
var (
	modelFamilies = []string{"claude", "opus", "sonnet", "haiku", "fable", "gpt", "codex", "gemini", "grok", "qwen", "deepseek", "kimi", "glm", "llama", "mistral"}
	harnessNames  = map[string]string{"claude code": "claude", "opencode": "opencode", "codex cli": "codex", "gemini cli": "gemini", "aider": "aider", "cursor": "cursor"}
	trailerRE     = regexp.MustCompile(`(?im)^\s*(co-authored-by|by):\s*(.+)$`)
	wordRE        = regexp.MustCompile(`[a-z]+`)
)

// trailerFindings is each trailer line that names a model family the run's model does not
// carry, or a harness other than the run's: the usage line and the route are the record,
// never the brief (section 6, reader-ignores-attribution: a reader never judges
// attribution). What the record does not say (no model, no harness) is not judged.
func trailerFindings(in WorkLintInput, messages []string) []LintFinding {
	model := strings.ToLower(in.Model)
	harness := strings.ToLower(in.Harness)
	var out []LintFinding
	seen := map[string]bool{}
	for _, m := range messages {
		for _, g := range trailerRE.FindAllStringSubmatch(m, -1) {
			line := strings.TrimSpace(g[0])
			who := strings.ToLower(g[2])
			if i := strings.IndexByte(who, '<'); i >= 0 {
				who = who[:i] // the address is not a name
			}
			var why string
			if model != "" {
				for _, w := range wordRE.FindAllString(who, -1) {
					if slices.Contains(modelFamilies, w) && !strings.Contains(model, w) {
						why = "names " + w + "; the attempt's usage records model " + in.Model
						break
					}
				}
			}
			if why == "" && harness != "" {
				for _, name := range slices.Sorted(maps.Keys(harnessNames)) {
					if strings.Contains(who, name) && !strings.Contains(harness, harnessNames[name]) {
						why = "names the harness " + name + "; the attempt ran under " + harness
						break
					}
				}
			}
			if why != "" && !seen[line] {
				seen[line] = true
				out = append(out, LintFinding{Token: LintTrailer, What: strconv.Quote(line) + " " + why})
			}
		}
	}
	return out
}

// verbRowRE is a row of nova-sprint's verbs table (cmd/nova-sprint/verbs.go): the verb's
// name, then its syntax.
var verbRowRE = regexp.MustCompile(`^\s*\{"([a-z][a-z -]*)",\s*"((?:[^"\\]|\\.)*)"`)

// verbFindings is each verbs-table row the diff adds or changes whose verb writes (its
// class in the same directory's verbClasses is not classRead) and whose syntax has no
// --dry-run. A verb no class names is not judged.
func verbFindings(files []diffcheck.File, v WorkView) []LintFinding {
	var out []LintFinding
	for _, f := range files {
		if !strings.HasPrefix(f.New, "cmd/") || !strings.HasSuffix(f.New, ".go") || strings.HasSuffix(f.New, "_test.go") {
			continue
		}
		lines := added(f)
		ns := slices.Sorted(maps.Keys(lines))
		var classes []byte
		for _, n := range ns {
			g := verbRowRE.FindStringSubmatch(lines[n])
			if g == nil || strings.Contains(g[2], "--dry-run") {
				continue
			}
			if classes == nil {
				classes, _ = v.show(path.Join(path.Dir(f.New), "coordinator.go"))
				if classes == nil {
					classes = []byte{}
				}
			}
			class := regexp.MustCompile(`"` + regexp.QuoteMeta(g[1]) + `":\s*(class[A-Za-z]+)`).FindSubmatch(classes)
			if class == nil || string(class[1]) == "classRead" {
				continue
			}
			out = append(out, LintFinding{File: f.New, Line: n, Token: LintVerbDryRun, What: "the verb " + strconv.Quote(g[1]) + " (" + string(class[1]) + ") has no --dry-run in its syntax"})
		}
	}
	return out
}

// clockFindings is each call of time.Sleep or time.Now on a line the diff adds to a
// _test.go file, outside a synctest bubble (a func literal given to synctest.Test or
// synctest.Run). time.Now passed as a value is an injected clock, never a call.
func clockFindings(files []diffcheck.File, v WorkView) []LintFinding {
	var out []LintFinding
	for _, f := range files {
		if !strings.HasSuffix(f.New, "_test.go") {
			continue
		}
		lines := added(f)
		if len(lines) == 0 {
			continue
		}
		src, ok := v.show(f.New)
		if !ok {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f.New, src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		var stack []ast.Node
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn := selectorOf(call.Fun)
			if fn != "time.Sleep" && fn != "time.Now" {
				return true
			}
			line := fset.Position(call.Pos()).Line
			if _, isNew := lines[line]; !isNew || inBubble(stack) {
				return true
			}
			out = append(out, LintFinding{File: f.New, Line: line, Token: LintTestClock, What: fn + " in a unit test, outside a synctest bubble"})
			return true
		})
	}
	return out
}

// selectorOf is pkg.Name for a selector on an identifier, "" otherwise.
func selectorOf(e ast.Expr) string {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return id.Name + "." + sel.Sel.Name
}

// inBubble says the innermost node of stack is inside a func literal that is an argument
// of synctest.Test or synctest.Run.
func inBubble(stack []ast.Node) bool {
	for i := len(stack) - 1; i > 0; i-- {
		lit, ok := stack[i].(*ast.FuncLit)
		if !ok {
			continue
		}
		if call, ok := stack[i-1].(*ast.CallExpr); ok {
			if fn := selectorOf(call.Fun); (fn == "synctest.Test" || fn == "synctest.Run") && slices.ContainsFunc(call.Args, func(a ast.Expr) bool { return a == lit }) {
				return true
			}
		}
	}
	return false
}

// WorkLinter is the work lint of a primary in review at its head, run by the binding's
// git (worklint_git.go, NewWorkLinter): its findings, nil when it passes. An error says the
// lint could not run (no clone of the repository, a git that failed): the attempt is asked
// as it was before the lint, and the error is no finding.
type WorkLinter func(s *Snapshot, pr *Card) ([]LintFinding, error)

// DefaultWorkLint is the work lint a tick runs when its request names none
// (TickReq.WorkLint): nova-sprint sets it once, at its start (cmd/nova-sprint/worklint.go);
// nil runs no lint.
var DefaultWorkLint WorkLinter

func (r TickReq) workLint() WorkLinter {
	if r.WorkLint != nil {
		return r.WorkLint
	}
	return DefaultWorkLint
}

// lintable says the primary is a finished attempt the lint holds before its first read:
// in review, its work not failed, no read card at its attempt, and no change queued for it.
func lintable(s *Snapshot, c *Card) bool {
	return c.Col == Review && c.F("result") != "failed" && len(readsAt(s, c, c.Int("attempt"))) == 0 && !s.Held[c.ID]
}

// lintFailing is the primaries in review the lint refuses, with their findings, in work
// order; a lint that could not run refuses none.
func lintFailing(s *Snapshot, l WorkLinter) ([]*Card, map[string][]LintFinding) {
	if l == nil || s.Work == nil {
		return nil, nil
	}
	var out []*Card
	found := map[string][]LintFinding{}
	for _, c := range s.Work.Column(Review) {
		if !lintable(s, c) {
			continue
		}
		fs, err := l(s, c)
		if err != nil || len(fs) == 0 {
			continue
		}
		out = append(out, c)
		found[c.ID] = fs
	}
	return out, found
}

// TickLint is the work lint as the pump runs it, before the accept (TickAccept): every
// finished attempt the lint refuses is reworked at once, the findings its fix and its
// finding, its lint reworks and broken reads counted one more; a rework refused (a bound)
// leaves its judgment, as any rework refused does, and the ask still asks it of no reader
// (lintHeld). ok is false when nothing is reworked.
func TickLint(s *Snapshot, r TickReq) (Plan, bool) {
	failing, found := lintFailing(s, r.workLint())
	if len(failing) == 0 {
		return Plan{}, false
	}
	per := map[string]ReworkCard{}
	var ids []string
	for _, c := range failing {
		fix := cutText(LintFix(c.Int("attempt"), c.F("head"), found[c.ID]), MaxCardTextBytes)
		per[c.ID] = ReworkCard{Fix: fix, Finding: cutText(LintFinds(found[c.ID]), MaxCardTextBytes), Set: map[string]string{
			FieldLintReworks: itoa(c.Int(FieldLintReworks) + 1),
			"broken_reads":   itoa(c.Int("broken_reads") + 1),
		}}
		ids = append(ids, c.ID)
	}
	p := Rework(s, ReworkReq{Sel: Sel{Only: ids}, Who: r.who(), PerCard: per})
	for i := range p.Units {
		if fs, ok := found[p.Units[i].Key]; ok {
			var toks []string
			for _, f := range fs {
				if !slices.Contains(toks, f.Token) {
					toks = append(toks, f.Token)
				}
			}
			p.Units[i].Moved += "; work lint: " + strings.Join(toks, ",")
		}
	}
	// a rework refused is a bound (the brief's, or the attempt cap's): the coordinator's
	// judgment, written once, with the bound's words; the ask still holds the attempt
	for _, x := range p.Refused {
		pr := s.Work.Placed(x.Key)
		if pr == nil || len(closesFor(s.Open, []string{NBriefWrong}, pr.ID)) > 0 ||
			slices.ContainsFunc(p.Notes, func(n Note) bool { return n.Type == NBriefWrong && slices.Contains(n.Primaries, pr.ID) }) {
			continue
		}
		n := judgment(NBriefWrong, pr.Row, s.Now, 0, pr.ID)
		n.Who, n.Attempt = r.who(), pr.Int("attempt")
		n.What = x.Why + "; the work lint refused attempt " + itoa(pr.Int("attempt")) + ": " + cutText(LintFinds(found[pr.ID]), MaxCardTextBytes)
		p.Notes = append(p.Notes, n)
	}
	p.Refused = nil
	if len(p.Units) == 0 && len(p.Notes) == 0 {
		return Plan{}, false
	}
	return p, true
}

// lintHeld is the ask with every primary the lint refuses hidden from it: no reader, the
// friends' or the machine's, is asked of an attempt the lint refuses, whether the pump's
// rework of it is still to come or was refused at a bound. Each hidden primary is due.
func lintHeld(ask TickPartFn) TickPartFn {
	return func(s *Snapshot, r TickReq) (Plan, int) {
		failing, _ := lintFailing(s, r.workLint())
		if len(failing) == 0 {
			return ask(s, r)
		}
		for _, c := range failing {
			c.Col = ""
		}
		s.Work.cells, s.Work.byPrimary = nil, nil
		p, due := ask(s, r)
		for _, c := range failing {
			c.Col = Review
		}
		s.Work.cells, s.Work.byPrimary = nil, nil
		return p, due + len(failing)
	}
}

// The ask the machine runs is the function TickTables holds, wrapped by the friend ask
// (friend_read.go, whose init runs before this file's): the lint's hold wraps it there,
// in TickParts, and in the held rule's parts, so every ask holds a refused attempt.
func init() {
	var before uintptr
	for i := range TickTables {
		for j := range TickTables[i].Parts {
			if TickTables[i].Parts[j].Name == "ask" && TickTables[i].Parts[j].Fn != nil {
				before = reflect.ValueOf(TickTables[i].Parts[j].Fn).Pointer()
				TickTables[i].Parts[j].Fn = lintHeld(TickTables[i].Parts[j].Fn)
			}
		}
	}
	for i := range TickParts {
		if TickParts[i].Name == "ask" {
			TickParts[i].Fn = lintHeld(TickParts[i].Fn)
		}
	}
	for i := range heldParts {
		if heldParts[i] != nil && reflect.ValueOf(heldParts[i]).Pointer() == before {
			heldParts[i] = lintHeld(heldParts[i])
		}
	}
}

// WorkLintTables is the tick's tables with the work lint l in every part's request: a
// store given them (store.Store.Updates) lints with l whatever DefaultWorkLint is.
func WorkLintTables(l WorkLinter) []TableUpdate {
	out := make([]TableUpdate, len(TickTables))
	for i, u := range TickTables {
		out[i] = TableUpdate{Table: u.Table, Parts: make([]TickPartDef, len(u.Parts))}
		for j, p := range u.Parts {
			out[i].Parts[j] = p
			if fn := p.Fn; fn != nil {
				out[i].Parts[j].Fn = func(s *Snapshot, r TickReq) (Plan, int) {
					r.WorkLint = l
					return fn(s, r)
				}
			}
		}
	}
	return out
}
