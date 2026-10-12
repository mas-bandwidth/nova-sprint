package sprint

import (
	"slices"
)

// The protected branches (docs/SPEC-SPRINT.md section 7): the lander never lands on a
// protected branch of a repository. dev and main are protected: every stream lands on its
// own sprint branch (sprint/<stream>), stream branches merge one at a time into dev (main
// where a repository has no dev), and a card never lands on dev or main. The mark
// land_protected, once a landing permission for these branches, is none now: a stream
// carrying one from an old store is held to the same rule, and `--land-protected default`
// (or `--promotion=false`) is the only accepted write of it, which takes the mark off.
const (
	// FieldLandProtected is a stream's control card's field: the repositories, comma
	// separated, whose protected branches the stream was once marked to land on, or
	// LandProtectedAny for every repository; written by `stream set --land-protected` before
	// 2026-10-12 and taken off by `--land-protected default`. It is no landing permission.
	FieldLandProtected = "land_protected"
	// LandProtectedAny is the old mark's word for every repository, a card naming no
	// repository among them.
	LandProtectedAny = "any"
)

// ProtectedBranches is the branches of a repository the lander never lands on: dev, which
// promotion alone reaches, and main, the release branch (docs/SPEC-SPRINT.md section 7).
var ProtectedBranches = []string{"dev", "main"}

// LandingFlow is the landing flow, one sentence (docs/SPEC-SPRINT.md section 7, the sprint
// branch): a card head lands in a batch on its stream's branch, and only the serial merge of
// stream branches reaches dev or main; a card never lands on dev or main itself.
const LandingFlow = "The landing flow: card heads land in a batch on their stream's branch sprint/<stream>, oldest first, one writer per stream; stream branches merge one at a time into dev (main where a repository has no dev); a card never lands on dev or main."

// ProtectedLandingWhy is why a landing whose target is a protected branch (dev or main) is
// refused, "" when base is none of them: it is refused for every stream, whatever the
// stream's mark, since land_protected is no landing permission (docs/SPEC-SPRINT.md section 7,
// the protected branches). The one command that fixes a card meant for such a branch
// re-points the stream's base to its sprint branch.
func ProtectedLandingWhy(s *Snapshot, stream, repo, base string) string {
	if !slices.Contains(ProtectedBranches, base) {
		return ""
	}
	return "stream " + stream + " may not land on " + base + ", a protected branch: " + LandingFlow +
		"; run: nova-sprint stream set " + stream + " --base sprint/" + stream
}

// ProtectedLandWhy is why the lander refuses to land card on base, the card's stream and
// repository given, "" when it may (docs/SPEC-SPRINT.md section 7, the protected branches):
// base is a protected branch, refused for every stream, marked or not (ProtectedLandingWhy).
func ProtectedLandWhy(s *Snapshot, stream, repo, base, card string) string {
	if ProtectedLandingWhy(s, stream, repo, base) == "" {
		return ""
	}
	of := "of " + repo
	if repo == "" {
		of = "of its repository (it names no REPO: line)"
	}
	return "card " + card + " lands on " + base + ", a protected branch " + of + ": " + LandingFlow +
		"; run: nova-sprint stream set " + stream + " --base sprint/" + stream
}

// landProtectedWhy is why a stream may not be marked to land on a protected branch (stream
// set --land-protected <anything>, --promotion, add --land-protected), since the mark is no
// landing permission (docs/SPEC-SPRINT.md section 7, the protected branches). Only default,
// which takes an old mark off, is accepted.
func landProtectedWhy(stream string) string {
	return "stream " + stream + " may not be marked to land on a protected branch: " + LandingFlow +
		"; run: nova-sprint stream set " + stream + " --base sprint/" + stream
}
