package sprint

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// NNeedBlocked is the judgment raised when an unlanded card's need is held, dropped or absent.
const NNeedBlocked = "a need is blocked: its dependents cannot move"

// Blocker is a card that blocks unlanded dependents.
type Blocker struct {
	ID         string
	Card       *Card
	Dependents []Dependent
}

// Dependent is an unlanded card waiting on a blocker.
type Dependent struct {
	ID     string
	Stream string
}

func holdingWord(typ string) string {
	switch typ {
	case NBound:
		return "bound"
	case NBriefWrong:
		return "brief-wrong"
	case NHarnessFault:
		return "harness-fault"
	default:
		return typ
	}
}

func shortAge(d time.Duration) string {
	s := d.Round(time.Minute).String()
	s = strings.TrimSuffix(s, "0s")
	if strings.HasSuffix(s, "0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	if s == "" {
		return "0m"
	}
	return s
}

// Blockers computes all blockers and their transitive dependents in s.
//
// tla/NeedsBlocked.tla BlockedDependentsCovered
func Blockers(s *Snapshot) []Blocker {
	if s == nil || s.Work == nil {
		return nil
	}
	// 1. Gather all unlanded cards in table order (columns then score).
	var unlandedCards []*Card
	for _, st := range []State{Waiting, Ready, Working, Review, Merging} {
		for _, c := range s.Work.Column(st) {
			if !IsSentinel(c) {
				unlandedCards = append(unlandedCards, c)
			}
		}
	}
	for _, c := range s.Work.Cards() {
		if IsSentinel(c) && c.Placed() && c.Col != Landed {
			unlandedCards = append(unlandedCards, c)
		}
	}

	// 2. Discover blockers: any need named by an unlanded card that UnresolvedNeeds reports held, dropped or absent.
	blockerSet := map[string]bool{}
	for _, c := range unlandedCards {
		unres := UnresolvedNeeds(s, c.ID, Split(c.F("needs")), nil)
		for _, u := range unres {
			if u.Reason == UnresolvedHeld || u.Reason == UnresolvedDropped || u.Reason == UnresolvedAbsent {
				blockerSet[u.Need] = true
			}
		}
	}
	if len(blockerSet) == 0 {
		return nil
	}

	blockerIDs := make([]string, 0, len(blockerSet))
	for id := range blockerSet {
		blockerIDs = append(blockerIDs, id)
	}
	slices.Sort(blockerIDs)

	var blockers []Blocker
	for _, bID := range blockerIDs {
		visited := map[string]bool{bID: true}
		queue := []string{bID}
		var depList []Dependent

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]

			// Sentinels: if curr is a sentinel, cards Behind(s, curr) are blocked.
			if currCard := s.Work.Card(curr); currCard != nil && IsSentinel(currCard) {
				for _, bh := range Behind(s, currCard) {
					if bh.Placed() && bh.Col != Landed && !visited[bh.ID] {
						visited[bh.ID] = true
						queue = append(queue, bh.ID)
						if !IsSentinel(bh) {
							depList = append(depList, Dependent{ID: bh.ID, Stream: bh.Row})
						}
					}
				}
			}

			// Forward needs: any unlanded card needing curr.
			for _, c := range unlandedCards {
				if visited[c.ID] {
					continue
				}
				needs := Split(c.F("needs"))
				waived := Split(c.F("waived"))
				if contains(needs, curr) && !contains(waived, curr) {
					visited[c.ID] = true
					queue = append(queue, c.ID)
					if !IsSentinel(c) {
						depList = append(depList, Dependent{ID: c.ID, Stream: c.Row})
					}
					if IsSentinel(c) {
						for _, bh := range Behind(s, c) {
							if bh.Placed() && bh.Col != Landed && !visited[bh.ID] {
								visited[bh.ID] = true
								queue = append(queue, bh.ID)
								if !IsSentinel(bh) {
									depList = append(depList, Dependent{ID: bh.ID, Stream: bh.Row})
								}
							}
						}
					}
				}
			}
		}

		if len(depList) > 0 {
			blockers = append(blockers, Blocker{
				ID:         bID,
				Card:       s.Work.Card(bID),
				Dependents: depList,
			})
		}
	}
	return blockers
}

// CoveredDependents returns a map of dependent ID -> blocker's judgment ID or alias (or blocker ID)
// for all unlanded cards that are covered by a blocker.
//
// tla/NeedsBlocked.tla BlockedDependentsCovered
func CoveredDependents(s *Snapshot) map[string]string {
	covered := map[string]string{}
	blockers := Blockers(s)
	if len(blockers) == 0 {
		return covered
	}
	openJudgments := map[string]string{}
	for _, o := range s.Open {
		if o.Note.Kind == Judgment && o.Note.Type == NNeedBlocked {
			sub := o.Subject()
			openJudgments[sub] = noteName(&o.Note)
		}
	}
	for _, b := range blockers {
		jid, open := openJudgments[b.ID]
		if !open {
			continue
		}
		for _, dep := range b.Dependents {
			covered[dep.ID] = jid
		}
	}
	return covered
}

// TickNeedBlocked raises and retires NNeedBlocked judgments.
//
// tla/NeedsBlocked.tla BlockedDependentsCovered
func TickNeedBlocked(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	blockers := Blockers(s)
	blockerMap := map[string]Blocker{}
	for _, b := range blockers {
		blockerMap[b.ID] = b
	}

	openBlockers := map[string]Open{}
	for _, o := range s.Open {
		if o.Note.Kind == Judgment && o.Note.Type == NNeedBlocked {
			openBlockers[o.Subject()] = o
		}
	}

	// 1. Retire
	for subject, o := range openBlockers {
		b, stillBlocked := blockerMap[subject]
		if !stillBlocked || len(b.Dependents) == 0 {
			p.Closes = append(p.Closes, o)
			why := "no dependents remain"
			c := s.Work.Card(subject)
			if c != nil && c.Col == Landed {
				why = subject + " landed"
			} else if c != nil && c.Placed() && needHolder(s, subject) == nil {
				why = subject + " is unheld"
			}
			p.Notes = append(p.Notes, decided(o, why, MachineActor, s.Now, subject))
		}
	}

	// 2. Raise or update in place
	for _, b := range blockers {
		if len(b.Dependents) == 0 {
			continue
		}
		var depStrs []string
		for _, dep := range b.Dependents {
			depStrs = append(depStrs, fmt.Sprintf("%s (%s)", dep.ID, dep.Stream))
		}
		depList := strings.Join(depStrs, ", ")

		var stateStr string
		var decisions []string
		h := needHolder(s, b.ID)
		if h != nil {
			hw := holdingWord(h.Type)
			stateStr = fmt.Sprintf("%s: %s %s, open %s", hw, noteName(h), hw, shortAge(s.Now.Sub(h.At)))
			if len(h.Decisions) > 0 {
				decisions = append([]string(nil), h.Decisions...)
			} else {
				decisions = []string{ReworkOnAHigherTier, "drop"}
			}
		} else {
			if b.Card != nil && !b.Card.Placed() {
				stateStr = "dropped"
			} else {
				stateStr = "absent"
			}
			decisions = []string{"nova-sprint needs set <dep> --needs <rest>", "drop <dep> --cascade"}
		}

		what := fmt.Sprintf("%s (%s) blocks %d cards: %s; decide: %s", b.ID, stateStr, len(b.Dependents), depList, strings.Join(decisions, ", "))

		stream := ""
		if b.Card != nil && b.Card.Row != "" {
			stream = b.Card.Row
		} else if len(b.Dependents) > 0 {
			stream = b.Dependents[0].Stream
		}

		if o, alreadyOpen := openBlockers[b.ID]; alreadyOpen {
			if o.Note.What != what || !slices.Equal(o.Note.Decisions, decisions) {
				updated := o.Note
				updated.What = what
				updated.Decisions = append([]string(nil), decisions...)
				p.Updates = append(p.Updates, updated)
			}
		} else {
			n := Note{
				Kind:      Judgment,
				Type:      NNeedBlocked,
				Stream:    stream,
				Primaries: []string{b.ID},
				Count:     1,
				At:        s.Now,
				Marked:    true,
				What:      what,
				Decisions: decisions,
			}
			p.Notes = append(p.Notes, n)
		}
	}

	return p, len(p.Notes)
}
