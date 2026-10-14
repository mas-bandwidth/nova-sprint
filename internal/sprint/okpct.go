package sprint

import (
	"slices"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// OkCounts counts the row's work cards (not its read cards) into ok, failed and
// pending by the owner's rule of 2026-10-14 (docs/SPEC-SPRINT.md section 1, done
// and ok%): ok% = finished-ok / (finished-ok + finished-failed), counted at the
// finish. A card waiting for its read is counted at its finish, as ok from the
// worker's word, and shown as pending; a bounce is not an attempt and not a
// failure; a later HOLD read turns that finish into a failure. The cells count
// so: ok and failed as they are; a finished card with ok=yes and a head is ok
// and pending; a finished card with ok!=yes (the worker's own failed finish) is
// failed; defect, withdrawn, ready, working and redealt count nothing. A card
// counts only when its finish stamp is after since (zero: every card), the same
// stamp a stats reset's mark reads. Pure: no card's state machine changes; ok%
// is a view over the cells.
func OkCounts(fleet *Table, row string, since time.Time) (ok, failed, pending int) {
	if fleet == nil {
		return 0, 0, 0
	}
	counts := func(c *Card) bool {
		if _, _, work := ParseWorkCard(c.ID); !work {
			return false // a read card is no work
		}
		if !since.IsZero() {
			t := stampAt(c, "finished")
			if t.IsZero() || !t.After(since) {
				return false
			}
		}
		return true
	}
	for _, c := range fleet.Cell(row, DoneOK) {
		if counts(c) {
			ok++
		}
	}
	for _, c := range fleet.Cell(row, DoneFailed) {
		if counts(c) {
			failed++
		}
	}
	for _, c := range fleet.Cell(row, Finished) {
		if !counts(c) {
			continue
		}
		switch {
		case c.F("ok") == "yes" && c.F("head") != "":
			ok++
			pending++
		case c.F("ok") != "yes":
			failed++
		}
	}
	return ok, failed, pending
}

// OkPctCells is a fleet or friends table with each row's ok and failed cells
// the counts OkCounts reports for its fleet row (rowOf maps a row's key to its
// fleet row), and a display-only pending count column beside ok%. The ok% and
// done formulas read the ok and failed cells, so ok% = ok/(ok+failed) over
// those counts, a finish counted at its finish, ok while its read is pending,
// and the footer's pooled ok% over the same. Display only: the stored cells
// stay as they are.
func OkPctCells(t ntable.Table, fleet *Table, since time.Time, rowOf func(key string) string) ntable.Table {
	cols := slices.Clone(t.Columns)
	at := slices.IndexFunc(cols, func(c ntable.Column) bool { return c.Name == OkPct })
	pendingCol := ntable.Column{Name: "pending", Projection: ntable.Count, Fold: ntable.Sum}
	insertAt := -1
	if i := slices.IndexFunc(cols, func(c ntable.Column) bool { return c.Name == "pending" }); i >= 0 {
		cols[i] = pendingCol
	} else if at >= 0 {
		insertAt = at + 1
		cols = slices.Insert(cols, insertAt, pendingCol)
	} else {
		insertAt = len(cols)
		cols = append(cols, pendingCol)
	}
	t.Columns = cols
	ok, failed, pending := t.Column(DoneOK), t.Column(DoneFailed), t.Column("pending")
	rows := make([]ntable.Row, len(t.Rows))
	for i, r := range t.Rows {
		cells := slices.Clone(r.Cells)
		if insertAt >= 0 {
			cells = slices.Insert(cells, min(insertAt, len(cells)), ntable.Cell{})
		}
		for len(cells) < len(t.Columns) {
			cells = append(cells, ntable.Cell{})
		}
		o, f, p := OkCounts(fleet, rowOf(r.Key), since)
		if fleet != nil {
			if ok >= 0 {
				cells[ok] = ntable.Cell{Count: int64(o)}
			}
			if failed >= 0 {
				cells[failed] = ntable.Cell{Count: int64(f)}
			}
		}
		cells[pending] = ntable.Cell{Count: int64(p)}
		r.Cells = cells
		rows[i] = r
	}
	t.Rows = rows
	return t
}
