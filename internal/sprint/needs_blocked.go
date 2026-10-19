package sprint

import (
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// tickCountBlockersWalk counts calls of the Blockers walk (tick_memo_complexity_test.go):
// a package-level counter, a few ns each, no build tag. tickCountUnresolvedNeeds counts
// the walk's UnresolvedNeeds calls; needs_resolve.go is not this card's to touch, so the
// counter sits on the walk's call site here.
var (
	tickCountBlockersWalk    int64
	tickCountUnresolvedNeeds int64
)

// tickCountEnabled gates the counters: the complexity test sets it around its measured
// tick, so the parallel equivalence test's ticks (which read the same package counters)
// cannot disturb the counts. The load is a few ns.
var tickCountEnabled int32

func tickCount(p *int64) {
	if atomic.LoadInt32(&tickCountEnabled) != 0 {
		atomic.AddInt64(p, 1)
	}
}

// tickUnresolvedNeeds is UnresolvedNeeds with the tick counter (the walk's call site).
func tickUnresolvedNeeds(s *Snapshot, id string, needs []string, adding map[string]bool) []UnresolvedNeed {
	tickCount(&tickCountUnresolvedNeeds)
	return UnresolvedNeeds(s, id, needs, adding)
}

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
	tickCount(&tickCountBlockersWalk)
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
	// The needs are split once per card and indexed by the need they name, in the
	// unlanded cards' order: the queue walk below then visits only the cards that need
	// the current blocker, instead of splitting every card's needs for every blocker.
	// The answers and the dependents' order are the scan's.
	needsOf := make(map[*Card][]string, len(unlandedCards))
	needers := map[string][]*Card{}
	blockerSet := map[string]bool{}
	for _, c := range unlandedCards {
		needs := Split(c.F("needs"))
		needsOf[c] = needs
		if len(needs) == 0 {
			continue
		}
		waived := Split(c.F("waived"))
		seen := map[string]bool{}
		for _, n := range needs {
			if seen[n] || contains(waived, n) {
				continue
			}
			seen[n] = true
			needers[n] = append(needers[n], c)
		}
	}
	for _, c := range unlandedCards {
		unres := tickUnresolvedNeeds(s, c.ID, needsOf[c], nil)
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

			// Forward needs: any unlanded card needing curr, in unlanded order.
			for _, c := range needers[curr] {
				if visited[c.ID] {
					continue
				}
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

// tickBlockers is Blockers(s) memoized on the snapshot: a tick's quietedByBlocker calls it
// for every judgment, and the first call walks the table once and the rest read the memo.
// The memo is per-snapshot and is reset wherever the snapshot is copied with a changed Work
// or Fleet (newHeld), so it never describes tables the snapshot does not hold. Blockers is a
// pure function of the snapshot (tla/NeedsBlocked.tla BlockedDependentsCovered), so the memo
// changes no decision.
func (s *Snapshot) tickBlockers() []Blocker {
	if s == nil {
		return nil
	}
	if s.Work == nil {
		if !s.blockersSet {
			s.blockersMemo, s.blockersSet = Blockers(s), true
		}
		return s.blockersMemo
	}
	// the work table's change count is the cache's key: a card put, dropped or
	// changed in place invalidates the memo even when the snapshot is not copied
	puts := s.Work.changes()
	if !s.blockersSet || s.blockersWorkPuts != puts {
		s.blockersMemo, s.blockersSet, s.blockersWorkPuts = Blockers(s), true, puts
	}
	return s.blockersMemo
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
				// The server-default answer to an unanswered bound judgment holds the card
				// by its fields (held, held_by, held_judgment) while closing that judgment,
				// so the blocker retires with no open holding judgment although the card is
				// still held.
				if IsHeld(c) {
					by := ""
					if hb := c.F(FieldHeldBy); hb != "" {
						by = " by " + hb
					}
					why = subject + " is still held" + by + " and no open judgment holds it: nova-sprint release " + subject + " deals it again"
				} else {
					why = subject + " is unheld"
				}
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
