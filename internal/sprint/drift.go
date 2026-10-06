package sprint

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
)

// The drift facts (docs/SPEC-SPRINT.md section 8, "The coordinator's pass", merge health).
// On 2026-10-04 and 2026-10-05 the base, dev and the side branches drifted apart for hours
// before anyone looked: cards on temporary and dead branches, the live server on a side
// branch, the base red at its tip, the promotion PR stuck. What the forge and the
// repository say of that is read outside the plan (ReadDrift, and the forge's promotion PR
// and whole-tree gate beside it) and recorded by one pure step, DriftRead, on the merge
// table: one copy, the last read, that the coordinator's pass judges (mergeHealthConds).
// The shape is drift-alarms' (nova-tools internal/sprint/drift.go, DriftFacts and
// DriftGate), with the promotion PR and the branches it did not carry.

// PropDriftFacts is the merge table's property: the drift facts as last read (DriftRead),
// JSON; absent or empty is none read.
const PropDriftFacts = "drift_facts"

// The promotion PR's states, each a line of merge health: every state of an open PR.
const (
	PROpen       = "open"
	PRQueued     = "queued"
	PRFailing    = "failing"
	PRConflicted = "conflicted"
)

// PRStates are the promotion PR's states in that order.
var PRStates = []string{PROpen, PRQueued, PRFailing, PRConflicted}

// DriftFacts is what the binding read of the forge and the repository. Base empty is no
// read. A nil Gate is no whole-tree run known; a nil Promotion is no promotion PR open.
type DriftFacts struct {
	Repo    string `json:",omitempty"` // the repository, owner/name
	Base    string `json:",omitempty"` // the sprint base, the branch every stream lands on
	BaseTip string `json:",omitempty"` // the base's tip as fetched
	// Gate is the last gate run at a tip of the base, with its scope.
	Gate *DriftGate `json:",omitempty"`
	// Promotion is the open promotion PR into dev; nil is none open.
	Promotion *PromotionPR `json:",omitempty"`
	// Branches are the branches named by an open card or by the running server
	// (BranchesNamed), each against the base.
	Branches []BranchLag `json:",omitempty"`
	// At is when they were recorded: the step's clock, set by DriftRead.
	At time.Time `json:",omitzero"`
}

// DriftGate is one gate run at a tip of the base: its scope (the whole tree, and the
// functional class tests included), whether it was red, and what failed. Only a run of the
// whole tree with the functional class judges the base: the lander's narrow gate alone
// never does, green or red.
type DriftGate struct {
	Tip        string
	Whole      bool     `json:",omitempty"`
	Functional bool     `json:",omitempty"`
	Red        bool     `json:",omitempty"`
	Failed     []string `json:",omitempty"`
}

// PromotionPR is the open promotion PR: its number and state (PRStates).
type PromotionPR struct {
	Number int
	State  string
}

// BranchLag is one branch against the base: who names it (the cards, the running server),
// its commits ahead of the base and behind it, and Missing when the remote has no such
// branch. A branch with commits ahead, or missing, is not on the base.
type BranchLag struct {
	Branch  string
	Named   string `json:",omitempty"`
	Ahead   int    `json:",omitempty"`
	Behind  int    `json:",omitempty"`
	Missing bool   `json:",omitempty"`
}

// BranchesNamed is each branch other than base named by an open card's BASE: (its base
// field, else its brief's line; a placed primary not landed, not a sentinel, not of the
// promotion stream, of the repository repo when its REPO: names one and repo is given) or
// by the running server (server, "" when not known), by name, with who names it: the work
// ReadDrift measures.
func BranchesNamed(s *Snapshot, repo, base, server string) []BranchLag {
	by := map[string][]string{}
	if s != nil && s.Work != nil {
		for _, c := range s.Work.Cards() {
			if !c.Placed() || IsSentinel(c) || c.Col == Landed || IsPromotionStream(s, c.Row) {
				continue
			}
			brief := c.F("brief")
			if r, ok := cardhdr.Value(brief, "REPO"); ok && repo != "" && r != repo {
				continue
			}
			ref := c.F("base") // add's record of the BASE: line (CardAdd.Base)
			if ref == "" {
				v, ok := cardhdr.Value(brief, "BASE")
				if !ok {
					continue
				}
				if ref, _, ok = cardhdr.ParseBase(v); !ok {
					continue
				}
			}
			if ref == base {
				continue
			}
			by[ref] = append(by[ref], c.ID)
		}
	}
	if server != "" && server != base {
		by[server] = append(by[server], "the running server")
	}
	var out []BranchLag
	for _, b := range slices.Sorted(maps.Keys(by)) {
		named := by[b]
		slices.Sort(named)
		out = append(out, BranchLag{Branch: b, Named: Preview(named, ", ")})
	}
	return out
}

// DriftRead records the drift facts as read: the merge table's PropDriftFacts, stamped
// with the step's clock, over whatever was recorded before; nil facts remove the record.
// A promotion PR in a state not of PRStates, or facts naming no base, are refused.
func DriftRead(s *Snapshot, f *DriftFacts) Plan {
	var p Plan
	was, had := s.Merge.Prop(PropDriftFacts)
	if f == nil {
		if had && was != "" {
			p.Props = append(p.Props, PropWrite{Table: Merge, Name: PropDriftFacts, Value: "", Was: was})
		}
		p.Units = append(p.Units, Unit{Key: "drift-read", Moved: "drift facts: none read"})
		return p
	}
	if f.Base == "" {
		p.refuse("drift-read", "the drift facts name no base")
		return p
	}
	if pr := f.Promotion; pr != nil && !slices.Contains(PRStates, pr.State) {
		p.refuse("drift-read", fmt.Sprintf("the promotion PR's state is one of %s; found %s", strings.Join(PRStates, ", "), orDash(pr.State)))
		return p
	}
	g := *f
	g.At = s.Now.UTC()
	b, err := json.Marshal(g)
	if err != nil {
		p.refuse("drift-read", "the drift facts do not encode: "+err.Error())
		return p
	}
	p.Props = append(p.Props, PropWrite{Table: Merge, Name: PropDriftFacts, Value: string(b), Was: was, WasAbsent: !had})
	p.Units = append(p.Units, Unit{Key: "drift-read", Moved: fmt.Sprintf("drift facts read at %s: %d lines of the promotion PR and the branches", stamp(s.Now), len(driftForgeLines(&g)))})
	return p
}

// RecordedDrift is the drift facts as last recorded (DriftRead); nil when none is, or the
// record does not read.
func RecordedDrift(s *Snapshot) *DriftFacts {
	if s == nil || s.Merge == nil {
		return nil
	}
	v, ok := s.Merge.Prop(PropDriftFacts)
	if !ok || v == "" {
		return nil
	}
	var f DriftFacts
	if json.Unmarshal([]byte(v), &f) != nil || f.Base == "" {
		return nil
	}
	return &f
}

// driftBaseRed is the facts' line of the base red at its tip: a whole-tree run with the
// functional class, red, at the tip the base is at; "" otherwise. A narrow run, or a run at
// an older tip, judges nothing.
func driftBaseRed(f *DriftFacts) string {
	g := f.Gate
	if g == nil || !g.Red || !g.Whole || !g.Functional || g.Tip == "" || g.Tip != f.BaseTip {
		return ""
	}
	return fmt.Sprintf("base %s is red at its tip %s by the whole-tree gate with the functional class: %s", f.Base, shortCommit(g.Tip), orDash(Preview(g.Failed, ", ")))
}

// driftForgeLines are the facts' lines after the base: the promotion PR, then each branch
// not on the base, by name.
func driftForgeLines(f *DriftFacts) []string {
	var lines []string
	if pr := f.Promotion; pr != nil {
		lines = append(lines, fmt.Sprintf("promotion PR #%d is %s", pr.Number, pr.State))
	}
	bs := slices.Clone(f.Branches)
	slices.SortFunc(bs, func(a, b BranchLag) int { return strings.Compare(a.Branch, b.Branch) })
	for _, b := range bs {
		switch {
		case b.Missing:
			lines = append(lines, fmt.Sprintf("branch %s (named by %s) is not on the remote", b.Branch, orDash(b.Named)))
		case b.Ahead > 0:
			lines = append(lines, fmt.Sprintf("branch %s (named by %s) is not on %s: %d ahead, %d behind", b.Branch, orDash(b.Named), f.Base, b.Ahead, b.Behind))
		}
	}
	return lines
}

// shortCommit is a commit's first nine hex digits, or the commit when shorter.
func shortCommit(sha string) string {
	if len(sha) > 9 {
		return sha[:9]
	}
	return sha
}
