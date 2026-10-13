package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A pass judgment with no primaries (stream-level, as NCoordinatorBehind is
// raised) must escalate once and stay escalated: counting, creation and
// closure all read the same canonical episode cards (cardsHeldBy), so the
// closure never sees a different fingerprint than creation wrote and closes
// and reopens it every tick (Stella's #211 finding 2).
func TestPassEscalationWithNoPrimariesDoesNotFlap(t *testing.T) {
	t.Parallel()
	base := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	s := &Snapshot{Now: base, Work: NewTable(Work), Readers: NewTable(Readers),
		Merge: NewTable(Merge), Fleet: NewTable(Fleet), Coordinator: "coordinator"}
	s.Work.Put(&Card{ID: "s1-1", Row: "s1", Col: Ready, Fields: map[string]string{"attempt": "1", "tier": "flash"}})
	cb := Note{ID: "cb1", Kind: Judgment, Type: NCoordinatorBehind, StreamLevel: true, At: base, Who: "machine",
		What: "level 0: 1 judgments wait past their deadline", Decisions: []string{"act", "wait"}}
	s.Open = []Open{{Key: OpenKey("cb1", StreamSubject("")), Note: cb}}
	conds := []cond{{typ: NCoordinatorBehind, streamLevel: true,
		what: "level 0: 1 judgments wait past their deadline", decisions: []string{"act", "wait"}}}
	escs := 0
	apply := func(p Plan) {
		closed := map[string]bool{}
		for _, o := range p.Closes {
			closed[o.Key] = true
		}
		var open, acked []Open
		for _, o := range s.Open {
			if !closed[o.Key] {
				open = append(open, o)
			}
		}
		for _, o := range s.Acked {
			if !closed[o.Key] {
				acked = append(acked, o)
			}
		}
		for _, u := range p.Updates {
			for i := range open {
				if open[i].Note.ID == u.ID {
					open[i].Note = u
				}
			}
		}
		for _, n := range p.Notes {
			if n.Type == NSeatNotMoving {
				escs++
			}
			if n.ID == "" {
				n.ID = "n-" + n.Type
			}
			switch n.Kind {
			case Judgment:
				for _, sub := range n.Subjects() {
					open = append(open, Open{Key: OpenKey(n.ID, sub), Note: n})
				}
			case Acknowledged:
				sub := n.What
				acked = append(acked, Open{Key: OpenKey(n.ID, sub), Note: n})
			}
		}
		s.Open, s.Acked = open, acked
	}
	for i := 1; i <= 8; i++ {
		s.Now = base.Add(time.Duration(i) * PassEvery)
		r := TickReq{Who: MachineActor}
		apply(reraiseHeld(s, r)) // the closure half of the tick
		var p Plan
		reraise(&p, s, conds, r) // the pass's raise-again half
		apply(p)
	}
	require.Equal(t, 1, escs, "one escalation across eight ticks, never closed and reopened")
	open := 0
	for _, o := range s.Open {
		if o.Note.Type == NSeatNotMoving {
			open++
		}
	}
	assert.Equal(t, 1, open, "the escalation is still open")
}
