package verbs

import (
	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// CardReportPlanner wires the canonical atomic layer-2 report plan.
type CardReportPlanner struct{}

func (CardReportPlanner) PlanReport(s store.Snapshot, c store.Card, r card.Report) (card.Plan, *card.Refusal) {
	p, ok := s.Cards[c.Of]
	if !ok {
		return card.Plan{}, &card.Refusal{Reason: "consumer " + c.ID + " has no producer"}
	}
	stream, ok := s.Streams[p.Stream]
	if !ok {
		return card.Plan{}, &card.Refusal{Reason: "producer " + p.ID + " has no stream"}
	}
	rows := make(map[string]card.Row, len(s.Rows))
	for id, row := range s.Rows {
		rows[id] = card.RowFromStore(row, 0, nil)
	}
	all := make(map[string]card.Card, len(s.Cards))
	for id, item := range s.Cards {
		all[id] = card.FromStore(item)
	}
	return card.ReportPlan(card.FromStore(c), card.FromStore(p), consumers(s, p.ID), rows, card.StreamFromStore(stream), all, r)
}
