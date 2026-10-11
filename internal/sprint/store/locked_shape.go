package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// LockedShape checks the columns and hidden columns the store's tables hold
// against the ones this build stores (sprint.Names.Definitions), holding to
// internal/sprint/TABLES.lock (the Change of 2026-10-06): the live store's
// fleet table takes finished and redealt, and hides finished, before the build
// that writes them is installed. run and the shadow tick call it once before
// they touch the store, so a build never starts on a table it would write a
// missing column to: the v1.2.10 outage refused the first finish to done
// ("column \"finished\" ...: no such column") because the adopt skipped the
// migration (internal/sprint/store/migrations/0001_fleet_finished_redealt.sh).
//
// A table the store does not have yet is left to Init (it creates that table
// whole); a column the store has that the lock does not name is left alone,
// because an older build must still run on a newer store (the rollback case);
// and readers.tiers absent is left alone, because the build adds that column
// itself (EnsureReaderTiers).
//
// It returns nil when every locked column and every locked hidden column is
// present. Otherwise one error names, per table, each missing column, then the
// nova-table commands that take the store to the lock, in the lock's order, and
// says they run on a STOPPED machine before this build is installed.
func (st *Store) LockedShape(ctx context.Context) error {
	var clauses, commands []string
	for _, def := range st.Names.Definitions() {
		shapes, err := st.B.Shapes(ctx, []string{def.Name})
		if err != nil {
			if tableMissing(err) {
				continue // Init creates a table the store does not have yet
			}
			return err
		}
		if len(shapes) == 0 {
			continue
		}
		shape := shapes[0]
		var missing, unhidden []string
		for i, col := range def.Columns {
			if col.Name == sprint.ReaderTiers && def.Name == st.Names.Table(sprint.Readers) {
				continue // the build adds tiers itself (EnsureReaderTiers)
			}
			if shape.Column(col.Name) >= 0 {
				continue
			}
			missing = append(missing, col.Name)
			at := "--first"
			if i > 0 {
				at = "--after " + def.Columns[i-1].Name
			}
			commands = append(commands, "nova-table col add "+def.Name+" "+lockedColumnSpec(col)+" "+at)
		}
		for _, col := range def.Hidden {
			if !shape.IsHidden(col) {
				unhidden = append(unhidden, col)
				commands = append(commands, "nova-table set "+def.Name+" --hide "+col)
			}
		}
		if len(missing) > 0 {
			clauses = append(clauses, def.Name+" is missing "+strings.Join(missing, ", "))
		}
		if len(unhidden) > 0 {
			clauses = append(clauses, def.Name+" does not hide "+strings.Join(unhidden, ", "))
		}
	}
	if len(commands) == 0 {
		return nil
	}
	return fmt.Errorf("%s; run on a STOPPED machine, before this build is installed (internal/sprint/TABLES.lock): %s",
		strings.Join(clauses, "; "), strings.Join(commands, "; "))
}

// lockedColumnSpec writes a column the way nova-table col add's spec reads it
// (ntable.ParseColumn): name[:projection[:fold[:label]]], with the defaults
// ParseColumn applies left out. A plain count column's spec is its bare name,
// as in the migration's lines.
func lockedColumnSpec(c ntable.Column) string {
	def := ntable.None
	switch {
	case c.Projection == ntable.Count, ntable.IsSum(c.Projection):
		def = ntable.Sum
	case ntable.IsPct(c.Projection):
		def = ntable.Pooled
	}
	if c.Projection == ntable.Count && c.Fold == ntable.Sum && c.Label == "" {
		return c.Name
	}
	spec := c.Name + ":" + c.Projection
	if c.Fold != def || c.Label != "" {
		spec += ":" + c.Fold
	}
	if c.Label != "" {
		spec += ":" + c.Label
	}
	return spec
}
