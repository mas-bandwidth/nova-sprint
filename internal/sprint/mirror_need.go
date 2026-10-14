package sprint

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// MirrorNeed is one repository's need of a bench mirror: the members that lack
// the mirror (they refused the repository at staging for want of a bench mirror,
// mirrorRefusals), and when the need was raised. One row per repository.
// docs/SPEC-SPRINT.md, the mirror need.
type MirrorNeed struct {
	Repo    string
	Members []string
	Raised  time.Time
}

// mirrorNeeds is the mirror need of every repository a member refused at staging for want
// of a bench mirror within StagingMirrorWindow (mirrorRefusals): the members that lack it,
// sorted, and the earliest raised time. The need is served by running the fleet's mirror
// refresh for that repository; once served (the refusal lapses or the member finishes the
// repository ok) the need is gone (docs/SPEC-SPRINT.md, the mirror need).
func mirrorNeeds(s *Snapshot) []MirrorNeed {
	if s == nil {
		return nil
	}
	repos := map[string]*MirrorNeed{}
	for member, rs := range mirrorRefusals(s) {
		for repo, at := range rs {
			n := repos[repo]
			if n == nil {
				n = &MirrorNeed{Repo: repo, Raised: at}
				repos[repo] = n
			}
			n.Members = append(n.Members, member)
			if at.Before(n.Raised) {
				n.Raised = at
			}
		}
	}
	if len(repos) == 0 {
		return nil
	}
	keys := slices.Sorted(maps.Keys(repos))
	out := make([]MirrorNeed, 0, len(keys))
	for _, r := range keys {
		n := repos[r]
		slices.Sort(n.Members)
		out = append(out, *n)
	}
	return out
}

// mirrorLackingMembers is the members that lack a bench mirror of repo: the members up.
// A member whose beat names the mirror it carries is not among them; until the fleet's
// beats carry the mirrors they hold (the card every-bench-carries-every-mirror-a-card-can-name,
// nova-tools), every up member is taken to lack a repository the sprint has only just
// named (docs/SPEC-SPRINT.md, the mirror need).
func mirrorLackingMembers(s *Snapshot, repo string) []string {
	if s == nil {
		return nil
	}
	return s.UpMembers()
}

// MirrorNotice is add's MIRROR line for each repository the admitted cards name and no
// member carries: "MIRROR <repo> needed on <members>", one line per repository, its
// members sorted. The repository is recorded on the stream's control card
// (FieldRepo, by sprint.Add); this line is what tells the seat to run the fleet's mirror
// refresh for it, the moment the sprint knows the repository is needed (docs/SPEC-SPRINT.md,
// the mirror need).
func MirrorNotice(s *Snapshot, repos []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range repos {
		name := RepoName(r)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		members := mirrorLackingMembers(s, name)
		if len(members) == 0 {
			continue
		}
		sorted := append([]string(nil), members...)
		slices.Sort(sorted)
		out = append(out, fmt.Sprintf("MIRROR %s needed on %s", name, strings.Join(sorted, ", ")))
	}
	return out
}
