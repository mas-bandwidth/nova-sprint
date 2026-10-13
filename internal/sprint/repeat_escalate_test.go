package sprint_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// repeatRig is a hand-built snapshot: one NBound judgment on a primary in ready,
// ticked through TickOverdue (reraiseHeld) with time passed in. No real time,
// sockets, network or services.
type repeatRig struct {
	s      *sprint.Snapshot
	pushes int
	escs   []sprint.Note
}

func newRepeatRig() *repeatRig {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	s := &sprint.Snapshot{Now: now, Work: sprint.NewTable(sprint.Work), Readers: sprint.NewTable(sprint.Readers),
		Merge: sprint.NewTable(sprint.Merge), Fleet: sprint.NewTable(sprint.Fleet), Coordinator: "coordinator"}
	s.Work.Put(&sprint.Card{ID: "s1-1", Row: "s1", Col: sprint.Ready, Fields: map[string]string{"attempt": "1", "tier": "flash", "result": ""}})
	j := sprint.Note{ID: "j1", Kind: sprint.Judgment, Type: sprint.NBound, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, At: now, Who: "machine"}
	s.Open = []sprint.Open{{Key: sprint.OpenKey("j1", "s1-1"), Note: j}}
	return &repeatRig{s: s}
}

func (r *repeatRig) tick(at time.Time) {
	r.s.Now = at
	p, _ := sprint.TickOverdue(r.s, sprint.TickReq{Who: sprint.MachineActor})
	for _, n := range p.Notes {
		switch {
		case n.Type == sprint.NRaisedAgain && strings.Contains(n.What, "still holds s1-1"):
			r.pushes++
		case n.Type == sprint.NSeatNotMoving:
			r.escs = append(r.escs, n)
		}
	}
	// apply the plan: closes leave, new notes enter (judgments open, acknowledged held)
	closed := map[string]bool{}
	for _, o := range p.Closes {
		closed[o.Key] = true
	}
	var open []sprint.Open
	for _, o := range r.s.Open {
		if !closed[o.Key] {
			open = append(open, o)
		}
	}
	var acked []sprint.Open
	for _, o := range r.s.Acked {
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
		if n.ID == "" {
			n.ID = "n-" + n.Type + "-" + at.Format("150405")
		}
		switch n.Kind {
		case sprint.Judgment:
			for _, sub := range n.Subjects() {
				open = append(open, sprint.Open{Key: sprint.OpenKey(n.ID, sub), Note: n})
			}
		case sprint.Acknowledged:
			sub := ""
			if len(n.Primaries) > 0 {
				sub = n.Primaries[0]
			}
			if n.Type == sprint.NHeldRaised {
				sub = n.What
			}
			acked = append(acked, sprint.Open{Key: sprint.OpenKey(n.ID, sub), Note: n})
		}
	}
	r.s.Open, r.s.Acked = open, acked
}

func TestAnUnchangedJudgmentIsPushedAtMostNTimesThenEscalated(t *testing.T) {
	t.Parallel()
	r := newRepeatRig()
	base := r.s.Now
	for i := 2; i <= 12; i++ {
		r.tick(base.Add(time.Duration(i) * 10 * time.Minute))
	}
	assert.Equal(t, 3, r.pushes, "exactly 3 unchanged pushes, then no further NRaisedAgain")
	require.Len(t, r.escs, 1, "one NSeatNotMoving escalation")
	assert.Contains(t, r.escs[0].What, "j1", "the escalation names the judgment: %s", r.escs[0].What)
	assert.Contains(t, r.escs[0].What, sprint.NBound, "the escalation names its type: %s", r.escs[0].What)
	assert.Contains(t, r.escs[0].What, "s1-1", "the escalation names its cards: %s", r.escs[0].What)
	assert.Contains(t, r.escs[0].What, "3", "the escalation names the count: %s", r.escs[0].What)
	assert.Equal(t, []string{sprint.SeatNotMovingDecision}, r.escs[0].Decisions)
}

func TestAChangeResetsTheRepeatCount(t *testing.T) {
	t.Parallel()
	r := newRepeatRig()
	base := r.s.Now
	r.tick(base.Add(20 * time.Minute))
	r.tick(base.Add(30 * time.Minute))
	require.Equal(t, 2, r.pushes)
	// the card's attempt moves: the fingerprint changes and the count resets
	r.s.Work.Card("s1-1").Fields["attempt"] = "2"
	r.tick(base.Add(40 * time.Minute))
	assert.Equal(t, 3, r.pushes, "a change resets the count: pushes resume")
	r.tick(base.Add(50 * time.Minute))
	r.tick(base.Add(60 * time.Minute))
	assert.Equal(t, 5, r.pushes, "three more unchanged pushes after the change")
	r.tick(base.Add(70 * time.Minute))
	assert.Equal(t, 5, r.pushes, "no further push once the bound is reached")
	require.Len(t, r.escs, 1, "then it escalates once")
	assert.True(t, strings.Contains(r.escs[0].What, "j1"))
}

func TestRepeatPushesReadsItsDefault(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 3, (&sprint.Snapshot{}).RepeatBound(), "no sprint row value gives N = 3")
	assert.Equal(t, 5, (&sprint.Snapshot{RepeatPushes: "5"}).RepeatBound(), "a value of 5 gives 5")
	assert.Equal(t, 3, (&sprint.Snapshot{RepeatPushes: "0"}).RepeatBound(), "a value under 1 gives the default")
	assert.Equal(t, 3, (&sprint.Snapshot{RepeatPushes: "x"}).RepeatBound(), "an unparseable value gives the default")
}

// The no-target judgment is kept by the tick: raised while the owner names no
// row, closed when the owner names one (Stella's #211 finding 3).
func TestTheNoTargetJudgmentIsKeptByTheTick(t *testing.T) {
	t.Parallel()
	assert.True(t, sprint.TickKept(sprint.NOwnerNoTarget), "the tick keeps the no-target judgment")
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	s := &sprint.Snapshot{Now: now, Work: sprint.NewTable(sprint.Work), Readers: sprint.NewTable(sprint.Readers),
		Merge: sprint.NewTable(sprint.Merge), Fleet: sprint.NewTable(sprint.Fleet), Coordinator: "coordinator", Owner: "glenn"}
	r := &repeatRig{s: s}
	r.tick(now.Add(10 * time.Minute))
	found := false
	for _, o := range s.Open {
		if o.Note.Type == sprint.NOwnerNoTarget {
			found = true
			assert.Equal(t, []string{"ack", "wait"}, o.Note.Decisions)
		}
	}
	require.True(t, found, "an owner that is no row raises the no-target judgment")
	s.Friends = []sprint.FriendSeat{{Name: "glenn"}}
	r.tick(now.Add(20 * time.Minute))
	for _, o := range s.Open {
		assert.NotEqual(t, sprint.NOwnerNoTarget, o.Note.Type, "a known owner closes the no-target judgment")
	}
}
