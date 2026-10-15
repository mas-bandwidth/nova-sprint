package store

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// The load cache's mark, read by a load that spans more than one read set
// (load.go readInto, loadcache.go markOf): a table of more than
// ntable.LimitReadSetMembers records is read in several read sets, and a store
// that lost writes and wrote others up to the same revision between two of them
// gives one revision and two histories. A load must refuse that as a table that
// moved (tla/LoadCache.tla, LoadIsSnapshot, with the store's rollback), never
// return a snapshot mixing the two.

// flipStore is the in-memory store read through a wrapper whose ReadSet, after
// the first read set of the named table, replaces the table's records with
// others at the same revision and gives the revision a new mark: a store that
// rolled back and rewrote up to the same revision between two read sets of one
// load. It counts the marks asked of it (TableChangesMarked).
type flipStore struct {
	*Mem
	table string
	mu    sync.Mutex
	done  bool
	marks int
}

func (f *flipStore) ReadSet(ctx context.Context, table string, ids []string) (ntable.ReadSetResult, error) {
	res, err := f.Mem.ReadSet(ctx, table, ids)
	if err != nil {
		return res, err
	}
	if table == f.table {
		f.mu.Lock()
		first := !f.done
		if first {
			f.done = true
		}
		f.mu.Unlock()
		if first {
			f.rewrite()
		}
	}
	return res, nil
}

func (f *flipStore) TableChangesMarked(ctx context.Context, table string, from, to uint64) ([]string, string, string, bool, error) {
	f.mu.Lock()
	f.marks++
	f.mu.Unlock()
	return f.Mem.TableChangesMarked(ctx, table, from, to)
}

func (f *flipStore) marked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.marks
}

// rewrite replaces every record's history field, at the same revision, with a
// new change event: a store that lost its writes and wrote another history up
// to the same revision.
func (f *flipStore) rewrite() {
	f.Mem.mu.Lock()
	defer f.Mem.mu.Unlock()
	t := f.Mem.tables[f.table]
	for _, mm := range t.members {
		mm.fields["hist"] = "B"
	}
	t.changes = append(t.changes, memChange{mark: memMarks.Add(1), epoch: f.Mem.active(t), before: t.rev - 1, after: t.rev, verb: "rewrite"})
}

// noMarkStore is the in-memory store wrapped in a backend that keeps no marked
// change stream: the reads must be what they were before the mark.
type noMarkStore struct{ Backend }

// seedHist places ids in the named table under one history, each a placed record
// with the field hist, applied until the table holds them all: a table of the
// size the load reads in several read sets.
func seedHist(t *testing.T, m *Mem, table string, ids []string, hist string) {
	t.Helper()
	for start := 0; start < len(ids); start += ntable.LimitChangedEntries {
		end := min(start+ntable.LimitChangedEntries, len(ids))
		es := make([]ntable.BatchMemberEntry, 0, end-start)
		for _, id := range ids[start:end] {
			es = append(es, ntable.BatchMemberEntry{ID: id, Expect: &ntable.MemberExpect{Absent: true},
				Create: &ntable.MemberCreateOp{Row: "s1", Col: sprint.Ready, Score: 0},
				Set:    map[string]string{"hist": hist}})
		}
		_, err := m.Apply(context.Background(), ntable.BatchManifest{Schema: 1, Table: table, Epoch: "0",
			ExpectedTableRevision: fmt.Sprint(m.Revision(table)), OperationID: fmt.Sprintf("seed-%s-%d", hist, start), Members: es})
		require.NoError(t, err)
	}
}

// chunkIDs is n ids not already on the table.
func chunkIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("x%d", i)
	}
	return ids
}

// histories counts the cards a table's snapshot carries of each history.
func histories(t *sprint.Table) (ofA, ofB int) {
	for _, c := range t.Cards() {
		switch c.F("hist") {
		case "A":
			ofA++
		case "B":
			ofB++
		}
	}
	return ofA, ofB
}

// A table read in more than one read set whose records were replaced at the same
// revision between the read sets is refused as moved: readInto reads the mark of
// the event that left the revision (markOf) before the first read set and after
// the last, and a mark that changed is a movedError. On BASE the read returns a
// snapshot of both histories.
func TestALoadOverChunksRefusesAReplacedHistory(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	table := h.st.Names.Table(sprint.Work)
	ids := chunkIDs(1240)
	require.Greater(t, len(ids), ntable.LimitReadSetMembers, "the table must be read in more than one read set")
	seedHist(t, h.m, table, ids, "A")

	st := h.st.clone()
	f := &flipStore{Mem: h.m, table: table}
	st.B = f
	got, err := st.loadOnce(h.ctx, []string{sprint.Work}, nil)
	var moved *movedError
	if err == nil {
		ofA, ofB := histories(got.Work)
		require.FailNow(t, fmt.Sprintf("the load returned a snapshot mixing two histories at one revision: %d cards of one history, %d of the other", ofA, ofB))
	}
	require.ErrorAs(t, err, &moved, "the read must be refused as moved: %v", err)
}

// A retry after the store settles reads one history whole: the first read set of
// the load sees the store rewrite, readInto refuses it as moved, and the retry,
// the store now quiet, reads the new history and no card of the one it rolled
// back from.
func TestALoadOverChunksReadsOneHistoryWholeAfterTheStoreSettles(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	table := h.st.Names.Table(sprint.Work)
	ids := chunkIDs(1240)
	seedHist(t, h.m, table, ids, "A")

	st := h.st.clone()
	st.B = &flipStore{Mem: h.m, table: table}
	got, err := st.Load(h.ctx, []string{sprint.Work}, nil)
	require.NoError(t, err, "the retry after the store settled")
	ofA, ofB := histories(got.Work)
	require.Zero(t, ofA, "the load mixed the history it rolled back from: %d cards of A, %d of B", ofA, ofB)
	require.GreaterOrEqual(t, ofB, len(ids), "the whole new history must be read: %d cards of B, want at least %d", ofB, len(ids))
}

// A table of at most ntable.LimitReadSetMembers records is read in one read set
// and reads no mark.
func TestALoadOverOneReadSetReadsNoMark(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	table := h.st.Names.Table(sprint.Work)
	seedHist(t, h.m, table, chunkIDs(1000), "A")

	st := h.st.clone()
	f := &flipStore{Mem: h.m, table: table}
	st.B = f
	_, err := st.Load(h.ctx, []string{sprint.Work}, nil)
	require.NoError(t, err)
	require.Zero(t, f.marked(), "a table of one read set read %d marks, want none", f.marked())
}

// A backend that keeps no marks reads as today: a table read in several read
// sets, with no TableMarker, is read whole.
func TestALoadOverChunksWithoutMarksReadsAsToday(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	table := h.st.Names.Table(sprint.Work)
	ids := chunkIDs(1240)
	seedHist(t, h.m, table, ids, "A")

	st := h.st.clone()
	st.B = noMarkStore{h.m}
	got, err := st.Load(h.ctx, []string{sprint.Work}, nil)
	require.NoError(t, err, "a backend with no marks")
	ofA, _ := histories(got.Work)
	require.Equal(t, len(ids), ofA, "the whole history must be read: %d cards, want %d", ofA, len(ids))
}
