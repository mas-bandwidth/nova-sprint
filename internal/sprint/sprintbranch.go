package sprint

import "strings"

// Every stream lands on the sprint branch, and promotion alone reaches dev
// (docs/SPEC-SPRINT.md section 7, the sprint branch): a card cut on dev is merged by
// the lander straight onto it and ejects the merge queue's promotion run, and a card cut
// on any other branch lands there and drifts from the base (found 2026-10-04: cards on
// a temporary integration branch and on personal ones, folded back by hand through 17
// conflicts), so add admits a card whose BASE is not the sprint's base only into the
// promotion stream, and the lander refuses a landing on dev or main outside it
// (ProtectedLandWhy, the same mark).

// DevBranch is the development branch, which promotion alone reaches.
const DevBranch = "dev"

// IsPromotionStream says the stream is the promotion stream: its control card carries
// the mark FieldLandProtected, which only the coordinator's `nova-sprint stream set <s>
// --land-protected <owner/name,...|any>` writes (Set) and `--land-protected default`
// takes off (docs/SPEC-SPRINT.md section 7, the sprint branch).
func IsPromotionStream(s *Snapshot, stream string) bool {
	return s.StreamCtl(stream).F(FieldLandProtected) != ""
}

// SprintBranchPrefix opens the name of a sprint branch, sprint/<name>: the sprint's base,
// the branch every stream lands on (docs/SPEC-SPRINT.md section 7, the sprint branch).
const SprintBranchPrefix = "sprint/"

// IsSprintBranch says base is a sprint branch, sprint/<name>, the sprint's base.
func IsSprintBranch(base string) bool {
	return strings.HasPrefix(base, SprintBranchPrefix) && len(base) > len(SprintBranchPrefix)
}

// SprintBranchWhy is why add (and brief, when it changes the BASE) refuses card into
// stream, "" when it may: its BASE names a branch that is not the sprint's base, a sprint
// branch sprint/<name> (dev, main, a temporary integration branch, a personal or a dead
// one alike), and the stream is not the promotion stream (docs/SPEC-SPRINT.md section 7,
// the sprint branch). A card naming no BASE lands on the lander's --base. The remedy
// re-cuts the card on the sprint's base, or marks the stream.
func SprintBranchWhy(s *Snapshot, stream, base, card string) string {
	if base == "" || IsSprintBranch(base) || IsPromotionStream(s, stream) {
		return ""
	}
	return "card " + card + " is cut on " + base + ", and stream " + stream + " is not the promotion stream: every stream lands on the sprint branch, and promotion alone reaches " + DevBranch +
		"; " + base + " is not the sprint's base, a sprint branch " + SprintBranchPrefix + "<name>" +
		"; nothing was written; re-cut the card with BASE: <the sprint base> (" + SprintBranchPrefix + "<name>, the branch its stream lands on), or, for the promotion stream, run: nova-sprint stream set " + stream + " --land-protected <owner/name,...|any>"
}
