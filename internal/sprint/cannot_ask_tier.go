package sprint

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// NNoReaderForTier is the tick's judgment that a primary in review can never be
// asked its read: no reader row of the roster, whatever its state, serves the
// primary's read tier besides the reader of the member that worked the attempt.
// A reader freeing or coming up cannot change the roster's tiers, so this wait
// takes no server re-ask default: it is one judgment per read tier naming every
// such primary, and its exit is the owner's decision (reader add, raise the read
// tier, or accept with evidence).
//
// It is told apart from the transient cannot ask (NCannotAsk, NFewReaders): a
// reader that serves the tier but is down, away or at its room is re-asked when
// one frees, while no reader of the roster serving the tier is structural and
// never re-asked.
//
// tla/WaitFor.tla ServerDefault/OwnerDeliver (class E cannotaskloop);
// docs/SPEC-SPRINT.md section 6, The readers.
const NNoReaderForTier = "no reader for the read tier"

// NoReaderForTierDecisions are the decisions open to NNoReaderForTier: add a
// reader of the tier, raise the stream's read tier, or accept the card on the
// owner's heavy read with the evidence path and reason it names.
//
// tla/WaitFor.tla OwnerDeliver; docs/SPEC-SPRINT.md section 6.
var NoReaderForTierDecisions = []string{
	"reader add",
	"raise the read tier",
	"accept --heavy --evidence <path> --reason <text>",
}

// noReaderTierExit is the kind's exit at its deadline: the owner delivery, never
// a safe server default (judgment_exits.go, v17-every-judgment-kind-has-a-deadline-exit;
// tla/WaitFor.tla OwnerDeliver).
const noReaderTierExit = "owner"

// noReaderForTier says the primary can never be asked its read: its read tier
// has no reader row in the roster besides its own worker's, whatever the rows'
// states. A roster with no row at all is NReviewNoReader's, not this kind's.
//
// tla/WaitFor.tla Read/ServerDefault; docs/SPEC-SPRINT.md section 6, The readers.
func (s *Snapshot) noReaderForTier(pr *Card) (string, bool) {
	// the reversed witness: a test removes the split for its own snapshot and the
	// structural wait is then the transient one (cannot_ask_tier_test.go)
	if f, ok := noReaderForTierSeam.Load(s); ok {
		return f.(func(*Card) (string, bool))(pr)
	}
	if s == nil || s.Readers == nil || len(s.Readers.Rows()) == 0 {
		return "", false
	}
	tier := s.readTierOf(pr)
	worker := attemptWorkerRow(s, pr)
	for _, rd := range s.Readers.Rows() {
		if sameReaderAsWorker(rd, worker) {
			continue
		}
		if s.readerServesTier(rd, tier) {
			return tier, false
		}
	}
	return tier, true
}

// noReaderForTierSeam is the test-only seam: a predicate per snapshot replaces
// the structural test, so a test can show the rule by its absence.
var noReaderForTierSeam sync.Map // *Snapshot -> func(*Card) (string, bool)

// attemptWorkerRow is the fleet row of the member that worked the primary's
// attempt: its work card's row, the member field, or the primary's own.
func attemptWorkerRow(s *Snapshot, pr *Card) string {
	if s != nil && s.Fleet != nil {
		if wc := s.Fleet.Card(WorkCardID(pr.ID, pr.Int("attempt"))); wc != nil {
			if wc.Row != "" {
				return wc.Row
			}
			if m := wc.F("member"); m != "" {
				return m
			}
		}
	}
	if pr != nil {
		return pr.F("member")
	}
	return ""
}

// sameReaderAsWorker says reader names the worker's own reader: reader-<member>,
// the member row itself, or reader-<friend> for a friend's row.
func sameReaderAsWorker(reader, worker string) bool {
	if reader == "" || worker == "" {
		return false
	}
	if reader == worker || reader == ReaderPrefix+worker {
		return true
	}
	if name, ok := FriendOfRow(worker); ok && reader == ReaderPrefix+name {
		return true
	}
	return false
}

// noReaderForTierConds is the tick's condition for every read tier no reader of
// the roster serves: one judgment per tier naming every primary it holds.
//
// tla/WaitFor.tla Read/ServerDefault; docs/SPEC-SPRINT.md section 6, The readers.
func noReaderForTierConds(structural map[string][]string) []cond {
	tiers := make([]string, 0, len(structural))
	for tier := range structural {
		tiers = append(tiers, tier)
	}
	sort.Strings(tiers)
	var out []cond
	for _, tier := range tiers {
		cards := structural[tier]
		sort.Strings(cards)
		out = append(out, cond{
			typ:         NNoReaderForTier,
			stream:      TierSubject(tier),
			streamLevel: true,
			primaries:   cards,
			what:        noReaderForTierWhat(tier, cards),
			decisions:   NoReaderForTierDecisions,
			tier:        tier,
		})
	}
	return out
}

// noReaderForTierWhat is the judgment's text: the tier, every primary it holds,
// and the three remedies.
func noReaderForTierWhat(tier string, cards []string) string {
	return fmt.Sprintf("%s for tier %s: %s; no reader row serves %s besides the card's own worker, so freeing one cannot help: nova-sprint reader add <name> --tiers %s, or raise the read tier, or accept --heavy --evidence <path> --reason <text>",
		NNoReaderForTier, tier, strings.Join(cards, ", "), tier, tier)
}
