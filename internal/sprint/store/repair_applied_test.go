package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forgetOps drops the table layer's record of every operation id applied to
// the table: a resend of one of them is judged again, not replayed.
func (m *Mem) forgetOps(table string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tables[table].ops = map[string]memOp{}
}

// cutAfterApply deals s1-1 and s1-2 in one step whose every manifest applies
// and whose release is lost: the operation stays pending, the fleet and work
// tables already as it leaves them.
func (h *harness) cutAfterApply() {
	h.t.Helper()
	h.m.Fail = func(p string) error {
		if p == "release" {
			return errors.New("lost")
		}
		return nil
	}
	_, err := h.st.Run(h.ctx, DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"s1-1", "s1-2"}}}))
	h.m.Fail = nil
	require.Error(h.t, err, "the release was not lost")
	require.NotNil(h.t, h.m.Pending(), "the step is not pending")
	require.Equal(h.t, sprint.Working, h.table().Work.Card("s1-1").Col, "the work manifest did not apply")
}

// The 2026-10-17 tick drain (v1.5.4 hotfix): a cut operation whose later
// manifest, the work table's, applied whole under its own id, and whose id the
// store no longer answers from, is repaired entry by entry. Each entry the
// store holds as its result (one revision past what it expected, at its
// target, its fields set) counts as applied: no skip, no judgment, its move
// line kept, and the log replays to the table (check rule 13 holds).
// tla/RepairLog.tla AppliedHasLine (the witness "skipapplied" is the bug).
func TestRepairCountsAnEntryTheStoreHoldsAsItsResultApplied(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	h.cutAfterApply()
	h.m.forgetOps("t-work")
	h.tick(time.Hour)
	rr, err := h.st.Repair(h.ctx)
	require.NoError(t, err, "repair: %+v", rr)
	require.Len(t, rr, 1, "repair: %+v", rr)
	assert.Equal(t, RepairFinished, rr[0].Done, "repair: %+v", rr)
	assert.Empty(t, rr[0].Skipped, "an applied entry was skipped")
	assert.Empty(t, h.skipNotes(), "a skip judgment was written for applied entries")
	assert.Nil(t, h.m.Pending(), "the fence is still held")
	lines, err := h.st.Log(h.ctx)
	require.NoError(t, err)
	moved := map[string]string{}
	for _, l := range lines {
		if l.Kind == sprint.LineMove && l.Table == sprint.Work {
			moved[l.Card] = l.To
		}
	}
	assert.Equal(t, "s1:working", moved["s1-1"], "s1-1's move line is missing")
	assert.Equal(t, "s1:working", moved["s1-2"], "s1-2's move line is missing")
	h.clean("after the repair")
}

// An entry refused on its expectation whose member the store does not hold as
// its result is still skipped: a member a writer outside the fence moved on
// after the entry applied is past the entry's result, and repair never
// overwrites it.
func TestRepairStillSkipsAnEntryTheStoreDoesNotHoldAsItsResult(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	h.cutAfterApply()
	h.m.forgetOps("t-work")
	s := h.table()
	p := s.Work.Card("s1-1")
	_, err := h.m.Apply(h.ctx, ntable.BatchManifest{Schema: 1, Table: "t-work", Epoch: "0", ExpectedTableRevision: fmt.Sprint(s.Work.Revision), OperationID: "outside",
		Members: []ntable.BatchMemberEntry{{ID: p.ID, Expect: &ntable.MemberExpect{Place: &ntable.PlaceExpect{Row: p.Row, Col: p.Col}}, Move: &ntable.MemberMoveOp{Row: "s1", Col: sprint.Ready}}}})
	require.NoError(t, err)
	h.tick(time.Hour)
	rr, err := h.st.Repair(h.ctx)
	require.NoError(t, err, "repair: %+v", rr)
	require.Len(t, rr, 1, "repair: %+v", rr)
	require.Equal(t, RepairSkipped, rr[0].Done, "repair: %+v", rr)
	require.Len(t, rr[0].Skipped, 1, "repair: %+v", rr)
	assert.Contains(t, rr[0].Skipped[0], "card s1-1", "repair: %+v", rr)
	assert.Equal(t, sprint.Ready, h.table().Work.Card("s1-1").Col, "the outside writer's move was overwritten")
}

// refusedResend refuses the first send of a manifest under its own operation
// id at the revision it records, as a bound the table layer checks ahead of
// its record would (table.lua T.apply: manifest_bytes before the lookup); the
// probe at a revision no table reaches, and every other send, pass through.
type refusedResend struct {
	Backend
	id    string
	sends []string
}

func (r *refusedResend) Apply(ctx context.Context, m ntable.BatchManifest) (ntable.Receipt, error) {
	r.sends = append(r.sends, m.OperationID+"@"+m.ExpectedTableRevision)
	if m.OperationID == r.id && m.ExpectedTableRevision != "18446744073709551615" {
		return ntable.Receipt{}, refusal("LIMIT", "manifest bytes: bound exceeded")
	}
	return r.Backend.Apply(ctx, m)
}

// A later manifest refused when it is sent again gets the first's probe: the
// table layer's record says its writer applied it whole, so repair finishes
// with it applied and sends none of its entries again
// (tla/RepairLog.tla RepairProbe).
func TestRepairProbesALaterManifestItsWriterApplied(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.setup(2)
	h.cutAfterApply()
	op := h.m.Pending()
	var work string
	for _, man := range op.Manifests {
		if man.Table == "t-work" {
			work = man.OperationID
		}
	}
	require.NotEmpty(t, work, "the operation has no work manifest: %+v", op.Manifests)
	st := *h.st
	rb := &refusedResend{Backend: h.m, id: work}
	st.B = rb
	h.tick(time.Hour)
	rr, err := st.Repair(h.ctx)
	require.NoError(t, err, "repair: %+v", rr)
	require.Len(t, rr, 1, "repair: %+v", rr)
	assert.Equal(t, RepairFinished, rr[0].Done, "repair: %+v", rr)
	for _, s := range rb.sends {
		assert.False(t, strings.HasPrefix(s, work+".e"), "an entry of the applied manifest was sent again: %v", rb.sends)
	}
	h.clean("after the repair")
}

// heldAsResult: the member one revision past the entry's expectation, in the
// place the entry leaves it, with its fields, is the entry applied; anything
// else is not.
func TestHeldAsResult(t *testing.T) {
	t.Parallel()
	move := ntable.BatchMemberEntry{ID: "c", Expect: &ntable.MemberExpect{Revision: "4", Place: &ntable.PlaceExpect{Row: "s", Col: "working"}},
		Move: &ntable.MemberMoveOp{Row: "s", Col: "ready"}, Set: map[string]string{"gen": "2"}, Unset: []string{"lane"}}
	set := ntable.BatchMemberEntry{ID: "c", Expect: &ntable.MemberExpect{Revision: "4", Place: &ntable.PlaceExpect{Row: "s", Col: "working"}},
		Set: map[string]string{"behind": "1"}}
	remove := ntable.BatchMemberEntry{ID: "c", Expect: &ntable.MemberExpect{Revision: "4"}, Remove: true}
	create := ntable.BatchMemberEntry{ID: "c", Expect: &ntable.MemberExpect{Absent: true}, Create: &ntable.MemberCreateOp{Row: "s", Col: "ready"}}
	guard := ntable.BatchMemberEntry{ID: "c", Expect: &ntable.MemberExpect{Revision: "4"}}
	at := func(rev uint64, placed bool, col string, fields map[string]string) ntable.ReadSetResult {
		return ntable.ReadSetResult{Members: []ntable.ReadSetMember{{ID: "c", Revision: rev, Placed: placed, Row: "s", Col: col, Fields: fields}}}
	}
	for _, tc := range []struct {
		name string
		e    ntable.BatchMemberEntry
		rs   ntable.ReadSetResult
		want bool
	}{
		{"a move applied", move, at(5, true, "ready", map[string]string{"gen": "2"}), true},
		{"a move, the revision two past", move, at(6, true, "ready", map[string]string{"gen": "2"}), false},
		{"a move, the revision as expected", move, at(4, true, "ready", map[string]string{"gen": "2"}), false},
		{"a move, another place", move, at(5, true, "review", map[string]string{"gen": "2"}), false},
		{"a move, a field set to another value", move, at(5, true, "ready", map[string]string{"gen": "3"}), false},
		{"a move, a field it unsets still there", move, at(5, true, "ready", map[string]string{"gen": "2", "lane": "1"}), false},
		{"a move, the member gone", move, ntable.ReadSetResult{}, false},
		{"fields set in place, applied", set, at(5, true, "working", map[string]string{"behind": "1"}), true},
		{"fields set in place, moved since", set, at(5, true, "ready", map[string]string{"behind": "1"}), false},
		{"a removal applied", remove, at(5, false, "", nil), true},
		{"a removal, still placed", remove, at(5, true, "ready", nil), false},
		{"a creation is never told applied this way", create, at(1, true, "ready", nil), false},
		{"a guard alone changes nothing", guard, at(5, true, "ready", nil), false},
	} {
		assert.Equal(t, tc.want, heldAsResult(tc.e, tc.rs), tc.name)
	}
}

// propsHeld: the table holds every property the manifest sets at its value.
func TestPropsHeld(t *testing.T) {
	t.Parallel()
	set := map[string]string{"stream_index": "3", "last_landing": "x"}
	assert.True(t, propsHeld(set, map[string]string{"stream_index": "3", "last_landing": "x", "other": "y"}), "every property at its value")
	assert.False(t, propsHeld(set, map[string]string{"stream_index": "2", "last_landing": "x"}), "one property at its value before")
	assert.False(t, propsHeld(set, map[string]string{"stream_index": "3"}), "one property absent")
	assert.False(t, propsHeld(nil, map[string]string{"stream_index": "3"}), "a manifest that sets none")
}
