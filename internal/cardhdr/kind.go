package cardhdr

// Mechanical card kinds: the KIND: line names one, and the card carries a machine
// proof in place of reads (docs/SPEC-SPRINT.md section 6, the mechanical proof).
// A script makes the change, the proof verifies it, and the machine lands it
// without a model reading it.
const (
	KindDelete  = "delete"            // deletes dead symbols
	KindRename  = "rename"            // renames a symbol at every reference
	KindRewrite = "assertion-rewrite" // rewrites only assertion lines
)

// MechanicalKind returns the kind and true when the brief's KIND line names a
// mechanical card kind that carries a proof in place of reads. A non-mechanical
// kind returns "", false, and the card is read as today.
func MechanicalKind(brief string) (kind string, ok bool) {
	k, found := Value(brief, "KIND")
	if !found {
		return "", false
	}
	switch k {
	case KindDelete, KindRename, KindRewrite:
		return k, true
	}
	return "", false
}
