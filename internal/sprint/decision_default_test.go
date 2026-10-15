package sprint_test

import (
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// The actual in-memory store, explicit time, no live transport. This regression
// uses only pre-existing entry points so the base fails behaviorally, not to compile.
func TestDecisionDefaultParksBoundAtTwiceDeadline(t *testing.T) {
	r := newHoldRig(t, 1, 0)
	r.must(store.NoteStep("test", sprint.Note{Kind: sprint.Judgment, Type: sprint.NBriefWrong, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, What: "attempt budget exhausted", At: r.now}))
	start := r.now
	pass := func(d time.Duration) {
		r.mu.Lock()
		r.now = start.Add(d)
		r.mu.Unlock()
		r.must(store.TickPartStep("overdue", sprint.TickOverdue, sprint.TickReq{}, nil, nil, nil))
		r.must(store.DrainStep())
	}
	pass(2*sprint.DeadlineJudgment - time.Second)
	require.Empty(t, r.snap().Work.Card("s1-1").F(sprint.FieldHeld))
	pass(2 * sprint.DeadlineJudgment)
	c := r.snap().Work.Card("s1-1")
	require.Equal(t, sprint.Waiting, c.Col, "bound primary must be parked at twice its judgment deadline")
	require.NotEmpty(t, c.F(sprint.FieldHeld))
	require.Contains(t, c.F("held_reason"), "attempt budget exhausted")
	require.Equal(t, "server-default", c.F("held_by"))
	var count int
	for _, n := range r.allNotes() {
		if n.Who == "server-default" && n.Answers != "" {
			count++
		}
	}
	require.Positive(t, count, "default must identify its actor and judgment in the audit log")
	pass(3 * sprint.DeadlineJudgment)
	require.Equal(t, sprint.Waiting, r.snap().Work.Card("s1-1").Col)
	r.must(store.ReleaseStep(sprint.ReleaseReq{IDs: []string{"s1-1"}, Reason: "reviewed; resume", Who: "coordinator", Coordinator: "coordinator"}))
	r.must(store.DrainStep())
	require.Empty(t, r.snap().Work.Card("s1-1").F(sprint.FieldHeld), "coordinator can reverse the safe hold")
}

func TestDecisionDefaultNeverAcceptsOrDrops(t *testing.T) {
	for _, typ := range []string{sprint.NReadyToAccept, sprint.NConflict, sprint.NRejected} {
		t.Run(typ, func(t *testing.T) {
			r := newHoldRig(t, 1, 0)
			r.must(store.NoteStep("test", sprint.Note{Kind: sprint.Judgment, Type: typ, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, At: r.now}))
			r.mu.Lock()
			r.now = r.now.Add(3 * sprint.DeadlineJudgment)
			r.mu.Unlock()
			r.must(store.TickPartStep("overdue", sprint.TickOverdue, sprint.TickReq{}, nil, nil, nil))
			r.must(store.DrainStep())
			c := r.snap().Work.Placed("s1-1")
			require.NotNil(t, c)
			require.Equal(t, sprint.Ready, c.Col)
			require.NotNil(t, r.openNote(typ, "s1-1"))
		})
	}
}

func TestDecisionDefaultReviewParkRetainsHeadAndLiveRead(t *testing.T) {
	r := newHoldRig(t, 1, 0)
	r.must(store.SetStep(sprint.SetReq{ReadCards: sprint.SwitchOn, Who: "coordinator"}))
	move := func(col string) {
		r.must(store.Step{Verb: "fixture", Load: store.All, Plan: func(s *sprint.Snapshot) sprint.Plan {
			c := s.Work.Card("s1-1")
			return sprint.Plan{Units: []sprint.Unit{{Key: c.ID, Changes: []sprint.Change{{Table: sprint.Work, Entry: ntable.BatchMemberEntry{ID: c.ID, Expect: &ntable.MemberExpect{Revision: fmt.Sprint(c.Rev), Place: &ntable.PlaceExpect{Row: c.Row, Col: c.Col}}, Move: &ntable.MemberMoveOp{Row: c.Row, Col: col}, Set: map[string]string{"attempt": "1", "head": "abc123", "result": "ok", "work": "s1-1.w1"}}}}}}}
		}})
		r.must(store.DrainStep())
	}
	move(sprint.Working)
	move(sprint.Review)
	r.must(store.Step{Verb: "fixture read", Load: store.All, Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Units: []sprint.Unit{{Key: "s1-1.r1.m2", Changes: []sprint.Change{{Table: sprint.Fleet, Entry: ntable.BatchMemberEntry{ID: "s1-1.r1.m2", Expect: &ntable.MemberExpect{Absent: true}, Create: &ntable.MemberCreateOp{Row: "m2", Col: sprint.Working}, Set: map[string]string{"kind": "read", "primary": "s1-1", "stream": "s1", "attempt": "1", "gen": "1", "head": "abc123", sprint.FieldReadCard: "1", "taken": r.now.Format(time.RFC3339), "asked": r.now.Format(time.RFC3339)}}}}}}}
	}})
	r.must(store.DrainStep())
	before := r.snap().Fleet.Card("s1-1.r1.m2").Fields
	r.must(store.NoteStep("fixture bound", sprint.Note{Kind: sprint.Judgment, Type: sprint.NBound, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, At: r.now.Add(-2 * sprint.DeadlineJudgment), What: "budget exhausted"}))
	r.mu.Lock()
	r.now = r.now.Add(2 * sprint.DeadlineJudgment)
	r.mu.Unlock()
	r.must(store.TickPartStep("overdue", sprint.TickOverdue, sprint.TickReq{}, nil, nil, nil))
	r.must(store.DrainStep())
	require.Equal(t, sprint.Waiting, r.snap().Work.Card("s1-1").Col)
	// The actual read-card deal pass used to retire reads whenever primary != Review.
	r.must(store.TickPartStep("deal", sprint.TickDeal, sprint.TickReq{}, nil, nil, nil))
	r.must(store.DrainStep())
	require.NotNil(t, r.snap().Fleet.Placed("s1-1.r1.m2"))
	require.Equal(t, before, r.snap().Fleet.Card("s1-1.r1.m2").Fields)
	r.must(store.ReleaseStep(sprint.ReleaseReq{IDs: []string{"s1-1"}, Who: "coordinator", Coordinator: "coordinator", Reason: "reviewed"}))
	r.must(store.DrainStep())
	c := r.snap().Work.Card("s1-1")
	require.Equal(t, sprint.Review, c.Col)
	require.Equal(t, "abc123", c.F("head"))
	require.Equal(t, 1, c.Int("attempt"))
	require.Empty(t, c.F(sprint.FieldHeld))
	require.Empty(t, c.F("decision_held_from"))
	require.Equal(t, before, r.snap().Fleet.Card("s1-1.r1.m2").Fields)
}

func TestDecisionDefaultCannotAskWaitsThirtyMinutes(t *testing.T) {
	r := newHoldRig(t, 1, 0)
	r.must(store.NoteStep("fixture", sprint.Note{Kind: sprint.Judgment, Type: sprint.NCannotAsk, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, At: r.now, What: "no eligible reader"}))
	r.mu.Lock()
	r.now = r.now.Add(2 * sprint.DeadlineJudgment)
	r.mu.Unlock()
	r.must(store.TickPartStep("overdue", sprint.TickOverdue, sprint.TickReq{}, nil, nil, nil))
	r.must(store.DrainStep())
	require.Nil(t, r.openNote(sprint.NCannotAsk, "s1-1"))
	var waited bool
	for _, o := range r.snap().Acked {
		if o.Note.Type == sprint.NCannotAsk {
			waited = true
			require.Equal(t, r.now.Add(30*time.Minute), o.Note.Review)
			require.Equal(t, "server-default", o.Note.Who)
		}
	}
	require.True(t, waited)
}

// The folded deadline lane must preserve the actionable no-wait remedy from
// the inbox, and identify cards even when the reason does not name them.
func TestDecisionBatchPreservesCardHoldingRemedies(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	for _, typ := range sprint.WaitRefusedKinds() {
		t.Run(typ, func(t *testing.T) {
			n := sprint.Note{ID: "judgment-1", Kind: sprint.Judgment, Type: typ, Primaries: []string{"s1-1"}, At: now.Add(-sprint.DeadlineJudgment), What: "needs a decision"}
			for _, lane := range []string{"coordinator", "owner"} {
				set, body, _ := sprint.DecisionBatch(now, []sprint.Open{{Key: sprint.OpenKey(n.ID, "s1-1"), Note: n}}, nil, lane, nil, nil)
				require.Equal(t, n.ID, set)
				require.Contains(t, body, "subjects=s1-1")
				require.Contains(t, body, "act: "+sprint.WaitRefusedAction(n))
				require.NotContains(t, body, "nova-sprint wait")
			}
		})
	}
}

func TestDecisionBatchNamesOnlyOpenSubjects(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	n := sprint.Note{ID: "judgment-1", Kind: sprint.Judgment, Type: sprint.NWorkFailed, Primaries: []string{"answered-card", "open-card"}, At: now.Add(-sprint.DeadlineJudgment)}
	for _, subject := range []string{"open-card", sprint.StreamSubject("stream-a"), sprint.SprintSubject} {
		t.Run(subject, func(t *testing.T) {
			_, body, _ := sprint.DecisionBatch(now, []sprint.Open{{Key: sprint.OpenKey(n.ID, subject), Note: n}}, nil, "coordinator", nil, nil)
			require.Contains(t, body, "subjects="+subject+" age=")
			require.NotContains(t, body, "answered-card")
			require.NotContains(t, body, "cards=")
		})
	}
}

func TestDecisionBatchDoesNotInventSubjectsWithoutOpenKeys(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	n := sprint.Note{ID: "judgment", Kind: sprint.Judgment, Type: sprint.NWorkFailed, Primaries: []string{"answered-card", "unknown-card"}, At: now.Add(-sprint.DeadlineJudgment)}
	_, body, _ := sprint.DecisionBatch(now, []sprint.Open{{Note: n}}, nil, "coordinator", nil, nil)
	require.NotContains(t, body, "answered-card")
	require.NotContains(t, body, "unknown-card")
}

func TestDecisionBatchTierCardsExcludeMovedAndRetieredPrimaries(t *testing.T) {
	r := newHoldRig(t, 4, 0)
	s := r.snap()
	// Only the first card is still on the original ready tier. One has been
	// dealt; another has changed tier even though it remains ready.
	s.Work.Card("s1-2").Col = sprint.Working
	s.Work.Card("s1-3").Fields["brief"] = "RESULT: c tier: pro\n"
	s.Work.Card("s1-3").Fields[sprint.FieldTierNow] = "pro"
	s.Work.Card("s1-4").Col = sprint.Review
	s.Work.Card("s1-4").Fields["result"] = "failed"
	n := sprint.Note{ID: "tier-judgment", Kind: sprint.Judgment, Type: sprint.NNoRoute, StreamLevel: true, Stream: sprint.TierSubject("flash"), Primaries: []string{"s1-1", "s1-2", "s1-3", "s1-4"}, At: s.Now.Add(-sprint.DeadlineJudgment), What: "4 primaries of tier flash wait: no route serves it"}
	for _, lane := range []string{"coordinator", "owner"} {
		_, body, _ := sprint.DecisionBatch(s.Now, []sprint.Open{{Key: sprint.OpenKey(n.ID, sprint.StreamSubject(n.Stream)), Note: n}}, nil, lane, nil, s)
		require.Contains(t, body, "cards=s1-1\n")
		require.Contains(t, body, "1 primaries of tier flash wait:")
		require.NotContains(t, body, "s1-2")
		require.NotContains(t, body, "s1-3")
		require.NotContains(t, body, "s1-4")
		require.NotContains(t, body, "4 primaries")
	}
}

func TestDecisionBatchTierDoesNotCountAlreadyRoutedReads(t *testing.T) {
	for _, col := range []string{sprint.Asked, sprint.Reading, sprint.OK} {
		t.Run(col, func(t *testing.T) {
			r := newHoldRig(t, 1, 0)
			s := r.snap()
			pr := s.Work.Card("s1-1")
			pr.Col = sprint.Review
			pr.Fields["result"], pr.Fields["head"] = "ok", "head"
			s.Work.SetProp(sprint.PropReadsNeeded, "1")
			s.Readers.Put(&sprint.Card{ID: sprint.ReadCardID(pr.ID, pr.Int("attempt"), "reader-a"), Row: "reader-a", Col: col, Fields: map[string]string{"primary": pr.ID, "head": "head", sprint.FieldRoute: "flash-route"}})
			require.Zero(t, sprint.ReadsWanted(s, pr), "fixture already has its required read")
			n := sprint.Note{ID: "tier-judgment", Kind: sprint.Judgment, Type: sprint.NNoRoute, StreamLevel: true, Stream: sprint.TierSubject("flash"), Primaries: []string{pr.ID}, At: s.Now.Add(-sprint.DeadlineJudgment), What: "1 primaries of tier flash wait: no route serves it"}
			_, body, _ := sprint.DecisionBatch(s.Now, []sprint.Open{{Key: sprint.OpenKey(n.ID, sprint.StreamSubject(n.Stream)), Note: n}}, nil, "coordinator", nil, s)
			require.Contains(t, body, "0 primaries of tier flash wait:")
			require.NotContains(t, body, "cards=")
			require.NotContains(t, body, pr.ID)
		})
	}
}
