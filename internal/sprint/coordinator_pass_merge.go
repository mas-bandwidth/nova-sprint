package sprint

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Merge health, the pass's fourth condition (docs/SPEC-SPRINT.md section 8, "The
// coordinator's pass", merge health; the owner, 2026-10-05: "How can we ensure that you
// ALWAYS do the merging properly from now on, vs. drifting and forgetting?", and "We really
// need to stop this branch divergence"). One judgment about the sprint listing what is
// wrong of: the base red at its tip, dev behind (DevBehind), the promotion PR's state, and
// the branches not on the base. It is an episode as the pass's others are: raised once,
// rewritten in place with the latest lines, raised again every PassEvery of running time
// while any line holds, closed when none does (tla/CoordinatorPass.tla: one condition on
// one subject, its lines the facts it is rewritten with).
//
// Dev behind and a stream the base-gate rule stopped (NBaseRed) are the store's facts and
// read here; the base's tip, the promotion PR and the branches are the forge's and the
// repository's, recorded on the merge table by MergeRead (PropMergeFacts) from the facts
// the drift alarms compute, and read by the pass from there: one copy, the last read. The
// pass never reads git or the forge itself.

// NMergeHealth is the pass's merge-health judgment.
const NMergeHealth = "merge health: the base, dev and the branches are not stitched"

// The promotion PR's states that are a line of merge health: every state of an open PR.
const (
	PROpen       = "open"
	PRQueued     = "queued"
	PRFailing    = "failing"
	PRConflicted = "conflicted"
)

// PropMergeFacts is the merge table's property: the merge's facts as last read (MergeRead),
// JSON; absent is none read.
const PropMergeFacts = "merge_facts"

// MergeFacts is what the forge and the repository say of the merge, as the drift alarms
// read it; nil is none read.
type MergeFacts struct {
	Base    string `json:",omitempty"` // the base branch
	BaseSha string `json:",omitempty"` // its tip
	// BaseRed is why the base's tip fails its gate; "" while it is green or not known.
	BaseRed string `json:",omitempty"`
	// Promotion is the open promotion PR into dev; nil is none open.
	Promotion *PromotionPR `json:",omitempty"`
	// Branches are the branches named by an open card or by the running server, each with
	// its commits ahead of the base and behind it.
	Branches []BranchLag `json:",omitempty"`
	// At is when they were read: the step's clock, set by MergeRead.
	At time.Time `json:",omitzero"`
}

// PromotionPR is the open promotion PR: its number and state (PROpen, PRQueued, PRFailing,
// PRConflicted).
type PromotionPR struct {
	Number int
	State  string
}

// BranchLag is one branch against the base: who names it (a card, the server), and its
// commits ahead and behind. A branch with commits ahead is not on the base.
type BranchLag struct {
	Branch string
	Named  string `json:",omitempty"`
	Ahead  int    `json:",omitempty"`
	Behind int    `json:",omitempty"`
}

// MergeReadReq is a read of the merge's facts to record: nil Facts records none read.
type MergeReadReq struct {
	Facts *MergeFacts `json:",omitempty"`
	Who   string
}

// MergeRead records the merge's facts as read (the drift alarms' binding, with each of its
// reads): the merge table's PropMergeFacts, stamped with the step's clock, over whatever
// was recorded before; nil Facts removes the record. The pass reads the record with every
// tick (RecordedMerge); a promotion PR in a state not of an open PR is refused.
func MergeRead(s *Snapshot, r MergeReadReq) Plan {
	var p Plan
	was, had := s.Merge.Prop(PropMergeFacts)
	if r.Facts == nil {
		if had {
			p.Props = append(p.Props, PropWrite{Table: Merge, Name: PropMergeFacts, Value: "", Was: was})
		}
		p.Units = append(p.Units, Unit{Key: "merge-read", Moved: "merge facts: none read"})
		return p
	}
	if pr := r.Facts.Promotion; pr != nil && !slices.Contains([]string{PROpen, PRQueued, PRFailing, PRConflicted}, pr.State) {
		p.refuse("merge-read", fmt.Sprintf("the promotion PR's state is one of %s, %s, %s, %s; found %s", PROpen, PRQueued, PRFailing, PRConflicted, orDash(pr.State)))
		return p
	}
	f := *r.Facts
	f.At = s.Now.UTC()
	b, err := json.Marshal(f)
	if err != nil {
		p.refuse("merge-read", "the merge facts do not encode: "+err.Error())
		return p
	}
	p.Props = append(p.Props, PropWrite{Table: Merge, Name: PropMergeFacts, Value: string(b), Was: was, WasAbsent: !had})
	said := "base green"
	if f.BaseRed != "" {
		said = "base red"
	}
	p.Units = append(p.Units, Unit{Key: "merge-read", Moved: fmt.Sprintf("merge facts read at %s: %s, %d more lines", stamp(s.Now), said, len(forgeLines(&f)))})
	return p
}

// RecordedMerge is the merge's facts as last recorded (MergeRead); nil when none is, or the
// record does not read.
func RecordedMerge(s *Snapshot) *MergeFacts {
	if s.Merge == nil {
		return nil
	}
	v, ok := s.Merge.Prop(PropMergeFacts)
	if !ok || v == "" {
		return nil
	}
	var f MergeFacts
	if json.Unmarshal([]byte(v), &f) != nil {
		return nil
	}
	return &f
}

// MergeHealthLines are the lines of merge health that hold now, in a fixed order: the base
// red, each stream the base-gate rule stopped, dev behind, the promotion PR, then the
// branches not on the base by name. None is merge health well. m is the merge's facts as
// recorded (RecordedMerge); nil is none read.
func MergeHealthLines(s *Snapshot, m *MergeFacts) []string {
	var lines []string
	if m != nil && m.BaseRed != "" {
		lines = append(lines, fmt.Sprintf("base %s is red at %s: %s", baseName(m), orDash(m.BaseSha), m.BaseRed))
	}
	var stopped []string
	seen := map[string]bool{}
	for _, o := range s.Open {
		if o.Note.Kind == Judgment && o.Note.Type == NBaseRed && !seen[o.Note.Stream] {
			seen[o.Note.Stream] = true
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
	if m != nil {
		lines = append(lines, forgeLines(m)...)
	}
	return lines
}

// baseName is the base the facts name, "the base" when they name none.
func baseName(m *MergeFacts) string {
	if m.Base != "" {
		return m.Base
	}
	return "the base"
}

// forgeLines are the facts' lines after the base: the promotion PR, then the branches not
// on the base by name.
func forgeLines(m *MergeFacts) []string {
	var lines []string
	if m.Promotion != nil {
		lines = append(lines, fmt.Sprintf("promotion PR #%d is %s", m.Promotion.Number, orDash(m.Promotion.State)))
	}
	bs := slices.Clone(m.Branches)
	slices.SortFunc(bs, func(a, b BranchLag) int { return strings.Compare(a.Branch, b.Branch) })
	for _, b := range bs {
		if b.Ahead <= 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("branch %s (%s) is not on %s: %d ahead, %d behind", b.Branch, orDash(b.Named), baseName(m), b.Ahead, b.Behind))
	}
	return lines
}

// mergeHealthConds is the one merge-health condition while any line holds.
func mergeHealthConds(s *Snapshot, r TickReq) []cond {
	lines := MergeHealthLines(s, RecordedMerge(s))
	if len(lines) == 0 {
		return nil
	}
	return []cond{{typ: NMergeHealth, streamLevel: true,
		what: fmt.Sprintf("merge health: %s; stitch them: merge the base into each branch or land it, fix the base, promote into dev (docs/SPEC-SPRINT.md section 7)",
			strings.Join(lines, "; ")),
		decisions: TickDecisions[NMergeHealth]}}
}
