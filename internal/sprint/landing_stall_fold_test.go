package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// landingStallRaises counts the plan's raises of the landing-stall judgment: the
// units of the alarm that say "landing stalled", the line the tick prints each time
// it raises (a raise writes one note and one unit).
func landingStallRaises(p Plan) int {
	n := 0
	for _, u := range p.Units {
		if u.Key == PartLandingStall {
			n++
		}
	}
	return n
}

// setLandRefusal rewrites the standing refusal's text where it stands in the open notes.
func setLandRefusal(w *world, what string) {
	for i := range w.s.Open {
		if w.s.Open[i].Note.Type == NLanderRefuses {
			w.s.Open[i].Note.What = what
		}
	}
}

func openLandingStalls(w *world) []Open {
	var out []Open
	for _, o := range w.s.Open {
		if o.Note.Kind == Judgment && o.Note.Type == NLandingStall {
			out = append(out, o)
		}
	}
	return out
}

// The sprint-21 failure review: "landing stalled" was raised 703 times, again each
// minute while one cause stood (49 times for the one E12 refusal of test/main.c).
// The judgment is raised once per cause (the refused card and its finding), updated
// in place while that cause stands, and a new cause opens a new one.
//
// tla/CoordinatorPass.tla (Kind "stall") StallOncePerCause
func TestLandingStalledIsOneJudgmentPerCause(t *testing.T) {
	t.Parallel()
	w := setup(t, 4)
	stallCard(w, "s1-1", Review)
	w.s.Work.SetProp(PropLastLanding, stamp(w.s.Now.Add(-2*time.Minute)))
	w.note(Note{Kind: Judgment, Type: NLanderRefuses, SprintLevel: true, To: w.s.Coordinator, At: w.s.Now,
		What: "code=E12 stream=v13: test/main.c is not formatted (refused 3 times)"})

	raises := 0
	var first Note
	for i := 0; i < 49; i++ {
		p, _ := TickLandingStall(w.s, TickReq{})
		raises += landingStallRaises(p)
		w.must(p)
		if i == 0 {
			got := w.notesOf(NLandingStall)
			require.Len(t, got, 1)
			first = got[0]
		}
		w.tick(time.Minute)
		// the same cause, its numbers moving: three more refusals, a newer sha
		setLandRefusal(w, "code=E12 stream=v13: test/main.c is not formatted (refused "+string(rune('0'+i%7))+" times)")
	}
	assert.Equal(t, 1, raises, "49 ticks one minute apart under one E12 cause raise it once")
	require.Len(t, w.notesOf(NLandingStall), 1, "one judgment written")
	open := openLandingStalls(w)
	require.Len(t, open, 1, "one open judgment")
	assert.Equal(t, first.ID, open[0].Note.ID, "the same judgment, updated in place")
	assert.NotEqual(t, first.What, open[0].Note.What, "its age and counts changed")
	assert.Equal(t, first.At, open[0].Note.At, "its raise time is the cause's first")

	// a new cause: a different finding of the same class opens a new judgment, the old closes
	setLandRefusal(w, "code=E12 stream=v13: internal/data/codegen is not formatted (refused 1 times)")
	p, _ := TickLandingStall(w.s, TickReq{})
	assert.Equal(t, 1, landingStallRaises(p), "a new cause raises")
	w.must(p)
	require.Len(t, w.notesOf(NLandingStall), 2, "a second judgment for the new cause")
	open = openLandingStalls(w)
	require.Len(t, open, 1, "the old cause's judgment is closed")
	assert.NotEqual(t, first.ID, open[0].Note.ID)
}
