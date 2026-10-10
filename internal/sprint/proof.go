package sprint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/internal/diffcheck"
)

// The mechanical proof (docs/SPEC-SPRINT.md section 6, the mechanical proof).
// Measured on the sprint log of epoch 15: the sprint began with waves of mechanical
// cards (dead-code deletions, renames, assertion rewrites), and each still took a
// model's read. A mechanical card carries a machine proof instead: after the work
// lint (worklint.go, TickLint) and the machine gate (gaterun.go, TickGate) have
// passed, and before any reader is asked, the machine proves the change by the kind
// the card's KIND line names (cardhdr): a deletion proves the named symbols are gone
// and nothing references them, a rename proves no old name is left and the new one
// exists, and an assertion rewrite proves only assertion lines changed. A passing
// proof makes ReadsNeeded zero for that attempt, so the card goes to merge with no
// read; a failing one reworks the attempt at once with the proof's finding as its
// fix, counted toward the card's bound as a broken read is. A card of a
// non-mechanical kind, or a sprint with no proof runner, is read as today.

// The proof's state on a primary.
const (
	ProofGreen = "green" // the proof ran and passed
	ProofRed   = "red"   // the proof ran and refused: the attempt was reworked
)

// The proof's fields on the primary, one set per attempt (FieldProofAttempt).
const (
	FieldProof        = "proof"         // green | red
	FieldProofAttempt = "proof_attempt" // the attempt the state belongs to
	FieldProofReworks = "proof_reworks" // the primary's count of attempts the proof sent back
)

// ProofFinding is one place a mechanical proof refuses: the file and line it names
// (0 when it names none), and what it found.
type ProofFinding struct {
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	What string `json:"what"`
}

// String is the finding as a rework's fix carries it: file:line, then the line.
func (f ProofFinding) String() string {
	at := f.File
	if at != "" && f.Line > 0 {
		at += ":" + strconv.Itoa(f.Line)
	}
	if at != "" {
		at += ": "
	}
	return at + f.What
}

// ProofFinds is the findings as one finding, the same words for the same findings
// at any attempt (the brief's bound compares them: SameFinding).
func ProofFinds(fs []ProofFinding) string {
	parts := make([]string, len(fs))
	for i, f := range fs {
		parts[i] = f.String()
	}
	return "machine proof: " + strings.Join(parts, "; ")
}

// ProofFix is the findings as one fix: the attempt and head the proof refused, then
// each finding.
func ProofFix(attempt int, head string, fs []ProofFinding) string {
	return "the machine proof refused attempt " + itoa(attempt) + " at " + orDash(head) + "; " + ProofFinds(fs)
}

// ProofRun is one attempt's proof as the machine ran it: Passed says the proof ran
// and held; Failed is the findings of a proof that refused. Neither (an empty run)
// is a proof that did not run: the attempt is read as today.
type ProofRun struct {
	Passed bool
	Failed []ProofFinding
}

// ProofRunner runs one finished attempt's mechanical proof and returns what it
// found. It is the machine's, never a model's: the binding builds it over the
// attempt's tree (proof_git.go, NewTreeProof), and the tests pass a fake. A runner
// that is not there (nil) is no proof: every attempt is read as before.
type ProofRunner func(s *Snapshot, pr *Card) ProofRun

// DefaultProof is the proof a tick runs when it runs one: nova-sprint sets it once,
// at its start; nil runs no proof.
var DefaultProof ProofRunner

// proof is the proof the tick runs.
func (r TickReq) proof() ProofRunner { return DefaultProof }

// MechanicalProved says the primary is a mechanical card whose proof passed at its
// current attempt: it needs no read (ReadsNeeded is zero) and goes to merge.
func MechanicalProved(pr *Card) bool {
	if pr == nil || !cardhdr.MechanicalKind(cardhdr.ReadKind(pr.F("brief"))) {
		return false
	}
	return pr.F(FieldProof) == ProofGreen && pr.Int(FieldProofAttempt) == pr.Int("attempt")
}

// proofable says the primary is a finished mechanical attempt the proof holds
// before its first read: in review, its work not failed, no read card at its
// attempt, no change queued for it, and the proof of this attempt not decided yet.
func proofable(s *Snapshot, c *Card, p ProofRunner) bool {
	if p == nil || c.Col != Review || c.F("result") == "failed" {
		return false
	}
	if !cardhdr.MechanicalKind(cardhdr.ReadKind(c.F("brief"))) {
		return false
	}
	if len(readsAt(s, c, c.Int("attempt"))) > 0 || s.Held[c.ID] {
		return false
	}
	return c.Int(FieldProofAttempt) != c.Int("attempt")
}

// proofWords names the kind the proof holds, for its moved line.
func proofWords(c *Card) string {
	return cardhdr.ReadKind(c.F("brief")) + " proved"
}

// TickProof is the mechanical proof as the pump runs it, after the work lint and
// the machine gate and before the readers are asked (proofPart): every proofable
// attempt is proved once by the card's kind. A proof that passes records the state
// on the attempt, so ReadsNeeded is zero for it; a proof that refuses reworks the
// attempt at once with its finding as the fix, its proof reworks and broken reads
// counted one more, and the ask asks no reader of it. ok is false when the proof
// moved nothing.
func TickProof(s *Snapshot, r TickReq) (Plan, bool) {
	p := r.proof()
	if p == nil || s.Work == nil {
		return Plan{}, false
	}
	var plan Plan
	type redProof struct {
		fix, finding string
		set          map[string]string
	}
	reds := map[string]redProof{}
	var redIDs []string
	for _, c := range s.Work.Column(Review) {
		if !proofable(s, c, p) {
			continue
		}
		run := p(s, c)
		attempt := c.Int("attempt")
		set := map[string]string{FieldProofAttempt: itoa(attempt)}
		switch {
		case run.Passed:
			set[FieldProof] = ProofGreen
			plan.Units = append(plan.Units, Unit{Key: c.ID, Stream: c.Row,
				Changes: []Change{change(Work, setEntry(c, set))},
				Moved:   c.ID + " proof green: " + proofWords(c) + " and no read is needed"})
		case len(run.Failed) > 0:
			set[FieldProof] = ProofRed
			set[FieldProofReworks] = itoa(c.Int(FieldProofReworks) + 1)
			set["broken_reads"] = itoa(c.Int("broken_reads") + 1)
			reds[c.ID] = redProof{fix: ProofFix(attempt, c.F("head"), run.Failed), finding: ProofFinds(run.Failed), set: set}
			redIDs = append(redIDs, c.ID)
		}
	}
	if len(redIDs) > 0 {
		per := map[string]ReworkCard{}
		for id, rd := range reds {
			per[id] = ReworkCard{Fix: cutText(rd.fix, MaxCardTextBytes), Finding: cutText(rd.finding, MaxCardTextBytes),
				Why: "the attempt finished and the machine proof refused it", Set: rd.set}
		}
		rp := Rework(s, ReworkReq{Sel: Sel{Only: redIDs}, Who: r.who(), PerCard: per})
		// the green marks of the same pass ride with the rework's plan
		rp.Units = append(rp.Units, plan.Units...)
		rp.Notes = append(rp.Notes, plan.Notes...)
		plan = rp
	}
	if plan.Empty() {
		return Plan{}, false
	}
	return plan, true
}

// proofHeldPrimaries is the primaries in review the proof holds from every ask: a
// mechanical card whose proof for this attempt has not been decided yet. While a
// proof is configured no reader, the friends' or the machine's, is asked of an
// attempt the machine can prove itself.
func proofHeldPrimaries(s *Snapshot, r TickReq) []*Card {
	if r.proof() == nil || s.Work == nil {
		return nil
	}
	var out []*Card
	for _, c := range s.Work.Column(Review) {
		if !cardhdr.MechanicalKind(cardhdr.ReadKind(c.F("brief"))) {
			continue
		}
		if c.Int(FieldProofAttempt) != c.Int("attempt") {
			out = append(out, c)
		}
	}
	return out
}

// hideProofHeld takes the primaries the proof holds out of the work table for the
// duration of the ask, as gateHeldPart does the gate's: no reader is asked of a
// mechanical attempt whose proof has not been decided.
func hideProofHeld(s *Snapshot, r TickReq) func() {
	held := proofHeldPrimaries(s, r)
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

// proofHeldPart wraps the tick's ask with the proof's hold.
func proofHeldPart(ask TickPartFn) TickPartFn {
	return func(s *Snapshot, r TickReq) (Plan, int) {
		restore := hideProofHeld(s, r)
		defer restore()
		return ask(s, r)
	}
}

// proofPart wraps the work pump's accept with the mechanical proof: the lint and
// the gate run first (TickAccept does both), and only when they moved nothing does
// the proof run; a proof that moved something is this part's plan, and the proof of
// the next tick runs the accept on the state it wrote.
func proofPart(accept TickPartFn) TickPartFn {
	return func(s *Snapshot, r TickReq) (Plan, int) {
		p, due := accept(s, r)
		if !p.Empty() || due > 0 {
			return p, due
		}
		if pp, ok := TickProof(s, r); ok {
			return pp, 1
		}
		return p, due
	}
}

// The tick's accept and ask are the functions TickTables holds, so the proof is
// installed there, after the gate's and the lint's own wrappers (their inits run
// before this file's). The accept part is wrapped first so a proofable attempt is
// proved after the lint and the gate; the ask parts are wrapped so a mechanical
// attempt whose proof has not been decided is asked of no reader.
func init() {
	accept := reflect.ValueOf(TickAccept).Pointer()
	wrapped := proofPart(TickAccept)
	for i := range TickTables {
		for j := range TickTables[i].Parts {
			if TickTables[i].Parts[j].Name == "accept" && TickTables[i].Parts[j].Fn != nil {
				TickTables[i].Parts[j].Fn = wrapped
			}
		}
	}
	for i := range TickParts {
		if TickParts[i].Name == "accept" {
			TickParts[i].Fn = wrapped
		}
	}
	for i := range heldParts {
		if heldParts[i] != nil && reflect.ValueOf(heldParts[i]).Pointer() == accept {
			heldParts[i] = wrapped
		}
	}
	var before uintptr
	for i := range TickTables {
		for j := range TickTables[i].Parts {
			if TickTables[i].Parts[j].Name == "ask" && TickTables[i].Parts[j].Fn != nil {
				before = reflect.ValueOf(TickTables[i].Parts[j].Fn).Pointer()
				TickTables[i].Parts[j].Fn = proofHeldPart(TickTables[i].Parts[j].Fn)
			}
		}
	}
	for i := range TickParts {
		if TickParts[i].Name == "ask" {
			TickParts[i].Fn = proofHeldPart(TickParts[i].Fn)
		}
	}
	for i := range heldParts {
		if heldParts[i] != nil && reflect.ValueOf(heldParts[i]).Pointer() == before {
			heldParts[i] = proofHeldPart(heldParts[i])
		}
	}
}

// ProofBudget bounds one attempt's whole proof: reading the tree at the head.
const ProofBudget = 2 * time.Minute

// ProofView is what a mechanical proof reads of one attempt's tree: the diff from
// the merge-base, and the source of every tracked .go file at the head.
type ProofView struct {
	Diff  string
	Files map[string]string
}

// proofFiles is the view's files in path order.
func proofFiles(v ProofView) []string {
	out := make([]string, 0, len(v.Files))
	for p := range v.Files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// WorkProof is the mechanical proof of one attempt's view: the findings, nil when
// the proof holds. p's Kind selects the check; a non-mechanical kind proves
// nothing.
func WorkProof(p cardhdr.Proof, v ProofView) []ProofFinding {
	switch p.Kind {
	case cardhdr.KindDeletion:
		return deletionFindings(p.Old, v)
	case cardhdr.KindRename:
		if len(p.Old) != 1 || p.New == "" {
			return []ProofFinding{{What: "the rename proof names no old and new name"}}
		}
		return renameFindings(p.Old[0], p.New, v)
	case cardhdr.KindAssertion:
		return assertionFindings(v.Diff)
	}
	return nil
}

// deletionFindings is every place the tree still names a symbol a deletion proves
// gone: the symbol's declaration and any reference to it, by go/parser over every
// Go file at the head.
func deletionFindings(names []string, v ProofView) []ProofFinding {
	var out []ProofFinding
	for _, name := range names {
		for _, f := range identPlaces(name, v) {
			out = append(out, ProofFinding{File: f.File, Line: f.Line, What: "the deleted symbol " + name + " is still named here"})
		}
	}
	return out
}

// renameFindings proves a rename: no occurrence of old is left anywhere in the
// tree, and the new name exists.
func renameFindings(old, new string, v ProofView) []ProofFinding {
	var out []ProofFinding
	for _, f := range identPlaces(old, v) {
		out = append(out, ProofFinding{File: f.File, Line: f.Line, What: "the old name " + old + " is still named here"})
	}
	if len(identPlaces(new, v)) == 0 {
		out = append(out, ProofFinding{What: "the new name " + new + " is in no Go file at the head"})
	}
	return out
}

// identPlaces is every place a Go file at the head names the identifier, in file
// and line order: the declaration and every reference alike.
func identPlaces(name string, v ProofView) []ProofFinding {
	var out []ProofFinding
	for _, p := range proofFiles(v) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, p, v.Files[p], parser.SkipObjectResolution)
		if err != nil {
			continue // a file that does not parse is the gate's to refuse
		}
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || id.Name != name {
				return true
			}
			out = append(out, ProofFinding{File: p, Line: fset.Position(id.Pos()).Line})
			return true
		})
	}
	return out
}

// assertionRE is the shape of an assertion line: a testify assertion (assert or
// require), or a testing.T call. An assertion rewrite changes only lines of this
// shape, in a block that holds one.
var assertionRE = regexp.MustCompile(`\b(assert|require)\.[A-Z]|\bt\.(Fatal|Fatalf|Error|Errorf|Fail|FailNow|Skip|Skipf)\b`)

// assertionFindings is every changed block of the diff that holds no assertion
// line: an assertion rewrite proves only assertion lines changed. A block is a run
// of changed lines in a hunk, so a multi-line assertion is one block and passes.
func assertionFindings(diff string) []ProofFinding {
	var out []ProofFinding
	for _, f := range diffcheck.Parse(diff) {
		for _, h := range f.Hunks {
			line := h.NewStart
			for i := 0; i < len(h.Lines); {
				if h.Lines[i][0] == ' ' {
					i, line = i+1, line+1
					continue
				}
				j := i
				first := line
				assertion := false
				for ; j < len(h.Lines) && h.Lines[j][0] != ' '; j++ {
					assertion = assertion || assertionRE.MatchString(h.Lines[j])
					if h.Lines[j][0] == '+' {
						line++
					}
				}
				if !assertion {
					out = append(out, ProofFinding{File: f.New, Line: first, What: "the change rewrites a line that is no assertion: " + strconv.Quote(strings.TrimSpace(h.Lines[i][1:]))})
				}
				i = j
			}
		}
	}
	return out
}
