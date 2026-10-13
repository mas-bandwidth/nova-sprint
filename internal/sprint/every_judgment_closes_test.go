package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every judgment kind has a verb that closes it (card
// every-judgment-kind-has-a-verb-that-closes-it; tla/SprintRules.tla
// NoRouteTick / NoRouteClosesWhenServed, LsLand): the kinds are read from
// the one table (Decisions + TickDecisions + the kinds notified outside
// those tables), so a new kind without a closing verb fails here.

func judgmentKindDecisions() map[string][]string {
	all := map[string][]string{}
	for typ, ds := range Decisions {
		all[typ] = ds
	}
	for typ, ds := range TickDecisions {
		all[typ] = ds
	}
	all[NLandingStall] = TickDecisions[NLandingStall]
	all[NLanderRefuses] = LandRefusalDecisions
	all[NLanderFails] = LandRefusalDecisions
	// kinds raised outside the two tables, with their own decision sources:
	all[NSeatSilent] = []string{SeatSilentDecision}   // seat_answers.go: the command is in the decision's text
	all[NCostGap] = CostGapDecisions                  // cost_reconcile.go
	all[NNeedBlocked] = []string{"needs set", "drop"} // needs_blocked.go: literal commands on the note
	return all
}

var closingVerbs = []string{"ack", "wait", "keep", "drop", "rework", "accept", "return", "brief", "rebrief", "release", "resume", "ask", "reader", "route add", "fleet", "raise", "promoted", "merge", "check", "clear", "add", "rank", "resolve", "take", "goal", "act", "read", "do more"}

func isClosingDecision(d string) bool {
	if d == SeatSilentDecision || d == "needs set" {
		return true
	}
	if d == "look" || d == "look at the card" || d == "look at the reader" || d == "look at the readers" || d == "look at both" {
		return false
	}
	for _, v := range closingVerbs {
		if d == v || strings.HasPrefix(d, v+" ") || strings.HasPrefix(d, v+" --") {
			return true
		}
	}
	return false
}

func TestEveryJudgmentKindHasAVerbThatClosesIt(t *testing.T) {
	t.Parallel()
	all := judgmentKindDecisions()
	require.Contains(t, all, NNoRoute)
	require.Contains(t, all, NLandingStall)
	for typ, ds := range all {
		closing := []string{}
		for _, d := range ds {
			if isClosingDecision(d) {
				closing = append(closing, d)
			}
		}
		assert.NotEmpty(t, closing, "%s lists no verb that closes it (decisions %v)", typ, ds)
		// the closing decision prints a runnable command
		n := Note{ID: "n-x.1", Kind: Judgment, Type: typ, Stream: "s1", Primaries: []string{"s1-1"}, Decisions: ds}
		g := Group{ID: n.ID, Kind: Judgment, Type: typ, Stream: "s1", Size: 1, Notes: []string{n.ID}, Members: []string{"s1-1"}, Decisions: ds}
		printed := map[string]bool{}
		for _, c := range commands(g, n, "", nil) {
			if len(c.Lines) > 0 {
				printed[c.Decision] = true
			}
		}
		for _, d := range closing {
			if d == "wait" || strings.HasPrefix(d, "wait ") || d == SeatSilentDecision || d == "needs set" {
				continue // waits are exercised below; the two specials carry their command in their text
			}
			assert.True(t, printed[d], "%s: closing decision %q prints no command", typ, d)
		}
		// running the seat verb closes it, where the seat verb is the closer:
		// ackable kinds ack, waitable tick-kept kinds wait.
		if contains(ds, "ack") || contains(ds, "keep") {
			w := setup(t, 1)
			note := Note{ID: "n-x.1", Kind: Judgment, Type: typ, Stream: "s1", Primaries: []string{"s1-1"}, Count: 1, Decisions: ds, At: w.s.Now}
			w.s.Open = append(w.s.Open, Open{Key: OpenKey(note.ID, "s1-1"), Note: note})
			p := Ack(w.s, AckReq{Notes: []string{note.ID}, Reason: "seen", Who: w.s.Coordinator})
			assert.Empty(t, p.Refused, "%s lists ack but ack was refused: %v", typ, p.Refused)
			assert.NotEmpty(t, p.Units, "%s lists ack but ack closed nothing", typ)
		} else if TickKept(typ) && (contains(ds, "wait") || hasWaitDecision(ds)) && WaitRefusal(Note{Kind: Judgment, Type: typ}) == "" {
			w := setup(t, 1)
			note := Note{ID: "n-x.1", Kind: Judgment, Type: typ, Stream: "s1", StreamLevel: true, Count: 1, Decisions: ds, At: w.s.Now}
			w.s.Open = append(w.s.Open, Open{Key: OpenKey(note.ID, StreamSubject("s1")), Note: note})
			p := Wait(w.s, WaitReq{Note: note.ID, Until: w.s.Now.Add(time.Hour), Who: w.s.Coordinator})
			assert.Empty(t, p.Refused, "%s lists wait but wait was refused: %v", typ, p.Refused)
		}
	}

	// A no-route judgment closes on the tick after route add serves its tier,
	// with a note saying the condition cleared; ack --reason closes it too.
	t.Run("NoRouteClosesAfterRouteAddAndByAck", func(t *testing.T) {
		w := setup(t, 1)
		var p Plan
		notify(&p, w.s, []cond{{typ: NNoRoute, stream: TierSubject("pro"), streamLevel: true, primaries: []string{"s1-1"}, what: "1 primaries of tier pro wait: no route"}}, []string{NNoRoute}, TickReq{})
		w.must(p)
		require.Len(t, w.notesOf(NNoRoute), 1)
		// route add serves the tier: the condition no longer holds
		p = Plan{}
		notify(&p, w.s, nil, []string{NNoRoute}, TickReq{})
		require.NotEmpty(t, p.Closes, "the tick after route add must close the no-route judgment")
		assert.NotEmpty(t, notesOfType(p.Notes, NConditionCleared), "the close must say the condition cleared: %v", p.Notes)
		for _, n := range notesOfType(p.Notes, NConditionCleared) {
			assert.Contains(t, n.What, "the condition cleared:")
		}
		w.must(p)
		// ack --reason
		p = Plan{}
		notify(&p, w.s, []cond{{typ: NNoRoute, stream: TierSubject("pro"), streamLevel: true, primaries: []string{"s1-1"}, what: "1 primaries of tier pro wait: no route"}}, []string{NNoRoute}, TickReq{})
		w.must(p)
		id := w.notesOf(NNoRoute)[0].ID
		// notesOf returns all; use the open one's id
		for _, o := range w.s.Open {
			if o.Note.Type == NNoRoute {
				id = o.Note.ID
			}
		}
		ack := Ack(w.s, AckReq{Notes: []string{id}, Reason: "route added", Who: w.s.Coordinator})
		require.Empty(t, ack.Refused, "ack --reason must close no-route: %v", ack.Refused)
		require.NotEmpty(t, ack.Units)
	})

	// A landing-stalled judgment is closed by ack --reason (held until the
	// next landing or the bound) and by the next landing; wait sets a review.
	t.Run("LandingStallClosesByAckWaitAndLanding", func(t *testing.T) {
		w := setup(t, 4)
		stallCard(w, "s1-1", Review)
		w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now.Add(-2*time.Minute)))
		p, _ := TickLandingStall(w.s, TickReq{})
		w.must(p)
		require.Len(t, w.notesOf(NLandingStall), 1)
		var id string
		for _, o := range w.s.Open {
			if o.Note.Type == NLandingStall {
				id = o.Note.ID
			}
		}
		require.NotEmpty(t, id)
		ack := Ack(w.s, AckReq{Notes: []string{id}, Reason: "watching it", Who: w.s.Coordinator})
		require.Empty(t, ack.Refused, "ack --reason must close landing-stalled: %v", ack.Refused)
		w.must(ack)
		applyAcked(w, ack)
		// held: the tick does not raise it again inside the bound
		p, _ = TickLandingStall(w.s, TickReq{})
		w.must(p)
		assert.Len(t, judgmentNotes(w.notesOf(NLandingStall)), 1, "an acked stall is held, not re-raised")
		// the next landing closes the held ack too
		w.tick(time.Minute)
		w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now))
		p, _ = TickLandingStall(w.s, TickReq{})
		w.must(p)
		applyCloses(w, p)
		assert.Empty(t, w.s.Acked, "a landing ends the held ack")
		// wait sets a review time
		w2 := setup(t, 4)
		stallCard(w2, "s1-1", Review)
		w2.s.Work.SetProp(PropLastLanding, stamp(w2.s.Now.Add(-2*time.Minute)))
		p2, _ := TickLandingStall(w2.s, TickReq{})
		w2.must(p2)
		var id2 string
		for _, o := range w2.s.Open {
			if o.Note.Type == NLandingStall {
				id2 = o.Note.ID
			}
		}
		wait := Wait(w2.s, WaitReq{Note: id2, Until: w2.s.Now.Add(time.Hour), Who: w2.s.Coordinator})
		require.Empty(t, wait.Refused, "wait must set a review on landing-stalled: %v", wait.Refused)
		require.NotEmpty(t, wait.Units)
	})
}

func judgmentNotes(notes []Note) []Note {
	var out []Note
	for _, n := range notes {
		if n.Kind == Judgment {
			out = append(out, n)
		}
	}
	return out
}

func applyCloses(w *world, p Plan) {
	for _, c := range p.Closes {
		for i, o := range w.s.Acked {
			if o.Key == c.Key {
				w.s.Acked = append(w.s.Acked[:i], w.s.Acked[i+1:]...)
				break
			}
		}
	}
}

func hasWaitDecision(ds []string) bool {
	for _, d := range ds {
		if d == "wait" || strings.HasPrefix(d, "wait ") {
			return true
		}
	}
	return false
}

func notesOfType(notes []Note, typ string) []Note {
	var out []Note
	for _, n := range notes {
		if n.Type == typ {
			out = append(out, n)
		}
	}
	return out
}

// applyAcked records a plan's acknowledged notes on the world, as the store does.
func applyAcked(w *world, p Plan) {
	for _, u := range p.Units {
		for _, n := range u.Notes {
			if n.Kind == Acknowledged {
				if n.ID == "" {
					n.ID = u.Key
				}
				sub := SprintSubject
				if len(n.Primaries) > 0 {
					sub = n.Primaries[0]
				}
				w.s.Acked = append(w.s.Acked, Open{Key: OpenKey(n.ID, sub), Note: n})
			}
		}
	}
}
