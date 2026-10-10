package sprint

import (
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
)

// The mechanical proof (docs/SPEC-SPRINT.md section 6, the mechanical proof):
// after the work lint (TickLint) and the machine gate (TickGate) pass and
// before any reader is asked, a mechanical card (KIND: delete, rename or
// assertion-rewrite) is run once through its proof. A passing proof makes
// ReadsNeeded zero for that attempt no reader is asked of it, and the card is
// acceptable at once; a failing proof reworks the attempt with the proof's
// findings (go/parser over the tree), counted toward the card's bound as a
// broken read is. A card whose kind is not mechanical is read as today.

// The proof's fields on the primary, one set per attempt (FieldProofAttempt).
const (
	FieldProof         = "proof"          // pass | fail | waiting
	FieldProofAttempt  = "proof_attempt"  // the attempt the state belongs to
	FieldProofLines    = "proof_lines"    // a passing proof's lines
	FieldProofFindings = "proof_findings" // a failing or waiting proof's reason
	FieldProofHost     = "proof_host"     // the host that ran the proof
	FieldProofReworks  = "proof_reworks"  // the primary's count of attempts the proof sent back
)

// The proof's states.
const (
	ProofPass    = "pass"
	ProofFail    = "fail"
	ProofWaiting = "waiting"
)

// NProofDown is the judgment a proof that could not run raises: the attempt
// waits, no reader is asked of it, and the coordinator is told.
const NProofDown = "the mechanical proof could not run"

// ProofFinding is one finding of a failing proof: the file and line (0 when
// it names none) and the line itself.
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

// ProofFinds is the findings as one finding, the same words for the same
// findings at any attempt (the brief's bound compares them: SameFinding).
func ProofFinds(fs []ProofFinding) string {
	parts := make([]string, len(fs))
	for i, f := range fs {
		parts[i] = f.String()
	}
	return "mechanical proof: " + strings.Join(parts, "; ")
}

// ProofFix is the findings as one fix: the attempt and head the proof refused,
// then each finding.
func ProofFix(attempt int, head string, fs []ProofFinding) string {
	return "the mechanical proof refused attempt " + itoa(attempt) + " at " + orDash(head) + "; " + ProofFinds(fs)
}

// ProofRun is the result of one attempt's proof: Kind is the mechanical kind
// it ran for, Host the host that ran it, Lines a passing proof's lines, Failed
// the failing findings, and Waiting why no proof ran.
type ProofRun struct {
	Kind    string
	Host    string
	Lines   []string
	Failed  []ProofFinding
	Waiting string
}

// ProofRunner runs one mechanical card's proof: it clones the repository, reads
// its view (WorkView), and returns what the proof found. It is the machine's,
// never a model's: the binding builds it the same way the gate's is built
// (cmd/nova-sprint/gaterun.go), and the tests pass a fake. A runner that is
// not there (nil) is no proof: every mechanical card is asked as before.
type ProofRunner func(s *Snapshot, pr *Card) ProofRun

// DefaultProof is the proof a tick runs when its request names none
// (TickReq.Proof): nova-sprint sets it once, at its start, from
// NOVA_SPRINT_PROOF_BENCH; nil runs no proof, and every mechanical card is read
// as today.
var DefaultProof ProofRunner

func (r TickReq) proof() ProofRunner {
	if r.Proof != nil {
		return r.Proof
	}
	return DefaultProof
}

// proofable says the primary is a finished attempt the proof holds before its
// first read: in review, its work not failed, no read card at its attempt, no
// change queued, a mechanical kind whose gate passed green for this attempt,
// and the proof of this attempt not decided yet.
func proofable(s *Snapshot, c *Card, p ProofRunner) bool {
	if p == nil || c.Col != Review || c.F("result") == "failed" {
		return false
	}
	if len(readsAt(s, c, c.Int("attempt"))) > 0 || s.Held[c.ID] {
		return false
	}
	if _, ok := cardhdr.MechanicalKind(c.F("brief")); !ok {
		return false
	}
	if c.F(FieldGate) != GateGreen || c.Int(FieldGateAttempt) != c.Int("attempt") {
		return false
	}
	return c.Int(FieldProofAttempt) != c.Int("attempt")
}

// TickProof is the mechanical proof as the pump runs it, after the machine gate
// (TickGate) and before the accept (TickAccept): every mechanical card in
// review whose gate passed green is run through its proof once. A failing
// proof is reworked at once with the proof's findings as its fix and its
// finding, its proof_reworks and broken_reads counted one more; a green one
// records the proof's lines on the attempt for the accept, and the card is
// acceptable with ReadsNeeded zero at once; a proof no host answered leaves the
// attempt in review with one judgment, ok is false when the proof moved
// nothing.
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
	var waiting []*Card
	for _, c := range s.Work.Column(Review) {
		if !proofable(s, c, p) {
			continue
		}
		run := p(s, c)
		attempt := c.Int("attempt")
		set := map[string]string{FieldProofAttempt: itoa(attempt)}
		switch {
		case run.Waiting != "":
			set[FieldProof] = ProofWaiting
			set[FieldProofFindings] = cutText(run.Waiting, MaxCardTextBytes)
			plan.Units = append(plan.Units, Unit{Key: c.ID, Stream: c.Row,
				Changes: []Change{change(Work, setEntry(c, set))},
				Moved:   c.ID + " proof waits: " + run.Waiting})
			waiting = append(waiting, c)
		case len(run.Failed) > 0:
			set[FieldProof] = ProofFail
			set[FieldProofReworks] = itoa(c.Int(FieldProofReworks) + 1)
			set["broken_reads"] = itoa(c.Int("broken_reads") + 1)
			set[FieldProofFindings] = cutText(ProofFinds(run.Failed), MaxCardTextBytes)
			reds[c.ID] = redProof{fix: ProofFix(attempt, c.F("head"), run.Failed), finding: ProofFinds(run.Failed), set: set}
			redIDs = append(redIDs, c.ID)
		default:
			set[FieldProof] = ProofPass
			set[FieldProofLines] = cutText(strings.Join(run.Lines, "\n"), MaxCardTextBytes)
			if run.Host != "" {
				set[FieldProofHost] = run.Host
			}
			plan.Units = append(plan.Units, Unit{Key: c.ID, Stream: c.Row,
				Changes: []Change{change(Work, setEntry(c, set))},
				Moved:   c.ID + " proof pass on " + orDash(run.Host)})
		}
	}
	if len(redIDs) > 0 {
		per := map[string]ReworkCard{}
		for id, rd := range reds {
			per[id] = ReworkCard{Fix: cutText(rd.fix, MaxCardTextBytes), Finding: cutText(rd.finding, MaxCardTextBytes),
				Why: "the attempt finished and the mechanical proof refused it", Set: rd.set}
		}
		rp := Rework(s, ReworkReq{Sel: Sel{Only: redIDs}, Who: r.who(), PerCard: per})
		rp.Units = append(rp.Units, plan.Units...)
		rp.Notes = append(rp.Notes, plan.Notes...)
		plan = rp
	}
	for _, c := range waiting {
		n := judgment(NProofDown, c.Row, s.Now, 0, c.ID)
		n.Who, n.Attempt = r.who(), c.Int("attempt")
		n.Decisions = []string{"wait", "rework", "drop"}
		n.What = "the mechanical proof could not run for attempt " + itoa(c.Int("attempt")) + ": " + c.F(FieldProofFindings) + "; the attempt waits, asked of no reader"
		plan.Notes = append(plan.Notes, n)
	}
	if len(plan.Units) == 0 && len(plan.Notes) == 0 {
		return Plan{}, false
	}
	return plan, true
}

// proofLines is a passing proof's recorded lines as the read packet carries
// them, without empty items.
func proofLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// gateProofLines returns a green gate's lines plus the proof's, the one list
// the read packet carries when both passed.
func gateProofLines(c *Card) []string {
	var out []string
	for _, l := range GateLines(c.F(FieldGateLines)) {
		out = append(out, l)
	}
	if c.F(FieldProof) == ProofPass {
		out = append(out, proofLines(c.F(FieldProofLines))...)
	}
	return out
}

// HasProof says c's attempt was proved by the machine: a passing proof for its
// current attempt.
func HasProof(c *Card) bool {
	return c.F(FieldProof) == ProofPass && c.Int(FieldProofAttempt) == c.Int("attempt")
}
