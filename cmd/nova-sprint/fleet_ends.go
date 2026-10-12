package main

import (
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// fleetEndProjection is the one fleet projection used by where's text and
// JSON, including the server's where endpoint. The stored table still keeps
// verdict-placement cells; these transient count cells come from typed end
// events and alone supply the machine's ok%.
func fleetEndProjection(t ntable.Table, ends map[string]sprint.EndCounts) ntable.Table {
	names := []string{"end_ok", "end_failed", "end_no_result", "end_provider", "end_coordination", "end_environment", "end_harness", "end_clear", "end_unknown"}
	t.Columns = append([]ntable.Column(nil), t.Columns...)
	t.Hidden = append([]string(nil), t.Hidden...)
	for i := range t.Columns {
		if t.Columns[i].Name == sprint.OkPct {
			t.Columns[i].Projection = "pct(end_ok/end_ok+end_failed+end_no_result)"
		}
	}
	for _, name := range names {
		t.Columns = append(t.Columns, ntable.Column{Name: name, Projection: ntable.Count, Fold: ntable.Sum})
		t.Hidden = append(t.Hidden, name)
	}
	for i := range t.Rows {
		c := ends[t.Rows[i].Key]
		counts := []int{c.OK, c.Failed, c.NoResult, c.Provider, c.Coordination, c.Environment, c.Harness, c.Clear, c.Unknown}
		t.Rows[i].Cells = append([]ntable.Cell(nil), t.Rows[i].Cells...)
		for _, n := range counts {
			t.Rows[i].Cells = append(t.Rows[i].Cells, ntable.Cell{Count: int64(n)})
		}
	}
	return t
}
