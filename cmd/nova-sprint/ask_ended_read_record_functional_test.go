//go:build functional

package main

import (
	"context"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestAnEndedReadRecordNeverBlocksAskOnAStore(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		friend bool
		ended  int
	}{
		{"member", false, 1}, {"friend-without-row", true, 1}, {"five-ended", false, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			addr := testutil.Start(t)
			client := redis.NewClient(&redis.Options{Addr: addr})
			t.Cleanup(func() { _ = client.Close() })
			require.NoError(t, fn.Load(ctx, client))
			ta := newFunctionalTestApp(t, addr)
			ta.runVerb("init", "--readers", "reader-m2", "--members", "m2:2")
			ta.a.readFriendHooks = func(context.Context, ...string) ([]hook.Status, time.Time, error) {
				return []hook.Status{{Name: "amy", State: hook.StateProven}}, time.Now(), nil
			}
			st, err := ta.a.store(common{redis: addr, actor: "coordinator"})
			require.NoError(t, err)
			run := func(step store.Step) store.Result {
				t.Helper()
				r, e := st.Run(ctx, step)
				require.NoError(t, e)
				require.Empty(t, r.Refused)
				return r
			}
			snap := func() *sprint.Snapshot {
				t.Helper()
				s, e := st.Load(ctx, store.All, nil)
				require.NoError(t, e)
				return s
			}
			reader, row := "m2", "m2"
			if tc.friend {
				reader, row = "amy", "friend.amy"
				_, _, _, err := st.SyncFriends(ctx, []store.FriendSpec{{Name: reader, Width: 2, Class: "flash", Roles: "reader"}})
				require.NoError(t, err)
				require.NoError(t, st.B.RowsAdd(ctx, st.Names.Table(sprint.Fleet), []string{row}))
			} else {
				run(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: reader, Width: 2}))
				require.NoError(t, st.B.RowsAdd(ctx, st.Names.Table(sprint.Readers), []string{"reader-m2"}))
				require.NoError(t, st.BeatReaders(ctx))
			}
			run(store.AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"p"}, Brief: "p: read a pushed head\n"}))
			for _, move := range []struct {
				col string
				set map[string]string
			}{{sprint.Working, nil}, {sprint.Review, map[string]string{"attempt": "1", "head": "abc123", "result": "ok"}}} {
				run(store.Step{Verb: "seed " + move.col, Load: store.All, Plan: func(s *sprint.Snapshot) sprint.Plan {
					c := s.Work.Card("p")
					return sprint.Plan{Units: []sprint.Unit{{Key: "p", Changes: []sprint.Change{{Table: sprint.Work, Entry: ntable.BatchMemberEntry{ID: "p", Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev), Place: &ntable.PlaceExpect{Row: c.Row, Col: c.Col}}, Move: &ntable.MemberMoveOp{Row: "s1", Col: move.col}, Set: move.set}}}}}}
				}})
			}
			for i := 0; i < tc.ended; i++ {
				id := sprint.ReadCardID("p", 1, reader)
				if i > 0 {
					id += fmt.Sprintf(".g%d", i)
				}
				run(store.Step{Verb: "seed read", Load: store.All, Plan: func(s *sprint.Snapshot) sprint.Plan {
					return sprint.Plan{Units: []sprint.Unit{{Key: id, Changes: []sprint.Change{{Table: sprint.Fleet, Entry: ntable.BatchMemberEntry{ID: id, Expect: &ntable.MemberExpect{Absent: true}, Create: &ntable.MemberCreateOp{Row: row, Col: sprint.Working, Score: 1}, Set: map[string]string{"kind": "read", sprint.FieldReadCard: "1", "primary": "p", "stream": "s1", "reader": reader, "attempt": "1", "head": "abc123", "gen": "1"}}}}}}}
				}})
				run(store.ReadStep(sprint.ReadReq{As: row, Sel: sprint.Sel{IDs: []string{id}}, Verdict: "broken", Finding: "STEP 2: origin has no branch; push the head before asking", Missing: map[string]sprint.MissingBranch{id: {Named: "missing", Holds: "pushed"}}}))
			}
			if tc.friend {
				require.NoError(t, st.B.RowsDel(ctx, st.Names.Table(sprint.Fleet), []string{row}))
			}
			run(store.Step{Verb: "seed cannot ask", Load: store.All, Plan: func(s *sprint.Snapshot) sprint.Plan {
				return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Judgment, Type: sprint.NCannotAsk, Stream: "s1", Primaries: []string{"p"}, Attempt: 1, At: s.Now, What: "read must be asked again"}}}
			}})
			var answers []string
			for _, o := range snap().Open {
				if o.Note.Type == sprint.NCannotAsk {
					answers = append(answers, o.Note.ID)
				}
			}
			require.Len(t, answers, 1)
			baseID := sprint.ReadCardID("p", 1, reader)
			diagnostic, diagnosticCode, diagnosticErr := ta.runVerbOut("repair", "--dry-run")
			out, code, errb := ta.runVerbOut("ask", "p", "--answers", answers[0])
			require.Zero(t, code, "ask baseline %s: %s%s", tc.name, out, errb)
			t.Logf("ask %s succeeded", tc.name)
			require.Zero(t, diagnosticCode, "repair: %s%s", diagnostic, diagnosticErr)
			require.Contains(t, diagnostic, baseID+" is held by an ended record; ask mints "+fmt.Sprintf("%s.g%d", baseID, tc.ended))
			s := snap()
			next := fmt.Sprintf("%s.g%d", sprint.ReadCardID("p", 1, reader), tc.ended)
			require.NotNil(t, s.Fleet.Placed(next))
			ids := sprint.ReadCardGenIDs("p", 1, reader, tc.ended-1)
			records, err := st.Records(ctx, sprint.Fleet, ids)
			require.NoError(t, err)
			require.Len(t, records, tc.ended)
			for _, c := range records {
				require.False(t, c.Placed())
				require.Equal(t, sprint.RetiredByMissingBranch, c.F("retired_by"))
			}
		})
	}
}
