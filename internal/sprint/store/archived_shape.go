package store

import (
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// ArchivedOfShape is the archived streams (stream archive) of the work table's shape, nil
// with none: the rows hidden (the table layer's row hide) that count no card but landed, as
// where counts them (cmd/nova-sprint archivedRow). A hidden row a card was added to again is
// back on the table. The tick reads the archived streams here because its twin carries no
// row hide.
func ArchivedOfShape(t ntable.Table) map[string]bool {
	landed := t.Column(sprint.Landed)
	var out map[string]bool
rows:
	for _, r := range t.Rows {
		if !r.Hidden {
			continue
		}
		for k, c := range t.Columns {
			if c.Projection == ntable.Count && k != landed && k < len(r.Cells) && r.Cells[k].Count > 0 {
				continue rows
			}
		}
		if out == nil {
			out = map[string]bool{}
		}
		out[r.Key] = true
	}
	return out
}
