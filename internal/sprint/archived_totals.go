package sprint

import (
	"cmp"
	"slices"
	"time"
)

// Archived work is in no total (the owner, 2026-10-14: "archived cards and work streams
// should not show up in totals, dashboard, costs, anywhere"). The where record (store
// WhereRecord) is counted from every card, so it carries beside its epoch figures what an
// archived stream holds that a total would count: its primaries (the done record's
// completions name their cards), its work cards in the fleet rows' ok and failed cells (the
// rows' ok%), and the stage times without it. where takes them off (where_live_totals.go).

// The archived set is read off the work table's shape (store.ArchivedOfShape): the hidden
// rows that count no card but landed.

// ArchivedPrimaries is every placed card of the archived streams, by id, sorted: the
// primaries a work, fix or read completion is of (ParseWorkCard, ParseReadCard).
func ArchivedPrimaries(s *Snapshot, archived map[string]bool) []string {
	if s == nil || s.Work == nil || len(archived) == 0 {
		return nil
	}
	var out []string
	for _, c := range s.Work.Cards() {
		if c.Placed() && archived[c.Row] {
			out = append(out, c.ID)
		}
	}
	slices.Sort(out)
	return out
}

// ArchivedFleetCard is a work card of an archived stream in a fleet row's ok or failed cell:
// the row, the cell, and when it finished (Unix seconds, 0 with no stamp: history, before
// any stats reset's mark).
type ArchivedFleetCard struct {
	Row string `json:"row"`
	Col string `json:"col"`
	At  int64  `json:"at,omitempty"`
}

// ArchivedFleetCards is every work card in a fleet row's ok or failed cell whose primary is
// an archived stream's, in row, cell and time order.
func ArchivedFleetCards(s *Snapshot, archived map[string]bool) []ArchivedFleetCard {
	if s == nil || s.Fleet == nil || s.Work == nil || len(archived) == 0 {
		return nil
	}
	var out []ArchivedFleetCard
	for _, c := range s.Fleet.Column(DoneOK, DoneFailed) {
		id, _, ok := ParseWorkCard(c.ID)
		if !ok {
			continue
		}
		pr := s.Work.Card(id)
		if pr == nil || !archived[pr.Row] {
			continue
		}
		f := ArchivedFleetCard{Row: c.Row, Col: c.Col}
		// the finish stamps the work card (steps_work: "finished"); finished_at as a fallback
		if at, err := time.Parse(time.RFC3339, cmp.Or(c.F("finished"), c.F(FieldFinishedAt))); err == nil {
			f.At = at.Unix()
		}
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b ArchivedFleetCard) int {
		return cmp.Or(cmp.Compare(a.Row, b.Row), cmp.Compare(a.Col, b.Col), cmp.Compare(a.At, b.At))
	})
	return out
}

// LiveCompletions is the completions whose card is no archived primary's (a work or fix
// completion's work card, a read completion's read card).
func LiveCompletions(cs []Completion, archivedPrimaries []string) []Completion {
	if len(archivedPrimaries) == 0 {
		return cs
	}
	gone := make(map[string]bool, len(archivedPrimaries))
	for _, id := range archivedPrimaries {
		gone[id] = true
	}
	out := make([]Completion, 0, len(cs))
	for _, c := range cs {
		if pr, _, ok := ParseWorkCard(c.Card); ok && gone[pr] {
			continue
		}
		if pr, _, _, ok := ParseReadCard(c.Card); ok && gone[pr] {
			continue
		}
		out = append(out, c)
	}
	return out
}
