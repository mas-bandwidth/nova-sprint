package sprint

import (
	"context"
	"fmt"
	"os"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/internal/diffcheck"
	"github.com/mas-bandwidth/nova-sprint/internal/gitrun"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// The machine gate (docs/SPEC-SPRINT.md section 6, the machine gate). Measured on the
// sprint log of epoch 15: 7,552 reads ran and every read ran Go on a bench itself (clone,
// sync, vet, test), and 323 broken reads were a red gate the machine could have seen
// without a model. The gate runs once, by the machine, after the work lint
// (worklint.go, TickLint) passes and before any reader is asked: the attempt's head on a
// bench through the bench-run verb (nova-ci bench run; a fake in tests), the TEST line's
// test plus the touched packages' go vet and go test -count=1 -timeout 600s. A red gate
// reworks the attempt at once with the gate's failing lines (file:line where the output
// has one) as its fix, counted toward the card's bound as a broken read is, and asks no
// reader; green records the gate's lines on the attempt, and the read packet carries them,
// so a reader judges whether the change does the brief and never reruns Go. A bench that
// does not answer is not a pass: the attempt waits, and one judgment says so.

// The gate's state on a primary.
const (
	GateGreen   = "green"   // the gate ran and passed
	GateRed     = "red"     // the gate ran and failed: the attempt was reworked
	GateWaiting = "waiting" // no bench answered: the attempt waits, no reader is asked
)

// The gate's fields on the primary, one set per attempt (FieldGateAttempt).
const (
	FieldGate        = "gate"         // green | red | waiting
	FieldGateAttempt = "gate_attempt" // the attempt the state belongs to
	FieldGateLines   = "gate_lines"   // a green gate's lines, one per line
	FieldGateBench   = "gate_bench"   // the bench that answered
	FieldGateWait    = "gate_wait"    // why no bench answered
	FieldGateReworks = "gate_reworks" // the primary's count of attempts the gate sent back
)

// NBenchDown is the judgment a gate that could not run raises: no bench answered, and the
// attempt waits, asked of no reader, until the coordinator acts.
const NBenchDown = "no bench answered the gate"

// GateFinding is one failing line of a red gate: the file and line the gate's output names
// (0 when it names none), and the line itself.
type GateFinding struct {
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	What string `json:"what"`
}

// String is the finding as a rework's fix carries it: file:line, then the line.
func (f GateFinding) String() string {
	at := f.File
	if at != "" && f.Line > 0 {
		at += ":" + strconv.Itoa(f.Line)
	}
	if at != "" {
		at += ": "
	}
	return at + f.What
}

// GateFinds is the findings as one finding, the same words for the same findings at any
// attempt (the brief's bound compares them: SameFinding).
func GateFinds(fs []GateFinding) string {
	parts := make([]string, len(fs))
	for i, f := range fs {
		parts[i] = f.String()
	}
	return "machine gate: " + strings.Join(parts, "; ")
}

// GateFix is the findings as one fix: the attempt and head the gate refused, then each
// failing line.
func GateFix(attempt int, head string, fs []GateFinding) string {
	return "the machine gate refused attempt " + itoa(attempt) + " at " + orDash(head) + "; " + GateFinds(fs)
}

// GateRun is one attempt's gate as the machine ran it on a bench: Host is the bench that
// answered, Lines the gate's lines (each command and the last line it printed) recorded on
// a green attempt, Failed the failing lines of a red one, and Waiting why no bench
// answered ("" when one did; a gate that could not run at all waits too).
type GateRun struct {
	Host    string
	Lines   []string
	Failed  []GateFinding
	Waiting string
}

// GateLines is a green gate's recorded lines as the read packet carries them, without
// empty items.
func GateLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// GateRunner runs one finished attempt's whole gate on a bench at its head and returns
// what it did. It is the machine's, never a model's: the binding builds it over the
// bench-run verb (cmd/nova-sprint/gaterun.go, `nova-ci bench run`), and the tests pass a
// fake. A runner that is not there (nil) is no gate: every attempt is asked as before.
type GateRunner func(s *Snapshot, pr *Card) GateRun

// DefaultGate is the gate a tick runs when its request names none (TickReq.Gate):
// nova-sprint sets it once, at its start (cmd/nova-sprint/gaterun.go); nil runs no gate.
var DefaultGate GateRunner

func (r TickReq) gate() GateRunner {
	if r.Gate != nil {
		return r.Gate
	}
	return DefaultGate
}

// GateBudget bounds one attempt's whole gate on a bench: the TEST line's test and the
// touched packages' vet and tests, each with go test's own -timeout 600s, and the copy.
const GateBudget = 20 * time.Minute

// gateLocRE is a file:line the gate's output names, the shape a go build, vet or test
// failure prints. The file is a Go file or a path under the module.
var gateLocRE = regexp.MustCompile(`([A-Za-z0-9_][A-Za-z0-9_./-]*\.go):(\d+):`)

// GateFindings is the failing lines of a red gate's output: every non-empty line, each
// with the file and line it names when it names one (the first match on the line). The
// lines are kept whole, so a reader and the next attempt see exactly what the bench
// printed.
func GateFindings(out string) []GateFinding {
	var fs []GateFinding
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		f := GateFinding{What: strings.TrimSpace(l)}
		if m := gateLocRE.FindStringSubmatch(l); m != nil {
			f.File = m[1]
			f.Line, _ = strconv.Atoi(m[2])
		}
		fs = append(fs, f)
	}
	return fs
}

// gateable says the primary is a finished attempt the gate holds before its first read:
// in review, its work not failed, no read card at its attempt, no change queued for it,
// and the gate of this attempt not decided yet (a green, red or waiting state at an
// earlier attempt is no decision about this one).
func gateable(s *Snapshot, c *Card, g GateRunner) bool {
	if g == nil || c.Col != Review || c.F("result") == "failed" {
		return false
	}
	if len(readsAt(s, c, c.Int("attempt"))) > 0 || s.Held[c.ID] {
		return false
	}
	return c.Int(FieldGateAttempt) != c.Int("attempt")
}

// gateHeldPrimaries is the primaries in review no reader may be asked of: one the gate
// refused (red) or that waits for a bench (waiting), and, while a gate is configured, one
// whose gate for this attempt has not been run yet. The pump's gate part (TickGate) runs
// it before the ask; the no-stall rule's ask, which may run with no pump, holds it too.
func gateHeldPrimaries(s *Snapshot, r TickReq) []*Card {
	if s.Work == nil {
		return nil
	}
	var out []*Card
	for _, c := range s.Work.Column(Review) {
		switch c.F(FieldGate) {
		case GateRed, GateWaiting:
			out = append(out, c)
		default:
			if r.gate() != nil && c.Int(FieldGateAttempt) != c.Int("attempt") {
				out = append(out, c)
			}
		}
	}
	return out
}

// hideGateHeld takes the primaries the gate holds out of the work table for the duration
// of the ask, as lintHeld does the lint's (worklint.go): no reader, the friends' or the
// machine's, is asked of an attempt the gate has not passed. restore puts them back.
func hideGateHeld(s *Snapshot, r TickReq) func() {
	held := gateHeldPrimaries(s, r)
	if len(held) == 0 {
		return func() {}
	}
	for _, c := range held {
		c.Col = ""
	}
	s.Work.cells, s.Work.byPrimary = nil, nil
	return func() {
		for _, c := range held {
			c.Col = Review
		}
		s.Work.cells, s.Work.byPrimary = nil, nil
	}
}

// gateHeldPart wraps the tick's ask with the machine gate's hold: every attempt the gate
// holds (gateHeldPrimaries) is taken out of the work table for the whole ask, so no reader
// is asked of it, the friends' frontier ask and the machine ask alike (the finding of
// attempt 7: the hold wrapped TickAsk alone, and friendAskPart asked the frontier readers
// before it). TickAsk holds them too, so a caller that reaches it directly is held as well.
func gateHeldPart(ask TickPartFn) TickPartFn {
	return func(s *Snapshot, r TickReq) (Plan, int) {
		restore := hideGateHeld(s, r)
		defer restore()
		return ask(s, r)
	}
}

// The ask the machine runs is the function TickTables holds: friend_read.go's init wraps
// TickAsk with the friend ask before this file's, and worklint.go's wraps the result with
// the lint's hold after it (worklint.go, whose init runs after this file's). This init
// wraps the friend ask with the gate's hold, so every ask, the friend ask included, holds
// an attempt whose gate has not passed.
func init() {
	var before uintptr
	for i := range TickTables {
		for j := range TickTables[i].Parts {
			if TickTables[i].Parts[j].Name == "ask" && TickTables[i].Parts[j].Fn != nil {
				before = reflect.ValueOf(TickTables[i].Parts[j].Fn).Pointer()
				TickTables[i].Parts[j].Fn = gateHeldPart(TickTables[i].Parts[j].Fn)
			}
		}
	}
	for i := range TickParts {
		if TickParts[i].Name == "ask" {
			TickParts[i].Fn = gateHeldPart(TickParts[i].Fn)
		}
	}
	for i := range heldParts {
		if heldParts[i] != nil && reflect.ValueOf(heldParts[i]).Pointer() == before {
			heldParts[i] = gateHeldPart(heldParts[i])
		}
	}
}

// TickGate is the machine gate as the pump runs it, after the work lint (TickAccept calls
// TickLint first) and before the readers are asked (TickAsk holds every attempt it has not
// passed): every gateable attempt is run on a bench once. A red gate is reworked at once
// with its failing lines as the fix and its finding, its gate reworks and broken reads
// counted one more; a green one records the gate's lines on the attempt for the read
// packet; a gate no bench answered leaves the attempt in review with one judgment. ok is
// false when the gate moved nothing.
func TickGate(s *Snapshot, r TickReq) (Plan, bool) {
	g := r.gate()
	if g == nil || s.Work == nil {
		return Plan{}, false
	}
	var p Plan
	type redGate struct {
		fix, finding string
		set          map[string]string
	}
	reds := map[string]redGate{}
	var redIDs []string
	var waiting []*Card
	for _, c := range s.Work.Column(Review) {
		if !gateable(s, c, g) {
			continue
		}
		run := g(s, c)
		attempt := c.Int("attempt")
		set := map[string]string{FieldGateAttempt: itoa(attempt)}
		switch {
		case run.Waiting != "":
			set[FieldGate] = GateWaiting
			set[FieldGateWait] = cutText(run.Waiting, MaxCardTextBytes)
			p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row,
				Changes: []Change{change(Work, setEntry(c, set))},
				Moved:   c.ID + " gate waits: " + run.Waiting})
			waiting = append(waiting, c)
		case len(run.Failed) > 0:
			set[FieldGate] = GateRed
			set[FieldGateReworks] = itoa(c.Int(FieldGateReworks) + 1)
			set["broken_reads"] = itoa(c.Int("broken_reads") + 1)
			reds[c.ID] = redGate{fix: GateFix(attempt, c.F("head"), run.Failed), finding: GateFinds(run.Failed), set: set}
			redIDs = append(redIDs, c.ID)
		default:
			set[FieldGate] = GateGreen
			set[FieldGateLines] = cutText(strings.Join(run.Lines, "\n"), MaxCardTextBytes)
			if run.Host != "" {
				set[FieldGateBench] = run.Host
			}
			p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row,
				Changes: []Change{change(Work, setEntry(c, set))},
				Moved:   c.ID + " gate green on " + orDash(run.Host)})
		}
	}
	if len(redIDs) > 0 {
		per := map[string]ReworkCard{}
		for id, rd := range reds {
			per[id] = ReworkCard{Fix: cutText(rd.fix, MaxCardTextBytes), Finding: cutText(rd.finding, MaxCardTextBytes),
				Why: "the attempt finished and the machine gate refused it", Set: rd.set}
		}
		rp := Rework(s, ReworkReq{Sel: Sel{Only: redIDs}, Who: r.who(), PerCard: per})
		// the green and waiting marks of the same pass ride with the rework's plan
		rp.Units = append(rp.Units, p.Units...)
		rp.Notes = append(rp.Notes, p.Notes...)
		p = rp
	}
	for _, c := range waiting {
		n := judgment(NBenchDown, c.Row, s.Now, 0, c.ID)
		n.Who, n.Attempt = r.who(), c.Int("attempt")
		// the gate's own decisions: it is no condition the tick keeps (the waiting state
		// itself holds the attempt), so it is not in Decisions or TickDecisions.
		n.Decisions = []string{"wait", "rework", "drop"}
		n.What = "the machine gate could not run for attempt " + itoa(c.Int("attempt")) + ": " + c.F(FieldGateWait) + "; the attempt waits, asked of no reader"
		p.Notes = append(p.Notes, n)
	}
	if len(p.Units) == 0 && len(p.Notes) == 0 {
		return Plan{}, false
	}
	return p, true
}

// GateTables is the tick's tables with the gate g in every part's request: a store given
// them (store.Store.Updates) gates with g whatever DefaultGate is, as WorkLintTables does
// the lint.
func GateTables(g GateRunner) []TableUpdate {
	out := make([]TableUpdate, len(TickTables))
	for i, u := range TickTables {
		out[i] = TableUpdate{Table: u.Table, Parts: make([]TickPartDef, len(u.Parts))}
		for j, p := range u.Parts {
			out[i].Parts[j] = p
			if fn := p.Fn; fn != nil {
				out[i].Parts[j].Fn = func(s *Snapshot, r TickReq) (Plan, int) {
					r.Gate = g
					return fn(s, r)
				}
			}
		}
	}
	return out
}

// The gate's judgment is its own type with its own decisions (NBenchDown), not a condition
// the tick keeps: the waiting state holds the attempt until the coordinator answers, so it
// is not in Decisions or TickDecisions, and no other part writes it.

// BenchResult is one command's result on a bench: the bench that answered, the command's
// exit status, and its combined output.
type BenchResult struct {
	Host string
	Code int
	Out  string
}

// BenchRun runs one command on a bench against a copy of the tree at head in clone: the
// bench-run verb's seam (card bench-run-verb: `nova-ci bench run --host <h> --dir <tree> --
// <argv>`). An error is a run that never reached the command (no bench answered, the copy
// failed): the gate waits, never a red. The production seam is cmd/nova-sprint/gaterun.go;
// the tests pass a fake.
type BenchRun func(ctx context.Context, hosts []string, clone, head string, argv []string) (BenchResult, error)

// BenchGateGit is where the machine gate finds and runs a card's repository: Clone is the
// clone of the repository a brief names (its REPO or base-repo line), an error when there
// is none; Hosts the benches in the order they are tried; Bench the bench-run verb; Env the
// git children's environment (View's, when it reads); View reads the attempt's view, nil
// for ReadWorkView; Budget bounds one attempt's whole gate, zero for GateBudget; TTL is how
// long one head's gate is kept, zero for two minutes (the held rule asks the tick's parts
// more than once, and a bench is not run twice for the same head while its result is
// fresh); Now is the clock the cache is kept by, nil for the wall clock.
type BenchGateGit struct {
	Clone  func(repo string) (string, error)
	Hosts  []string
	Bench  BenchRun
	Env    []string
	View   func(ctx context.Context, dir, base, head string, env []string) (WorkView, error)
	Budget time.Duration
	TTL    time.Duration
	Now    func() time.Time
}

// TestLineOfBrief is the TEST line of a brief, the zero TestLine when it names none or one
// cardhdr.ParseTest refuses (a card with no test is the brief lint's to refuse).
func TestLineOfBrief(brief string) cardhdr.TestLine {
	v, ok := cardhdr.Value(brief, "TEST")
	if !ok {
		return cardhdr.TestLine{}
	}
	tl, why := cardhdr.ParseTest(v)
	if why != "" {
		return cardhdr.TestLine{}
	}
	return tl
}

// GatePackages is the packages a change touches, ./dir each, sorted: the directory of every
// changed .go file, and the TEST line's package even when no .go file of it changed (a test
// file may be the whole change).
func GatePackages(v WorkView, tl cardhdr.TestLine) []string {
	set := map[string]bool{}
	for _, f := range diffcheck.Parse(v.Diff) {
		if f.New == "" || !strings.HasSuffix(f.New, ".go") {
			continue
		}
		set[pkgArg(path.Dir(f.New))] = true
	}
	if tl.Package != "" {
		set[pkgArg(strings.TrimPrefix(tl.Package, "./"))] = true
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// pkgArg is a directory as a go package argument: "." is ".", "a/b" is "./a/b".
func pkgArg(dir string) string {
	if dir == "." || dir == "" {
		return "."
	}
	return "./" + strings.TrimPrefix(dir, "./")
}

// GateCommands is the gate of an attempt whose brief names test tl and whose change touches
// pkgs, in the order the brief names them: the touched packages' go vet and go test
// -count=1 -timeout 600s, then the TEST line's test alone. Each is one command run through
// the bench-run verb.
func GateCommands(tl cardhdr.TestLine, pkgs []string) [][]string {
	tags := []string(nil)
	if tl.Tags != "" {
		tags = []string{"-tags", tl.Tags}
	}
	var cmds [][]string
	if len(pkgs) > 0 {
		vet := append([]string{"go", "vet"}, tags...)
		cmds = append(cmds, append(vet, pkgs...))
		test := append([]string{"go", "test", "-count=1", "-timeout", "600s"}, tags...)
		cmds = append(cmds, append(test, pkgs...))
	}
	if tl.Package != "" && tl.Name != "" {
		run := append([]string{"go", "test", "-count=1", "-timeout", "600s"}, tags...)
		cmds = append(cmds, append(run, "-run", "^"+tl.Name+"$", tl.Package))
	}
	return cmds
}

// benchTestRunner adapts BenchRun to BenchRunner for lint checks.
func benchTestRunner(bench BenchRun, hosts []string, dir string, args []string, env []string) BenchRunner {
	return func(commit, pkg, testname string) (bool, error) {
		ctx := context.Background()
		// Build test command: go test -run <testname> <pkg>
		argv := append([]string{"go", "test", "-count=1", "-timeout", "600s", "-run", "^" + testname + "$"}, pkg)
		res, err := bench(ctx, hosts, dir, commit, argv)
		if err != nil {
			return false, err
		}
		// go test exits zero when the named test does not exist.  A new test is
		// allowed to be absent at the merge-base, so do not mistake that for a
		// passing pin probe.
		if res.Code == 0 && strings.Contains(res.Out, "[no tests to run]") {
			return false, nil
		}
		return res.Code == 0, nil
	}
}

// GateLintRun runs the lint checks after the gate commands pass.
// It returns GateLintFindings if any issues are found.
func GateLintRun(v WorkView, tl cardhdr.TestLine, mergeBase, dir string, hosts []string, benchRun BenchRun, env []string, deletion bool) ([]GateLintFinding, error) {
	// A comparison has no valid meaning without the pinned merge-base. This
	// keeps the fake bench seam focused on gate commands; real WorkView values
	// always carry the merge-base from ReadWorkView.
	if mergeBase == "" || v.Head == "" {
		return nil, nil
	}
	// Build input from work view
	var changedFiles []string
	for _, f := range diffcheck.Parse(v.Diff) {
		if f.New != "" && strings.HasSuffix(f.New, ".go") && !strings.HasSuffix(f.New, "_test.go") {
			changedFiles = append(changedFiles, f.New)
		}
	}

	reverted, err := materializeRevertedTree(context.Background(), dir, mergeBase, v.Head, changedFiles, env)
	if err != nil {
		return nil, fmt.Errorf("could not materialize reverted tree: %w", err)
	}
	headDir, cleanupHead, err := materializeHeadTree(context.Background(), dir, v.Head, env)
	if err != nil {
		return nil, fmt.Errorf("could not materialize pinned head: %w", err)
	}
	defer cleanupHead()
	baseDir, cleanupBase, err := materializeHeadTree(context.Background(), dir, mergeBase, env)
	if err != nil {
		return nil, fmt.Errorf("could not materialize merge-base: %w", err)
	}
	defer cleanupBase()
	input := GateLintInput{
		MergeBase:    mergeBase,
		Head:         v.Head,
		RevertedHead: reverted,
		TestPkg:      tl.Package,
		TestName:     tl.Name,
		ChangedFiles: changedFiles,
		ChangedDir:   headDir,
		BaseDir:      baseDir,
		SkipReach:    deletion,
	}

	if benchRun == nil {
		return GateLintFindingsChecked(input, nil)
	}

	adapted := benchTestRunner(benchRun, hosts, dir, nil, env)
	return GateLintFindingsChecked(input, adapted)
}

func materializeHeadTree(ctx context.Context, dir, head string, env []string) (string, func(), error) {
	root, err := os.MkdirTemp("", "nova-gatelint-head-")
	if err != nil {
		return "", nil, err
	}
	work := path.Join(root, "tree")
	o := gitrun.Options{C: dir, Env: env, OwnRepo: true}
	if _, err := gitrun.Output(ctx, o, "worktree", "add", "--detach", work, head); err != nil {
		_ = os.RemoveAll(root)
		return "", nil, err
	}
	got, err := gitrun.Output(ctx, gitrun.Options{C: work, Env: env, OwnRepo: true}, "rev-parse", "HEAD")
	if err != nil || !strings.HasPrefix(strings.TrimSpace(got), head) || len(head) < 7 {
		_, _ = gitrun.Output(ctx, o, "worktree", "remove", "--force", work)
		_ = os.RemoveAll(root)
		if err == nil {
			err = fmt.Errorf("checked out %s, want %s", strings.TrimSpace(got), head)
		}
		return "", nil, err
	}
	cleanup := func() {
		_, _ = gitrun.Output(context.Background(), o, "worktree", "remove", "--force", work)
		_ = os.RemoveAll(root)
	}
	return work, cleanup, nil
}

func materializeRevertedTree(ctx context.Context, dir, base, head string, paths []string, env []string) (string, error) {
	root, err := os.MkdirTemp("", "nova-gatelint-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(root)
	work := path.Join(root, "tree")
	o := gitrun.Options{C: dir, Env: env, OwnRepo: true}
	if _, err := gitrun.Output(ctx, o, "worktree", "add", "--detach", work, head); err != nil {
		return "", err
	}
	defer gitrun.Output(ctx, o, "worktree", "remove", "--force", work)
	for _, p := range paths {
		if _, err := gitrun.Output(ctx, o, "cat-file", "-e", base+":"+p); err != nil {
			if _, err := gitrun.Output(ctx, gitrun.Options{C: work, Env: env, OwnRepo: true}, "rm", "-f", "--", p); err != nil {
				return "", err
			}
			continue
		}
		if _, err := gitrun.Output(ctx, gitrun.Options{C: work, Env: env, OwnRepo: true}, "checkout", base, "--", p); err != nil {
			return "", err
		}
	}
	wo := gitrun.Options{C: work, Env: append(env, "GIT_AUTHOR_NAME=nova-gatelint", "GIT_AUTHOR_EMAIL=nova-gatelint@invalid", "GIT_COMMITTER_NAME=nova-gatelint", "GIT_COMMITTER_EMAIL=nova-gatelint@invalid"), OwnRepo: true}
	if _, err := gitrun.Output(ctx, wo, "add", "-A"); err != nil {
		return "", err
	}
	tree, err := gitrun.Output(ctx, wo, "write-tree")
	if err != nil {
		return "", err
	}
	commit, err := gitrun.Output(ctx, wo, "commit-tree", strings.TrimSpace(tree), "-p", head, "-m", "gatelint reverted non-test tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(commit), nil
}

// NewBenchGate is the machine gate over g: each attempt's brief names its repository, its
// head is read (the files the change touches), and each gate command is run on a bench
// through g.Bench. A run that does not answer is GateRun.Waiting; a command that ran and
// failed gives its failing lines; every command that passed gives its line.
func NewBenchGate(g BenchGateGit) GateRunner {
	budget := g.Budget
	if budget <= 0 {
		budget = GateBudget
	}
	view := g.View
	if view == nil {
		view = ReadWorkView
	}
	ttl := g.TTL
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	now := g.Now
	if now == nil {
		now = time.Now
	}
	type kept struct {
		at  time.Time
		run GateRun
	}
	var mu sync.Mutex
	cache := map[string]kept{}
	return func(s *Snapshot, pr *Card) GateRun {
		brief := pr.F("brief")
		tl := TestLineOfBrief(brief)
		cb := swarm.ReadCardBase([]byte(brief))
		base := cb.Ref
		if base == "" {
			base = "main"
		}
		head := pr.F("head")
		if cb.Repo == "" || g.Clone == nil || g.Bench == nil || len(g.Hosts) == 0 {
			return GateRun{Waiting: "no bench is configured for the gate"}
		}
		if !headRE.MatchString(head) {
			return GateRun{Waiting: "the attempt names no pushed head: " + orDash(head)}
		}
		// one head's gate is read once while its result is fresh: the held rule asks the
		// tick's parts more than once, and a bench is not run twice for one commit
		key := strings.Join([]string{cb.Repo, base, head, brief}, "\x00")
		mu.Lock()
		if k, ok := cache[key]; ok && now().Sub(k.at) < ttl {
			mu.Unlock()
			return k.run
		}
		mu.Unlock()
		run := func() GateRun {
			dir, err := g.Clone(cb.Repo)
			if err != nil {
				return GateRun{Waiting: err.Error()}
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			v, err := view(ctx, dir, base, head, g.Env)
			if err != nil {
				return GateRun{Waiting: err.Error()}
			}
			if !v.Pushed {
				return GateRun{Waiting: "the head " + head + " is not in the repository"}
			}
			cmds := GateCommands(tl, GatePackages(v, tl))
			if len(cmds) == 0 {
				return GateRun{Host: "", Lines: []string{"no gate command: the brief names no test and the change touches no package"}}
			}
			var out GateRun
			for _, argv := range cmds {
				res, err := g.Bench(ctx, g.Hosts, dir, head, argv)
				if err != nil {
					return GateRun{Host: res.Host, Waiting: err.Error()}
				}
				if out.Host == "" {
					out.Host = res.Host
				}
				out.Lines = append(out.Lines, gateRunLine(argv, res))
				if res.Code != 0 {
					out.Failed = GateFindings(res.Out)
					return out
				}
			}
			// Run lint checks after gate commands pass
			kind, _ := cardhdr.Value(brief, "KIND")
			lintFindings, lintErr := GateLintRun(v, tl, v.MergeBase, dir, g.Hosts, g.Bench, g.Env, strings.EqualFold(kind, "deletion"))
			if lintErr != nil {
				return GateRun{Host: out.Host, Waiting: "machine gate lint did not answer: " + lintErr.Error()}
			}
			if len(lintFindings) > 0 {
				for _, lf := range lintFindings {
					out.Failed = append(out.Failed, GateFinding{What: GateLintFindingString(lf)})
				}
				return out
			}
			return out
		}()
		mu.Lock()
		if len(cache) > 4096 {
			clear(cache)
		}
		cache[key] = kept{at: now(), run: run}
		mu.Unlock()
		return run
	}
}

// gateLine is one gate line: the command, then the last non-empty line it printed.
func gateRunLine(argv []string, res BenchResult) string {
	last := ""
	for _, l := range strings.Split(res.Out, "\n") {
		if strings.TrimSpace(l) != "" {
			last = strings.TrimSpace(l)
		}
	}
	line := strings.Join(argv, " ")
	if last == "" {
		return line + " (exit " + strconv.Itoa(res.Code) + ")"
	}
	return line + ": " + last
}
