package sprint_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// TestAReadEnvironmentRefusalIsNotARedo replays the environment-return shapes
// through Read and the complete tick, including its automatic answer rules.
// The wall return is the r14 return at 2026-10-15T09:59:54Z. The staging
// return is the r2 return at 2026-10-14T22:37:44Z. Names are fixture roles;
// attempts and heads retain the observed pins. No git or host is consulted.
func TestAReadEnvironmentRefusalIsNotARedo(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, head, reason string
		attempt, exit      int
		stale              bool
	}{
		{"wall return", "25e1c27cc07119686bc77235e921996f90a1a33b", `no verdict (ran=false verdict="ok"): environment: the wall refused a write outside the job; Gate green`, 14, 0, false},
		{"staging return", "98310c8aab1813d614939e18522ae928544bb816", `no verdict (ran=false verdict=""): staging refused: head 98310c8aab1813d614939e18522ae928544bb816 is in neither the mirror /fixture/mirror.git nor origin https://example.invalid/repo.git`, 2, 0, false},
		{"stale pin return", "25e1c27cc07119686bc77235e921996f90a1a33b", "no verdict: read brief pin no longer matches the card head", 14, 0, true},
		{"harness exit 134", "25e1c27cc07119686bc77235e921996f90a1a33b", "no verdict (ran=false): aborted", 14, 134, false},
		{"harness exit 137", "25e1c27cc07119686bc77235e921996f90a1a33b", "no verdict (ran=false): killed", 14, 137, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newReadCardsRig(t, "m1", "m2", "m3")
			r.st.AnswerRules = true
			_, _, _, err := r.st.SetMachine(r.ctx, false)
			require.NoError(t, err)
			const id = "environment-replay"
			r.must(store.AddStep(sprint.AddReq{Stream: "s1", Cards: []sprint.CardAdd{{ID: id, Brief: "environment-replay: work (s1)\nREPO: example/repo\nThe task.\n"}}}))
			work := sprint.WorkCardID(id, tc.attempt)
			read := sprint.ReadCardID(id, tc.attempt, "m2")
			poke := func(table string, entries ...ntable.BatchMemberEntry) {
				t.Helper()
				table = r.st.Names.Table(table)
				_, err := r.m.Apply(r.ctx, ntable.BatchManifest{Schema: 1, Table: table, Epoch: "0",
					ExpectedTableRevision: fmt.Sprint(r.m.Revision(table)), OperationID: "seed-" + table, Actor: "test", Members: entries})
				require.NoError(t, err)
			}
			// Keep the independent work-attempt cap open so it cannot hide a redo.
			poke(sprint.Work, ntable.BatchMemberEntry{ID: id, Expect: &ntable.MemberExpect{Revision: fmt.Sprint(r.snap().Work.Card(id).Rev), Place: &ntable.PlaceExpect{Row: "s1", Col: sprint.Ready}}, Move: &ntable.MemberMoveOp{Row: "s1", Col: sprint.Review},
				Set: map[string]string{"attempt": fmt.Sprint(tc.attempt), "head": tc.head, "work": work, "result": "ok", sprint.FieldDecidedAttempt: fmt.Sprint(tc.attempt - 1)}})
			readHead := tc.head
			if tc.stale {
				readHead = "1111111111111111111111111111111111111111"
			}
			poke(sprint.Fleet,
				ntable.BatchMemberEntry{ID: work, Expect: &ntable.MemberExpect{Absent: true}, Create: &ntable.MemberCreateOp{Row: "m1", Col: sprint.Finished, Score: 1},
					Set: map[string]string{"kind": "work", "primary": id, "stream": "s1", "attempt": fmt.Sprint(tc.attempt), "member": "m1", "head": tc.head, "branch": "sprint/replay", "ok": "yes", "gen": "1", "redeals": "0"}},
				ntable.BatchMemberEntry{ID: read, Expect: &ntable.MemberExpect{Absent: true}, Create: &ntable.MemberCreateOp{Row: "m2", Col: sprint.Working, Score: 1},
					Set: map[string]string{"kind": "read", "primary": id, "stream": "s1", "attempt": fmt.Sprint(tc.attempt), "reader": "m2", "head": readHead, "gen": "1", sprint.FieldReadCard: "1"}})
			_, _, _, err = r.st.SetMachine(r.ctx, true)
			require.NoError(t, err)
			r.read(r.rec(read), sprint.ReadReq{Return: true, Reason: tc.reason, Exit: tc.exit})
			r.tick()
			s := r.snap()
			pr := s.Work.Card(id)
			assert.Equal(t, tc.attempt, pr.Int("attempt"), "the read does not create another work attempt")
			assert.Equal(t, tc.head, pr.F("head"))
			assert.Equal(t, sprint.Review, pr.Col)
			assert.Nil(t, s.Fleet.Card(sprint.WorkCardID(id, tc.attempt+1)), "no next work card")
			assert.Equal(t, "1", r.rec(work).F("gen"), "no work generation advances")
			assert.Equal(t, "0", r.rec(work).F("redeals"), "no work redeal is spent")
			reads := r.readCards(id)
			require.Len(t, reads, 1, "asked again")
			assert.Equal(t, tc.attempt, reads[0].Int("attempt"))
			assert.Equal(t, tc.head, reads[0].F("head"))
			t.Logf("attempt=%d head=%s work-gen=%s redeals=%s next-read=%s retired-by=%s", pr.Int("attempt"), pr.F("head"), r.rec(work).F("gen"), r.rec(work).F("redeals"), reads[0].ID, r.rec(read).F("retired_by"))

			// Exhaust the available readers and any environment re-asks. The
			// replay must distinguish a stranded read from a new work attempt.
			returns := 1
			for len(reads) > 0 && returns <= 2*sprint.EnvReadCap {
				r.read(reads[0], sprint.ReadReq{Return: true, Reason: tc.reason, Exit: tc.exit})
				r.tick()
				returns++
				reads = r.readCards(id)
				assert.Equal(t, tc.attempt, r.snap().Work.Card(id).Int("attempt"))
				assert.Equal(t, "1", r.rec(work).F("gen"))
				assert.Equal(t, "0", r.rec(work).F("redeals"))
			}
			require.Empty(t, reads, "the existing return bound settles")
			var judgments []string
			for _, open := range r.snap().Open {
				if open.Note.Type == sprint.NCannotAsk || open.Note.Type == sprint.NBound {
					judgments = append(judgments, open.Note.What)
				}
			}
			assert.NotEmpty(t, judgments, "exhaustion is judged, not silently stuck")
			t.Logf("after %d returns: attempt=%d work-gen=%s redeals=%s judgments=%v", returns, r.snap().Work.Card(id).Int("attempt"), r.rec(work).F("gen"), r.rec(work).F("redeals"), judgments)
		})
	}
}
