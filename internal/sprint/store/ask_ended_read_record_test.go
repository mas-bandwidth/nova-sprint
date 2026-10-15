package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/require"
)

// seedEndedAsk puts primary p in review with `ended` ended reads of one reader (a member
// m2 with a reader row, or a friend amy whose fleet row is gone) and the coordinator's
// cannot-ask judgment open; it returns the reader, her row and the judgment's id.
func seedEndedAsk(h *harness, friend bool, ended int) (reader, row string, answers []string) {
	h.t.Helper()
	t := h.t
	reader, row = "m2", "m2"
	if friend {
		reader, row = "amy", "friend.amy"
		_, _, _, err := h.st.SyncFriends(h.ctx, []FriendSpec{{Name: reader, Width: 2, Class: "flash", Roles: "reader"}})
		require.NoError(t, err)
		h.up(reader)
		require.NoError(t, h.m.RowsAdd(h.ctx, h.st.Names.Table(sprint.Fleet), []string{row}))
	} else {
		h.must(FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: reader, Width: 2}))
		require.NoError(t, h.m.RowsAdd(h.ctx, h.st.Names.Table(sprint.Readers), []string{"reader-m2"}))
		require.NoError(t, h.st.BeatReaders(h.ctx))
	}
	h.must(AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"p"}, Brief: "p: read a pushed head\n"}))
	for _, move := range []struct {
		col string
		set map[string]string
	}{{sprint.Working, nil}, {sprint.Review, map[string]string{"attempt": "1", "head": "abc123", "result": "ok"}}} {
		h.must(Step{Verb: "seed " + move.col, Load: All, Plan: func(s *sprint.Snapshot) sprint.Plan {
			c := s.Work.Card("p")
			return sprint.Plan{Units: []sprint.Unit{{Key: "p", Changes: []sprint.Change{{Table: sprint.Work, Entry: ntable.BatchMemberEntry{ID: "p", Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev), Place: &ntable.PlaceExpect{Row: c.Row, Col: c.Col}}, Move: &ntable.MemberMoveOp{Row: "s1", Col: move.col}, Set: move.set}}}}}}
		}})
	}
	for i := 0; i < ended; i++ {
		id := sprint.ReadCardID("p", 1, reader)
		if i > 0 {
			id += fmt.Sprintf(".g%d", i)
		}
		h.must(Step{Verb: "seed read", Load: All, Plan: func(s *sprint.Snapshot) sprint.Plan {
			return sprint.Plan{Units: []sprint.Unit{{Key: id, Changes: []sprint.Change{{Table: sprint.Fleet, Entry: ntable.BatchMemberEntry{ID: id, Expect: &ntable.MemberExpect{Absent: true}, Create: &ntable.MemberCreateOp{Row: row, Col: sprint.Working, Score: 1}, Set: map[string]string{"kind": "read", sprint.FieldReadCard: "1", "primary": "p", "stream": "s1", "reader": reader, "attempt": "1", "head": "abc123", "gen": "1"}}}}}}}
		}})
		h.must(ReadStep(sprint.ReadReq{As: row, Sel: sprint.Sel{IDs: []string{id}}, Verdict: "broken", Finding: "STEP 2: origin has no branch; push the head before asking", Missing: map[string]sprint.MissingBranch{id: {Named: "missing", Holds: "pushed"}}}))
	}
	if friend {
		require.NoError(t, h.m.RowsDel(h.ctx, h.st.Names.Table(sprint.Fleet), []string{row}))
	}
	h.must(Step{Verb: "seed cannot ask", Load: All, Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Judgment, Type: sprint.NCannotAsk, Stream: "s1", Primaries: []string{"p"}, Attempt: 1, At: s.Now, What: "read must be asked again"}}}
	}})

	for _, o := range h.snap().Open {
		if o.Note.Type == sprint.NCannotAsk {
			answers = append(answers, o.Note.ID)
		}
	}
	require.Len(t, answers, 1)
	return reader, row, answers
}

func TestAskEndedReadRecordOnTheTwin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		friend bool
		ended  int
	}{
		{"member", false, 1}, {"friend-without-row", true, 1}, {"five-ended", false, 5}, {"long-chain", false, 130},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			reader, row, answers := seedEndedAsk(h, tc.friend, tc.ended)
			before := h.m.Calls["readset"]
			h.must(AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{"p"}}, Answers: answers, Who: "tester"}))
			_ = row
			t.Logf("ended=%d readset exchanges=%d", tc.ended, h.m.Calls["readset"]-before)
			s := h.snap()
			next := fmt.Sprintf("%s.g%d", sprint.ReadCardID("p", 1, reader), tc.ended)
			require.NotNil(t, s.Fleet.Placed(next))
			ids := sprint.ReadCardGenIDs("p", 1, reader, tc.ended-1)
			records, err := h.st.Records(h.ctx, sprint.Fleet, ids)
			require.NoError(t, err)
			require.Len(t, records, tc.ended)
			for _, c := range records {
				require.False(t, c.Placed())
				require.Equal(t, sprint.RetiredByMissingBranch, c.F("retired_by"))
			}
		})
	}
}

// hidingBackend is a store whose read of one record misses it until a write is refused:
// the allocator then plans on a snapshot that does not hold the ended record, as the
// older binary did. tla/AskReadIDs.tla Retry.
type hidingBackend struct {
	Backend
	hide    string
	forever bool
	refused bool
}

func (b *hidingBackend) ReadSet(ctx context.Context, table string, ids []string) (ntable.ReadSetResult, error) {
	res, err := b.Backend.ReadSet(ctx, table, ids)
	if err != nil || (b.refused && !b.forever) {
		return res, err
	}
	kept := res.Members[:0:0]
	for _, m := range res.Members {
		if m.ID == b.hide {
			res.Missing = append(res.Missing, m.ID)
			continue
		}
		kept = append(kept, m)
	}
	res.Members = kept
	return res, nil
}

func (b *hidingBackend) Apply(ctx context.Context, man ntable.BatchManifest) (ntable.Receipt, error) {
	r, err := b.Backend.Apply(ctx, man)
	if err != nil {
		b.refused = true
	}
	return r, err
}

// A create refused MEMBEREXISTS on an ended read record the load missed is re-planned once
// with that record loaded, and mints the next id; a record that stays unseen is refused
// the second time, naming the id.
func TestAskReplansOnceAfterAnEndedReadRecordRefusesTheCreate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		forever bool
	}{{"once", false}, {"still-unseen", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			reader, _, answers := seedEndedAsk(h, false, 1)
			base := sprint.ReadCardID("p", 1, reader)
			hb := &hidingBackend{Backend: h.st.B, hide: h.st.sid(base), forever: tc.forever}
			h.st.B = hb
			res := h.run(AskStep(sprint.AskReq{Sel: sprint.Sel{IDs: []string{"p"}}, Answers: answers, Who: "tester"}))
			if tc.forever {
				require.NotEmpty(t, res.Refused)
				require.Contains(t, res.Refused[0].Why, "MEMBEREXISTS")
				require.Contains(t, res.Refused[0].Why, base)
				return
			}
			require.Empty(t, res.Refused)
			require.True(t, hb.refused, "the first create was refused")
			require.NotNil(t, h.snap().Fleet.Placed(base+".g1"))
			records, err := h.st.Records(h.ctx, sprint.Fleet, []string{base})
			require.NoError(t, err)
			require.Len(t, records, 1)
			require.False(t, records[0].Placed())
		})
	}
}
