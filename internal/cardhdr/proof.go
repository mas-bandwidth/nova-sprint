package cardhdr

import (
	"regexp"
	"strconv"
	"strings"
)

// The mechanical kinds (docs/SPEC-SPRINT.md section 6, the mechanical proof): a
// card whose change the machine can prove by a proof of its own, so no reader is
// asked of it. A card of any other kind is read as today.
//
// The kind is named on the card's KIND: line, the operands on its PROOF: line:
//
//	KIND: deletion           PROOF: <name>[,<name>...]
//	KIND: rename             PROOF: <old> <new>
//	KIND: assertion-rewrite  (no operands)
const (
	KindDeletion  = "deletion"
	KindRename    = "rename"
	KindAssertion = "assertion-rewrite"
)

// ProofLine is the card header line that carries a mechanical card's operands, the
// one spelling of it: internal/sprint reads the line through cardhdr.ReadProof.
const ProofLine = "PROOF"

// Proof is a mechanical card's proof: the kind the card's KIND line names, and the
// names its PROOF line gives. Old is the symbols a deletion proves gone; for a
// rename it is the one old name, and New the name that must exist.
type Proof struct {
	Kind string
	Old  []string
	New  string
}

// identRE is a Go identifier as a PROOF operand names it.
var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// MechanicalKind says the kind is one a proof reads.
func MechanicalKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindDeletion, KindRename, KindAssertion:
		return true
	}
	return false
}

// ReadKind is a brief's kind: the first word of its KIND line, lowercased, "" when
// the brief carries no KIND line.
func ReadKind(brief string) string {
	v, _ := Value(brief, "KIND")
	if f := strings.Fields(v); len(f) > 0 {
		return strings.ToLower(f[0])
	}
	return ""
}

// ReadProof reads a brief's PROOF line for the kind its KIND line names. why is ""
// or the one line naming what is wrong and what to write; a brief of a
// non-mechanical kind reads the zero Proof and no why.
func ReadProof(brief string) (Proof, string) {
	p := Proof{Kind: ReadKind(brief)}
	if !MechanicalKind(p.Kind) {
		return p, ""
	}
	v, ok := Value(brief, ProofLine)
	if !ok {
		if p.Kind == KindAssertion {
			return p, "" // no operands: the diff alone is the proof
		}
		return p, "KIND: " + p.Kind + " names no PROOF: line: " + proofRemedy(p.Kind)
	}
	f := strings.Fields(v)
	switch p.Kind {
	case KindDeletion:
		for _, part := range strings.Split(v, ",") {
			name := strings.TrimSpace(part)
			if !identRE.MatchString(name) {
				return p, "PROOF: " + v + " names " + strconv.Quote(name) + ", which is no Go identifier: " + proofRemedy(p.Kind)
			}
			p.Old = append(p.Old, name)
		}
		if len(p.Old) == 0 {
			return p, "PROOF: names no symbol: " + proofRemedy(p.Kind)
		}
	case KindRename:
		if len(f) != 2 || !identRE.MatchString(f[0]) || !identRE.MatchString(f[1]) {
			return p, "PROOF: " + v + " is not `<old> <new>` (two Go identifiers): " + proofRemedy(p.Kind)
		}
		p.Old, p.New = []string{f[0]}, f[1]
	case KindAssertion:
		// no operands: the diff alone is the proof
	}
	return p, ""
}

// proofRemedy is what a mechanical card's PROOF line must carry.
func proofRemedy(kind string) string {
	switch kind {
	case KindDeletion:
		return "write `PROOF: <name>[,<name>...]` naming the symbols that must be gone and unreferenced"
	case KindRename:
		return "write `PROOF: <old> <new>` naming the name that must be gone and the name that must exist"
	case KindAssertion:
		return "write `PROOF: assertion-rewrite` or leave the line out: the kind carries the proof"
	}
	return "write a PROOF line for the mechanical kind"
}
