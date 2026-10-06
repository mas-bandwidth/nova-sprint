package sprint

import (
	"fmt"
	"slices"
	"strings"
)

// Merge health, a condition of the coordinator's pass (docs/SPEC-SPRINT.md section 8, "The
// coordinator's pass", merge health; the owner, 2026-10-05: "How can we ensure that you
// ALWAYS do the merging properly from now on, vs. drifting and forgetting?", "We really
// need to stop this branch divergence", and "Prevention is better than cure"). One
// judgment about the sprint listing each line of what is wrong, in a fixed order:
//
//   - the base red at its tip: the drift facts' whole-tree gate with the functional class
//     red at the tip the base is at, and each stream the base-gate rule stopped (NBaseRed);
//   - dev behind (DevBehind, DevLag);
//   - the promotion PR open, queued, failing or conflicted, with its number;
//   - each branch named by an open card or by the running server that is not on the base,
//     with its commits ahead and behind (or not on the remote at all).
//
// The store's lines are read from the snapshot; the forge's and the repository's from the
// drift facts the merge table records (RecordedDrift; drift.go), never a second copy. It is
// an episode as the pass's other judgments are (tla/CoordinatorPass.tla: one condition on
// one subject): written once when a line first holds, rewritten in place with the latest
// lines as they move, raised again with a push every PassEvery of running time while any
// line holds, and closed when none does. Its decisions are act and wait: no ack, so it
// cannot be quieted while the branches stay apart, only waited for a while.

// NMergeHealth is the pass's merge-health judgment.
const NMergeHealth = "merge health: the base, dev and the branches are not stitched"

func init() {
	TickDecisions[NMergeHealth] = []string{"act", "wait"}
}

// MergeHealthLines are the lines of merge health that hold now, in the order above. None
// is merge health well.
func MergeHealthLines(s *Snapshot) []string {
	var lines []string
	f := RecordedDrift(s)
	if f != nil {
		if red := driftBaseRed(f); red != "" {
			lines = append(lines, red)
		}
	}
	var stopped []string
	for _, o := range s.Open {
		if o.Note.Kind == Judgment && o.Note.Type == NBaseRed && !slices.Contains(stopped, o.Note.Stream) {
			stopped = append(stopped, o.Note.Stream)
		}
	}
	slices.Sort(stopped)
	for _, st := range stopped {
		lines = append(lines, fmt.Sprintf("stream %s stopped: its base fails its tree gate", st))
	}
	if d, ok := DevBehind(s); ok {
		lines = append(lines, d.Line())
	}
	if f != nil {
		lines = append(lines, driftForgeLines(f)...)
	}
	return lines
}

// mergeHealthConds is the one merge-health condition, about the sprint, while any line
// holds.
func mergeHealthConds(s *Snapshot) []cond {
	lines := MergeHealthLines(s)
	if len(lines) == 0 {
		return nil
	}
	return []cond{{typ: NMergeHealth, streamLevel: true,
		what: fmt.Sprintf("merge health: %s; stitch them: fix the base first, merge the base into each branch or land it, promote into dev (docs/SPEC-SPRINT.md section 7)",
			strings.Join(lines, "; ")),
		decisions: TickDecisions[NMergeHealth]}}
}
