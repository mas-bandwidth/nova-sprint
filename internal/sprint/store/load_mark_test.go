package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mixedBackend is a test store that answers one table's shapes, cell ids and read
// sets from records of its own. It holds one history's records at one revision and,
// after the first read set of that table, the other history's records at the same
// revision: a store that rolled back and rewrote up to the same revision between two
// read sets (tla/LoadCache.tla, LoadIsSnapshot). The two histories are told apart by
// a field's value.
type mixedBackend struct {
	Backend
	table string       // the stored table this backend answers for
	shape ntable.Table // the table's one revision
	ids   []string     // the member ids, in read order (more than one read set)
	row   string
	col   string
	field string // the field whose value names the history
	histA string
	histB string
	// swap replaces the records with the other history after the first read set:
	// set, it is the loss and rewrite; clear, the store is still.
	swap    bool
	swapped bool
	readSets int
}

func (b *mixedBackend) Shapes(ctx context.Context, tables []string) ([]ntable.Table, error) {
	out := make([]ntable.Table, 0, len(tables))
	for _, name := range tables {
		if name == b.table {
			out = append(out, b.shape)
			continue
		}
		one, err := b.Backend.Shapes(ctx, []string{name})
		if err != nil {
			return nil, err
		}
		out = append(out, one...)
	}
	return out, nil
}

func (b *mixedBackend) CellIDs(context.Context, []ntable.Table) (map[string][]string, error) {
	return map[string][]string{b.table: b.ids}, nil
}

func (b *mixedBackend) ReadSet(_ context.Context, table string, ids []string) (ntable.ReadSetResult, error) {
	b.readSets++
	hist := b.histA
	if b.swapped {
		hist = b.histB
	}
	res := ntable.ReadSetResult{Table: table, Epoch: b.shape.Epoch, Revision: b.shape.Revision}
	for _, id := range ids {
		res.Members = append(res.Members, ntable.ReadSetMember{
			ID: id, Revision: 1, Placed: true, Row: b.row, Col: b.col, Score: 1,
			Fields: map[string]string{b.field: hist},
		})
	}
	if b.swap && !b.swapped {
		b.swapped = true
	}
	return res, nil
}

// markBackend is mixedBackend with the marks of the event that left the table's
// revision: the first history's before the swap, the second's after, as a store that
// rolled back and rewrote gives the same revision a new event (loadcache.go markOf).
type markBackend struct {
	*mixedBackend
	markA, markB string
	marks        int
}

func (b *markBackend) TableChangesMarked(_ context.Context, _ string, from, to uint64) ([]string, string, string, bool, error) {
	b.marks++
	if from != to {
		return nil, "", "", true, nil
	}
	if b.swapped {
		return nil, "", b.markB, true, nil
	}
	return nil, "", b.markA, true, nil
}

// newMixedBackend is a mixedBackend over the harness's store for the work table,
// holding n records of one history at one revision.
func newMixedBackend(h *harness, n int) *mixedBackend {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("c%04d", i)
	}
	table := h.st.Names.Table(sprint.Work)
	return &mixedBackend{
		Backend: h.m,
		table:   table,
		shape:   ntable.Table{Name: table, Rows: []ntable.Row{{Key: "r"}}},
		ids:     ids,
		row:     "r",
		col:     "backlog",
		field:   "hist",
		histA:   "a",
		histB:   "b",
	}
}

// TestLoadMarkMixedHistoryRefused pins readInto's mark read (load.go; the load
// table's broken "nomark", tla/LoadCache.tla LoadIsSnapshot): a table of more than
// one read set whose store lost writes and rewrote to the same revision between two
// sets gives one revision and two histories, and the read must be refused as moved,
// with no snapshot of both.
func TestLoadMarkMixedHistoryRefused(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	b := &markBackend{mixedBackend: newMixedBackend(h, 1100), markA: "m1", markB: "m2"}
	b.swap = true
	c := h.st.clone()
	c.B = b
	_, err := c.loadOnce(h.ctx, []string{sprint.Work}, nil)
	var moved *movedError
	require.ErrorAs(t, err, &moved, "a load that read two histories at one revision must be refused as moved")
}

// TestLoadMarkRetrySettlesOnOneHistory pins that the moved read is taken again
// (load.go Load, LoadTries): a store that swaps once, then is still, gives the load
// one history whole on its next read.
func TestLoadMarkRetrySettlesOnOneHistory(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	b := &markBackend{mixedBackend: newMixedBackend(h, 1100), markA: "m1", markB: "m2"}
	b.swap = true
	c := h.st.clone()
	c.B = b
	s, err := c.Load(h.ctx, []string{sprint.Work}, nil)
	require.NoError(t, err, "the load is taken again after the store settles")
	require.NotNil(t, s)
	cards := s.Work.Cards()
	require.Len(t, cards, 1100, "one history whole")
	for _, card := range cards {
		assert.Equal(t, "b", card.Fields["hist"], "the settled history is the one whole")
	}
}

// TestLoadMarkShortTableReadsNoMark pins that a table of at most one read set is read
// as before: no mark is read.
func TestLoadMarkShortTableReadsNoMark(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	b := &markBackend{mixedBackend: newMixedBackend(h, ntable.LimitReadSetMembers), markA: "m1", markB: "m2"}
	b.swap = true
	c := h.st.clone()
	c.B = b
	s, err := c.loadOnce(h.ctx, []string{sprint.Work}, nil)
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Zero(t, b.marks, "a table of one read set reads no mark")
}

// TestLoadMarkBackendWithoutMarks pins that a backend that keeps no marks reads as
// before (loadcache.go TableMarker): with no mark to read, the read is not marked and
// nothing changes for it.
func TestLoadMarkBackendWithoutMarks(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	b := newMixedBackend(h, 1100) // no TableChangesMarked
	b.swap = true
	c := h.st.clone()
	c.B = b
	s, err := c.loadOnce(h.ctx, []string{sprint.Work}, nil)
	require.NoError(t, err, "a backend with no marks is unchanged")
	require.NotNil(t, s)
	assert.Len(t, s.Work.Cards(), 1100, "the backend with no marks reads its records")
}
