package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A deadline judgment exists only while a take is live (docs/SPEC-SPRINT.md
// section 8, Notifications; tla/SprintRules.tla LateOnlyWhileLive): a work card
// withdrawn from its take (a provider failed it, an environment exited 134)
// raises none while its primary waits ready, and one open when its take is
// withdrawn closes. Each world is in memory: s.Now is moved by hand, no clock,
// no store.

// lateTake is setup with s1-1 at its sixth attempt, its work card s1-1.w6
// dealt and taken, and the clock moved 3 hours past the take: past
// DeadlineUnfinished.
func lateTake(t *testing.T) *world {
	t.Helper()
	w := setup(t, 1)
	// five attempts before, the coordinator's last decision at the fifth (FieldDecidedAttempt),
	// so the attempt cap does not hold the sixth
	w.s.Work.Card("s1-1").Fields["attempt"] = "5"
	w.s.Work.Card("s1-1").Fields[FieldDecidedAttempt] = "5"
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Fleet.Card("s1-1.w6")
	require.NotNil(t, wc, "the deal cut the sixth attempt's work card")
	w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
	w.tick(3 * time.Hour)
	require.Greater(t, 3*time.Hour, DeadlineUnfinished)
	require.Equal(t, stamp(w.s.Now.Add(-3*time.Hour)), w.s.Fleet.Card("s1-1.w6").F("first_taken"))
	return w
}

// withdrawTake withdraws the work card from its take, as last night's provider
// failures did: the card to withdrawn, its primary back to ready, no live take.
func withdrawTake(w *world) {
	w.t.Helper()
	presenceWith(w) // no member beats: its take ends and the card is withdrawn
	require.Equal(w.t, Withdrawn, w.s.Fleet.Card("s1-1.w6").Col)
	require.Equal(w.t, Ready, w.state("s1-1"))
}

// workLate is the NWorkLate notes a plan writes on card.
func workLate(p Plan, card string) []Note {
	var out []Note
	for _, n := range p.Notes {
		if n.Type == NWorkLate && n.Card == card {
			out = append(out, n)
		}
	}
	return out
}

// Last night's case (epoch 20): a withdrawn work card whose primary is ready,
// its first take 3 hours ago, raised "a work card is past its deadline ... at
// withdrawn" at every tick and at each wait's expiry. It raises none.
func TestAWithdrawnTakeRaisesNoDeadline(t *testing.T) {
	t.Parallel()
	w := lateTake(t)
	withdrawTake(w)
	p, _ := TickDeadlines(w.s, TickReq{})
	assert.Empty(t, workLate(p, "s1-1.w6"), "a withdrawn take has no deadline")
}

// A late judgment raised while the take was live closes when its take is
// withdrawn: its cause no longer stands (LateStands).
func TestAnOpenDeadlineRetiresWhenItsTakeIsWithdrawn(t *testing.T) {
	t.Parallel()
	w := lateTake(t)
	p, _ := TickDeadlines(w.s, TickReq{})
	require.Len(t, workLate(p, "s1-1.w6"), 1, "the live take past its deadline is judged")
	w.must(p)
	withdrawTake(w)
	var open []Open
	for _, o := range w.s.Open {
		if o.Note.Type == NWorkLate && o.Note.Card == "s1-1.w6" {
			open = append(open, o)
		}
	}
	require.Len(t, open, 1, "the late judgment is open on the withdrawn card")
	p, _ = TickDeadlines(w.s, TickReq{})
	var closed []Open
	for _, c := range p.Closes {
		if c.Note.Type == NWorkLate && c.Note.Card == "s1-1.w6" {
			closed = append(closed, c)
		}
	}
	require.Len(t, closed, 1, "the tick closes the late judgment of a withdrawn take")
	assert.Equal(t, open[0].Key, closed[0].Key)
	assert.Empty(t, workLate(p, "s1-1.w6"), "and raises none in its place")
}

// A live take past its deadline still raises exactly one late judgment: the
// rule narrows the withdrawn case and nothing else.
func TestALiveTakePastItsDeadlineStillRaises(t *testing.T) {
	t.Parallel()
	w := lateTake(t)
	require.Equal(t, Working, w.s.Fleet.Card("s1-1.w6").Col)
	p, _ := TickDeadlines(w.s, TickReq{})
	late := workLate(p, "s1-1.w6")
	require.Len(t, late, 1)
	assert.Contains(t, late[0].What, "not finished")
}
