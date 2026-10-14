package sprint_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// A need that is absent, dropped, in an unknown stream, or held at its bound with no decision is
// refused at add and at any brief import, all or nothing (v1.2.17,
// a-missing-or-blocked-need-is-found-at-check-and-while-running; the owner, 2026-10-13: "This
// needs to stop happening. A missing card stalls the sprint."). The store is the in-memory one,
// its clock the test's (tla/NeedsBlocked.tla AdmittedNeedsResolved, witness admitheld).

type needsRig struct {
	t   *testing.T
	ctx context.Context
	st  *store.Store
	mu  sync.Mutex
	now time.Time
}

func newNeedsRig(t *testing.T) *needsRig {
	t.Helper()
	r := &needsRig{t: t, ctx: context.Background(), now: time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)}
	m := configMem()
	n := 0
	r.st = &store.Store{B: m, Actor: "coordinator", Now: func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
		NewID: func() string { r.mu.Lock(); defer r.mu.Unlock(); n++; return fmt.Sprint(n) }, Sleep: func(time.Duration) {}}
	require.NoError(t, r.st.Init(r.ctx))
	require.NoError(t, m.SetCoordinator(r.ctx, "coordinator"))
	return r
}

func (r *needsRig) run(step store.Step) store.Result {
	r.t.Helper()
	res, err := r.st.Run(r.ctx, step)
	require.NoError(r.t, err, step.Verb)
	return res
}

func (r *needsRig) snap() *sprint.Snapshot {
	r.t.Helper()
	s, err := r.st.Load(r.ctx, []string{sprint.Work, sprint.Merge}, nil)
	require.NoError(r.t, err)
	return s
}

// add is add of the briefs' cards to the stream, one step, as add --brief-dir makes it.
func (r *needsRig) add(stream string, cards ...sprint.CardAdd) store.Result {
	r.t.Helper()
	return r.run(store.AddStep(sprint.AddReq{Stream: stream, Cards: cards, Who: "coordinator"}))
}

// bound opens the bound judgment on card, opened ago before now (a card at its attempt bound
// with no decision, as the tick raises it: steps_tick.go AtRedealBound).
func (r *needsRig) bound(card, stream string, ago time.Duration) {
	r.t.Helper()
	res := r.run(store.Step{Verb: "bound", Load: []string{sprint.Work}, Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Notes: []sprint.Note{{Kind: sprint.Judgment, Type: sprint.NBound, Stream: stream, Primaries: []string{card}, Count: 1,
			At: s.Now.Add(-ago), Decisions: append([]string(nil), sprint.Decisions[sprint.NBound]...)}}}
	}})
	require.Empty(r.t, res.Refused)
}

// decide closes the open bound judgment on card, as a rework's decision does (Rework's Closes:
// closesFor NWorkFailed, NBound, NStranded).
func (r *needsRig) decide(card string) {
	r.t.Helper()
	res := r.run(store.Step{Verb: "rework", Load: []string{sprint.Work}, Plan: func(s *sprint.Snapshot) sprint.Plan {
		var closes []sprint.Open
		for _, o := range s.Open {
			if o.Note.Type == sprint.NBound && o.Subject() == card {
				closes = append(closes, o)
			}
		}
		return sprint.Plan{Units: []sprint.Unit{{Key: card, Closes: closes, Moved: card + " reworked: the bound decided"}}}
	}})
	require.Empty(r.t, res.Refused)
}

func (r *needsRig) onTable(id string) bool { return r.snap().Work.Placed(id) != nil }

// Last night: a-route-may-be-frontier ready in tools-v1217 with an open bound judgment naming it
// for 3 h; a-funded-provider-is-a-config-row-and-always-shown needs it.
func TestAddRefusesANeedAtItsBoundWithNoDecision(t *testing.T) {
	t.Parallel()
	r := newNeedsRig(t)
	require.Empty(t, r.add("tools-v1217", sprint.CardAdd{ID: "a-route-may-be-frontier"}).Refused)
	r.bound("a-route-may-be-frontier", "tools-v1217", 3*time.Hour)

	res := r.add("tools-v1217", sprint.CardAdd{ID: "a-funded-provider-is-a-config-row-and-always-shown", Needs: []string{"a-route-may-be-frontier"}})
	require.Len(t, res.Refused, 1, "refused: %+v", res.Refused)
	why := res.Refused[0].Why
	assert.Contains(t, why, "needs a-route-may-be-frontier (held)")
	assert.Contains(t, why, "held by judgment ")
	assert.Contains(t, why, sprint.NBound+", open 3h0m0s")
	assert.Contains(t, why, "nova-sprint rework a-route-may-be-frontier --fix <text>")
	assert.Contains(t, why, "nova-sprint drop a-route-may-be-frontier --reason <text>")
	assert.False(t, r.onTable("a-funded-provider-is-a-config-row-and-always-shown"), "nothing written")

	r.decide("a-route-may-be-frontier")
	require.Empty(t, r.add("tools-v1217", sprint.CardAdd{ID: "a-funded-provider-is-a-config-row-and-always-shown", Needs: []string{"a-route-may-be-frontier"}}).Refused,
		"the rework's decision closed the bound: the need is resolved")
	assert.True(t, r.onTable("a-funded-provider-is-a-config-row-and-always-shown"))
}

// An add of three briefs, one needing a held card: all refused, naming every card and need, the
// table unchanged. A partial import strands the dependents inside it.
func TestABriefImportWithOneUnresolvedNeedWritesNothing(t *testing.T) {
	t.Parallel()
	r := newNeedsRig(t)
	require.Empty(t, r.add("s1", sprint.CardAdd{ID: "held-card"}).Refused)
	r.bound("held-card", "s1", time.Hour)
	before := len(r.snap().Work.Cards())

	res := r.add("s2", sprint.CardAdd{ID: "i1"}, sprint.CardAdd{ID: "i2", Needs: []string{"i1"}}, sprint.CardAdd{ID: "i3", Needs: []string{"held-card"}})
	require.Len(t, res.Refused, 3, "every card refused: %+v", res.Refused)
	for _, ref := range res.Refused {
		assert.Contains(t, ref.Why, "i3 needs held-card (held)", ref.Key)
		assert.Contains(t, ref.Why, "nothing is written", ref.Key)
	}
	assert.Len(t, r.snap().Work.Cards(), before, "the table unchanged")
	for _, id := range []string{"i1", "i2", "i3"} {
		assert.False(t, r.onTable(id), id)
	}
}

// A need whose card's stream has no control card in this epoch is refused.
func TestAnUnknownStreamNeedIsRefused(t *testing.T) {
	t.Parallel()
	r := newNeedsRig(t)
	// a card in a stream with no control card: written as no verb would (an older binary, a
	// migration)
	res := r.run(store.Step{Verb: "migrate", Load: []string{sprint.Work, sprint.Merge}, Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Rows: []sprint.RowAdd{{Table: sprint.Work, Row: "ghost"}, {Table: sprint.Merge, Row: "ghost"}},
			Units: []sprint.Unit{{Key: "ghost-1", Changes: []sprint.Change{{Table: sprint.Work, Entry: ntable.BatchMemberEntry{ID: "ghost-1",
				Expect: &ntable.MemberExpect{Absent: true}, Create: &ntable.MemberCreateOp{Row: "ghost", Col: sprint.Ready, Score: 1},
				Set: map[string]string{"kind": "primary", "stream": "ghost", "attempt": "0"}}}}}}}
	}})
	require.Empty(t, res.Refused)
	require.Nil(t, r.snap().StreamCtl("ghost"))

	got := r.add("s1", sprint.CardAdd{ID: "c1", Needs: []string{"ghost-1"}})
	require.Len(t, got.Refused, 1, "%+v", got.Refused)
	assert.Contains(t, got.Refused[0].Why, "needs ghost-1 (unknown stream): ghost-1's stream ghost has no control card in this epoch")
	assert.False(t, r.onTable("c1"))
}
